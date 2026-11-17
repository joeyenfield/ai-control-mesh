package providers

// llama.cpp's llama-server in router mode (tools/server/README.md, "Using multiple models").
// Routes used:
//
//	GET    /models               every model it can serve: the cache, its models folder; each with its status
//	                             (unloaded, loading, loaded, sleeping, downloading, incomplete)
//	POST   /models               download a Hugging Face model ("user/repo:quant") into the cache, in the background
//	GET    /models/sse           the download's progress (download_progress, then download_finished or download_failed)
//	DELETE /models?model=        delete a cached model
//	POST   /models/load|unload   load or unload one
//	GET    /health               up, and never needs a key
//
// A llama-server run with one model (-m) has no router: it lists that model, loaded, and
// can't change models.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ai-control-mesh/machine/internal/settings"
)

// LlamaPort is where llama-server listens by default (and where agent-machine starts it).
const LlamaPort = 9931

// LlamaPortEnv moves the llama-server agent-machine starts (and looks for) to another port.
const LlamaPortEnv = "AI_PROXY_LLAMACPP_PORT"

// LlamaServerPort is the port agent-machine's llama-server uses: AI_PROXY_LLAMACPP_PORT, else 9931.
func LlamaServerPort() int {
	if port, err := strconv.Atoi(os.Getenv(LlamaPortEnv)); err == nil && port > 0 && port < 65536 {
		return port
	}
	return LlamaPort
}

// llamaModelName is what can be downloaded: a Hugging Face repo, and optionally its quant.
func llamaModelName(model string) error {
	repo, quant, _ := strings.Cut(model, ":")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(model, " \t\r\n?#&") || len(model) > 300 {
		return errors.New("name a Hugging Face model as user/repo or user/repo:quant (Q4_K_M, ...)")
	}
	if strings.Contains(quant, "/") {
		return errors.New("a quant is a name such as Q4_K_M")
	}
	return nil
}

func fileSize(path string) any {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(file string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".gguf") {
			if found, err := entry.Info(); err == nil {
				total += found.Size()
			}
		}
		return nil
	})
	return total
}

// LlamaCache is where llama.cpp keeps the models it downloads (LLAMA_CACHE). Package servers
// points it at the machine's setting; alone here it reads this process's environment, which is
// what llama.cpp itself uses when nothing is set.
var LlamaCache = func() string { return strings.TrimSpace(os.Getenv("LLAMA_CACHE")) }

// llamaModelFolders are the folders models live in here: the GGUF folder llama-server serves and
// agent-machine downloads into, and the folder it downloads Hugging Face models into (its standard
// cache when the machine names none).
func llamaModelFolders() []string {
	folders := []string{LlamaFolder()}
	if cache := strings.TrimSpace(LlamaCache()); cache != "" {
		folders = append(folders, cache)
	} else if cache, err := os.UserCacheDir(); err == nil {
		return append(folders, filepath.Join(cache, "llama.cpp"))
	}
	return folders
}

// ggufShard marks one part of a model llama.cpp split across files (name-00001-of-00003.gguf).
var ggufShard = regexp.MustCompile(`-\d+-of-\d+`)

// ggufName reduces a model or folder name for matching: lower case, with the extension, a split
// model's part numbers and every separator gone, so Qwen-GGUF, qwen.gguf and qwen_gguf are one
// name.
func ggufName(name string) string {
	trimmed := strings.ToLower(name)
	if strings.HasSuffix(trimmed, ".gguf") {
		trimmed = trimmed[:len(trimmed)-len(".gguf")]
	}
	trimmed = ggufShard.ReplaceAllString(trimmed, "")
	joined := make([]rune, 0, len(trimmed))
	for _, letter := range trimmed {
		if letter >= 'a' && letter <= 'z' || letter >= '0' && letter <= '9' {
			joined = append(joined, letter)
		}
	}
	return string(joined)
}

const (
	llamaScanDepth = 4   // llama.cpp nests a model's folder and its quant folder at most
	llamaScanFiles = 400 // a status poll every few seconds must not walk a folder of any size
)

// ggufGroup is a name's share of the files on disk: everything in a folder (a model llama.cpp
// keeps in several files, or its quant folders), or the parts one model in one folder is made of.
type ggufGroup struct {
	folder string
	stem   string // empty = the whole folder
}

// llamaDisk is llama.cpp's model files on this machine, and what the names in each folder and the
// parts of each model add up to.
type llamaDisk struct {
	files   []map[string]ggufGroup
	folders map[string]int64
	models  map[string]int64
}

// add records one .gguf file, naming it by every run of the chain of names from the searched
// folder down to the file: a folder's name means everything in it, and a name that reaches the
// file's own means that model and its other parts, which are always in the same folder. chain and
// the folders that hold it come in step: chain[i] is what folders[i] is called, and the last name
// is the file's own.
func (d *llamaDisk) add(chain []string, folders []string, home string, size int64) {
	stem := chain[len(chain)-1]
	groups := map[string]ggufGroup{}
	for i := range chain {
		group := ggufGroup{folder: home, stem: stem}
		if i < len(folders) {
			group = ggufGroup{folder: folders[i]}
		}
		name := ""
		for j := 0; j <= i; j++ {
			name += chain[j]
			groups[name] = group
		}
	}
	d.files = append(d.files, groups)
	d.models[home+"\x1f"+stem] += size
	for _, folder := range folders {
		d.folders[folder] += size
	}
}

// llamaModelFiles reads every folder llama.cpp keeps models in, without following a folder deeper
// than its layout or reading more than llamaScanFiles files.
func llamaModelFiles() *llamaDisk {
	disk := &llamaDisk{folders: map[string]int64{}, models: map[string]int64{}}
	seenRoots := map[string]bool{}
	for _, root := range llamaModelFolders() {
		if len(disk.files) >= llamaScanFiles {
			break
		}
		if root == "" {
			continue
		}
		// A configured model/cache folder may be the same directory through another path.
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		root, err = filepath.Abs(resolved)
		if err != nil || seenRoots[root] {
			continue
		}
		seenRoots[root] = true
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil || rel == "." {
				return nil
			}
			parts := strings.Split(rel, string(filepath.Separator))
			if len(parts)-1 > llamaScanDepth {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") {
					return fs.SkipDir
				}
				return nil
			}
			name := strings.ToLower(entry.Name())
			if !strings.HasSuffix(name, ".gguf") || strings.Contains(name, "mmproj") {
				return nil // a vision projector goes with a model but isn't part of its size
			}
			info, err := os.Stat(path) // a GGUF symlink has the size of its target, not its link text
			if err != nil || info.IsDir() {
				return nil
			}
			chain := make([]string, 0, len(parts))
			for _, part := range parts { // the last is the file's own name
				name := ggufName(part)
				if name == "" {
					return nil // a name nothing can match
				}
				chain = append(chain, name)
			}
			folders := make([]string, 0, len(parts)-1)
			for i := range parts[:len(parts)-1] {
				folders = append(folders, filepath.Join(append([]string{root}, parts[:i+1]...)...))
			}
			home := root
			if len(folders) > 0 {
				home = folders[len(folders)-1]
			}
			disk.add(chain, folders, home, info.Size())
			if len(disk.files) >= llamaScanFiles {
				return fs.SkipAll
			}
			return nil
		})
	}
	return disk
}

// llamaModelKeys are the names a listed model goes by on disk: llama.cpp's cache names a file or
// folder after its repo and quant, its models folder after the model alone. The quant is kept: a
// model listed with one quant is not the same files as another quant of the same repo.
func llamaModelKeys(id string) []string {
	repo, quant, _ := strings.Cut(id, ":")
	parts := strings.Split(repo, "/")
	short := ggufName(parts[len(parts)-1])
	org := ""
	if len(parts) > 1 {
		org = ggufName(parts[len(parts)-2])
	}
	// A Hugging Face GGUF repo is usually named "<model>-GGUF" while its files and folders carry
	// the model's name alone.
	shorts := []string{short}
	if bare := strings.TrimSuffix(short, "gguf"); bare != "" && bare != short {
		shorts = append(shorts, bare)
	}
	keys := []string{}
	if name := ggufName(quant); name != "" {
		for _, model := range shorts {
			keys = append(keys, model+name)
			if org != "" {
				keys = append(keys, org+model+name)
			}
		}
		return keys
	}
	for _, model := range shorts {
		keys = append(keys, model)
		if org != "" {
			keys = append(keys, org+model)
		}
	}
	return keys
}

// sizeFor is a listed model's bytes as the files here: the largest group of files one of its names
// matches, so a model in several files counts all of them and never another model's parts.
func (d *llamaDisk) sizeFor(id string) (int64, bool) {
	for _, key := range llamaModelKeys(id) {
		if key == "" {
			continue
		}
		best := int64(0)
		seen := map[ggufGroup]bool{}
		for _, groups := range d.files {
			group, ok := groups[key]
			if !ok || seen[group] {
				continue
			}
			seen[group] = true
			total := d.folders[group.folder]
			if group.stem != "" {
				total = d.models[group.folder+"\x1f"+group.stem]
			}
			if total > best {
				best = total
			}
		}
		if best > 0 {
			return best, true
		}
	}
	return 0, false
}

// llamaModels is the router's models as the console shows them, loaded first, then by name.
func llamaModels(listed any) []map[string]any {
	models := []map[string]any{}
	var disk *llamaDisk // the model files here, read only for a model the router gives no size for
	for _, raw := range list(object(listed)["data"]) {
		item := object(raw)
		id := str(item["id"])
		if id == "" {
			continue
		}
		status := object(item["status"])
		state := str(status["value"])
		if state == "" {
			// One model, no router: it's what's loaded.
			state = "loaded"
		}
		meta := object(item["meta"])
		path := str(item["path"])
		model := map[string]any{
			"name": id, "state": state, "loaded": state == "loaded" || state == "sleeping",
			"failed": status["failed"] == true, "path": path, "size": fileSize(path),
			"context_train": meta["n_ctx_train"], "parameters": meta["n_params"], "progress": nil,
			"input": object(item["architecture"])["input_modalities"],
		}
		if model["size"] == nil && meta["size"] != nil {
			model["size"] = meta["size"]
		}
		if progress := object(status["progress"]); len(progress) > 0 {
			var done, total float64
			for _, file := range progress {
				done += number(object(file)["done"])
				total += number(object(file)["total"])
			}
			model["progress"] = map[string]any{"completed": int64(done), "total": int64(total)}
		}
		// An older router gives the size of a model only once it has read its file, and names
		// no file at all, so a model that sits in one of these folders has to be found here.
		if model["size"] == nil {
			if disk == nil {
				disk = llamaModelFiles()
			}
			if total, ok := disk.sizeFor(id); ok {
				model["size"] = total
			}
		}
		// A downloading model has no file to measure yet, so the total the router reports for its
		// download stands in for its size.
		if model["size"] == nil && model["progress"] != nil {
			model["size"] = model["progress"].(map[string]any)["total"]
		}
		models = append(models, model)
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i]["loaded"] != models[j]["loaded"] {
			return models[i]["loaded"] == true
		}
		return str(models[i]["name"]) < str(models[j]["name"])
	})
	return models
}

// llamaHeaders is the key a llama.cpp container takes (the server's, sent with the request), if any.
func llamaHeaders(key string) map[string]string {
	if key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + key}
}

func llamaStatus(base, key string) map[string]any {
	listed, status, err := getJSON(base+"/models", llamaHeaders(key), 8*time.Second)
	if err != nil {
		return map[string]any{"online": false, "error": err.Error(), "models": []any{}, "router": false}
	}
	if status >= 400 {
		return map[string]any{"online": false, "error": fmt.Sprintf("llama-server returned %d", status), "models": []any{}, "router": false}
	}
	data := list(object(listed)["data"])
	// The router gives every model a status; a llama-server with one model doesn't.
	router := len(data) == 0
	for _, raw := range data {
		if _, ok := object(raw)["status"]; ok {
			router = true
		}
	}
	return map[string]any{"online": true, "router": router, "models": llamaModels(listed)}
}

func llamaCall(method, address, key string, body any, timeout time.Duration) error {
	response, err := send(method, address, body, llamaHeaders(key), timeout)
	if err != nil {
		return unavailable("llama-server", err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode == http.StatusUnauthorized {
		return errors.New("llama-server wants an API key this machine doesn't have")
	}
	if response.StatusCode == http.StatusNotFound {
		return errors.New("this llama-server serves one model: start it in router mode (without -m) to change its models")
	}
	if response.StatusCode >= 400 {
		return errors.New(errorText(data, response.StatusCode, "llama-server"))
	}
	if answer := object(decode(data)); answer["success"] == false {
		return errors.New(errorText(data, response.StatusCode, "llama-server"))
	}
	return nil
}

func decode(data []byte) any {
	var value any
	_ = json.Unmarshal(data, &value)
	return value
}

// sameModel tells whether an event is about the model being pulled: the router may name it
// with or without its quant, and in any case.
func sameModel(event, model string) bool {
	event, model = strings.ToLower(event), strings.ToLower(model)
	if event == model {
		return true
	}
	repo, _, _ := strings.Cut(model, ":")
	return !strings.Contains(model, ":") && strings.HasPrefix(event, repo+":")
}

// llamaTooOld is why a router has no downloads: llama.cpp's router could only load and unload
// models before its June 2026 builds (b9590 and older), which Termux mirrors that stopped
// syncing still hand out.
func llamaTooOld() string {
	text := "this llama-server is too old to download models (llama.cpp added downloads to its router in mid-2026): update llama.cpp"
	if strings.Contains(os.Getenv("PREFIX"), "com.termux") || os.Getenv("TERMUX_VERSION") != "" {
		return text + ". In Termux: pkg upgrade llama-cpp; if it stays old, pick another mirror with termux-change-repo and upgrade again"
	}
	return text + ", from the machine's llama.cpp server tab or however it was installed"
}

// noDownloads is a router too old to download, where agent-machine can download the files instead.
type noDownloads struct{ text string }

func (e noDownloads) Error() string { return e.text }

// llamaPullBy pulls with the router, or has agent-machine download the files (method "direct"),
// which it also does by itself when its own router is too old to.
func (m *Manager) llamaPullBy(method, base, key, model string, port int, progress Progress) error {
	switch method {
	case "direct":
		return m.llamaFetch(base, key, model, port, progress)
	case "", "router":
	default:
		return fmt.Errorf("pull with the router or directly, not %q", method)
	}
	err := llamaPull(base, key, model, progress)
	var old noDownloads
	if method == "" && errors.As(err, &old) && llamaOwn(port) {
		progress(map[string]any{"status": "this llama-server can't download models: agent-machine downloads the files itself"})
		return m.llamaFetch(base, key, model, port, progress)
	}
	return err
}

// llamaNoDownloads says why the router's download progress stream was refused.
func llamaNoDownloads(base, key string, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("llama-server wants an API key this machine doesn't have")
	case http.StatusNotFound:
		// A router lists every model with a status; only an old one lacks the download routes.
		if llamaStatus(base, key)["router"] == true {
			return noDownloads{llamaTooOld()}
		}
		return errors.New("this llama-server serves one model: start it in router mode (without -m) to download models")
	}
	return fmt.Errorf("llama-server answered %d to /models/sse, so it can't report a download's progress", status)
}

// llamaPull starts the router's download and relays its progress, Ollama's style, until it finishes.
func llamaPull(base, key, model string, progress Progress) error {
	if err := llamaModelName(model); err != nil {
		return err
	}
	// Listen first, so no event is missed between starting the download and listening.
	headers := map[string]string{"Accept": "text/event-stream"}
	for name, value := range llamaHeaders(key) {
		headers[name] = value
	}
	events, err := send(http.MethodGet, base+"/models/sse", nil, headers, 0)
	if err != nil {
		return unavailable("llama-server", err)
	}
	defer events.Body.Close()
	if events.StatusCode >= 400 {
		return llamaNoDownloads(base, key, events.StatusCode)
	}
	if err := llamaCall(http.MethodPost, base+"/models", key, map[string]any{"model": model}, time.Minute); err != nil {
		return err
	}
	progress(map[string]any{"status": "pulling " + model})
	// A router that stops reporting, or never reports this model, must not hold the
	// connection and its worker forever: give up after llamaPullIdle without any event.
	var timedOut atomic.Bool
	watchdog := time.AfterFunc(llamaPullIdle, func() { timedOut.Store(true); events.Body.Close() })
	defer watchdog.Stop()
	reader := bufio.NewReader(events.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if text := strings.TrimSpace(string(line)); strings.HasPrefix(text, "data:") {
			var event map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, "data:"))), &event) == nil && sameModel(str(event["model"]), model) {
				// Only this model's events count as progress: another model's download
				// (or heartbeats) must not keep a stalled pull alive forever.
				watchdog.Reset(llamaPullIdle)
				switch str(event["event"]) {
				case "download_progress":
					var done, total float64
					for _, file := range object(event["data"]) {
						done += number(object(file)["done"])
						total += number(object(file)["total"])
					}
					progress(map[string]any{"status": "downloading", "completed": int64(done), "total": int64(total)})
				case "download_finished":
					// The router lists it once its models are read again.
					_, _, _ = getJSON(base+"/models?reload=1", llamaHeaders(key), 30*time.Second)
					progress(map[string]any{"status": "success"})
					return nil
				case "download_failed":
					text := str(object(event["data"])["error"])
					if text == "" {
						text = str(object(event["data"])["message"])
					}
					if text == "" {
						text = "the download failed"
					}
					progress(map[string]any{"status": "error", "error": text})
					return errors.New(text)
				}
			}
		}
		if err != nil {
			if timedOut.Load() {
				return errors.New("llama-server reported no download progress for 10 minutes; giving up")
			}
			if err == io.EOF {
				return errors.New("llama-server stopped before the download finished")
			}
			return unavailable("llama-server", err)
		}
	}
}

// llamaPullIdle ends a pull whose progress stream has gone quiet.
const llamaPullIdle = 10 * time.Minute

func llamaDelete(base, key, model string) error {
	return llamaCall(http.MethodDelete, base+"/models?model="+url.QueryEscape(model), key, nil, time.Minute)
}

// llamaLog is the log of the llama-server agent-machine started (package servers writes it).
func llamaLog(port, tail int) (string, error) {
	if port != LlamaServerPort() {
		return "", fmt.Errorf("only the llama-server agent-machine starts (on port %d) has a log here", LlamaServerPort())
	}
	text, err := tailFile(filepath.Join(settings.Home(), "llamacpp.log"), tail)
	if errors.Is(err, os.ErrNotExist) {
		return "No log yet: agent-machine writes one when it starts llama.cpp.", nil
	}
	return text, err
}
