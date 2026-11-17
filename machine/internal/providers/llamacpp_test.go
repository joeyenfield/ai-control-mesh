package providers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// router plays llama-server's router: two models, one loaded, and a download that reports
// progress on /models/sse once it's asked for.
func router(t *testing.T, key string, outcome string) *upstream {
	t.Helper()
	dir := t.TempDir()
	model := filepath.Join(dir, "qwen.gguf")
	_ = os.WriteFile(model, make([]byte, 1234), 0o600)
	// A model the router hasn't loaded comes with no file of its own, so agent-machine finds it
	// among the folders llama.cpp keeps models in.
	gguf(t, filepath.Join(dir, "gguf", "zeta-GGUF-Q4_K_M", "zeta-Q4_K_M.gguf"), 2222)
	ownLlamaFolders(t, filepath.Join(dir, "gguf"), filepath.Join(dir, "cache"))
	started := make(chan struct{}, 1)
	guard := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if key != "" && r.Header.Get("Authorization") != "Bearer "+key {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"error":{"code":401,"message":"Invalid API Key"}}`)
				return
			}
			handler(w, r)
		}
	}
	return serve(t, map[string]http.HandlerFunc{
		"GET /models": guard(reply(map[string]any{"data": []any{
			map[string]any{"id": "unsloth/zeta-GGUF:Q4_K_M", "path": "", "status": map[string]any{"value": "unloaded", "failed": true}},
			map[string]any{"id": "ggml-org/qwen-GGUF:Q8_0", "path": model, "status": map[string]any{"value": "loaded"}, "meta": map[string]any{"n_ctx_train": 32768}},
			map[string]any{"id": "big/one-GGUF:Q4_K_M", "status": map[string]any{"value": "downloading", "progress": map[string]any{"a": map[string]any{"done": 5, "total": 10}, "b": map[string]any{"done": 5, "total": 10}}}},
			map[string]any{"id": "nobody/absent-GGUF:Q5_K_M", "status": map[string]any{"value": "unloaded"}},
		}})),
		"POST /models": guard(func(w http.ResponseWriter, r *http.Request) {
			started <- struct{}{}
			reply(map[string]any{"success": true})(w, r)
		}),
		"GET /models/sse": guard(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				return
			}
			for _, line := range []string{
				`{"model":"someone/else-GGUF:Q4_K_M","event":"download_finished","data":{}}`,
				`{"model":"ggml-org/new-GGUF:Q4_K_M","event":"download_progress","data":{"u1":{"done":40,"total":100},"u2":{"done":10,"total":100}}}`,
				outcome,
			} {
				fmt.Fprintf(w, "data: %s\n\n", line)
				w.(http.Flusher).Flush()
			}
		}),
		"DELETE /models":      guard(reply(map[string]any{"success": true})),
		"POST /models/load":   guard(reply(map[string]any{"success": true})),
		"POST /models/unload": guard(reply(map[string]any{"success": false, "error": map[string]any{"message": "model is not loaded"}})),
	})
}

// gguf writes a file of size bytes where llama.cpp would keep it.
func gguf(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ownLlamaFolders points agent-machine at test folders for the models, as package servers does on
// a machine.
func ownLlamaFolders(t *testing.T, models, cache string) {
	t.Helper()
	keepFolder, keepCache := LlamaFolder, LlamaCache
	LlamaFolder = func() string { return models }
	LlamaCache = func() string { return cache }
	t.Cleanup(func() { LlamaFolder, LlamaCache = keepFolder, keepCache })
}

func TestLlamaStatusListsTheRoutersModels(t *testing.T) {
	fake := router(t, "", "")
	manager := &Manager{Machine: off}
	answer := manager.Handle(Request{Op: "status", Kind: "llamacpp", Port: fake.port()}, nil)
	state := answer["state"].(map[string]any)
	models := state["models"].([]map[string]any)
	if state["online"] != true || state["router"] != true || len(models) != 4 {
		t.Fatalf("%v", state)
	}
	if models[0]["name"] != "ggml-org/qwen-GGUF:Q8_0" || models[0]["loaded"] != true || models[0]["size"] != int64(1234) || models[0]["context_train"] != float64(32768) {
		t.Fatalf("loaded first, with its file's size: %v", models[0])
	}
	if downloading := models[1]; downloading["state"] != "downloading" || downloading["progress"].(map[string]any)["completed"] != int64(10) {
		t.Fatalf("%v", downloading)
	}
	if downloading := models[1]; downloading["size"] != int64(20) {
		t.Fatalf("a model still downloading shows what it will be: %v", downloading)
	}
	if absent := models[2]; absent["name"] != "nobody/absent-GGUF:Q5_K_M" || absent["size"] != nil {
		t.Fatalf("no file of its own anywhere: %v", absent)
	}
	if models[3]["failed"] != true {
		t.Fatalf("%v", models[3])
	}
	if zeta := models[3]; zeta["size"] != int64(2222) {
		t.Fatalf("an unloaded model the router names no file for still has a size here: %v", zeta)
	}
	if offline := (&Manager{Machine: off}).Handle(Request{Op: "status", Kind: "llamacpp", Port: 1}, nil)["state"].(map[string]any); offline["online"] != false {
		t.Fatalf("%v", offline)
	}
}

func TestLlamaSizeComesFromTheModelFolders(t *testing.T) {
	models, cache := t.TempDir(), t.TempDir()
	// How llama.cpp and agent-machine lay models out: a folder per quant in the download cache
	// and in the GGUF folder, the cache's flat file, a folder named after the model with the
	// quant as its file, and a model in several files.
	gguf(t, filepath.Join(cache, "Tiny-GGUF-Q4_K_M", "tiny-Q4_K_M.gguf"), 1000)
	gguf(t, filepath.Join(cache, "Tiny-GGUF-Q4_K_M", "tiny-Q4_K_M-mmproj-f16.gguf"), 700) // not part of the model
	gguf(t, filepath.Join(cache, "Flat-GGUF-Q5_K_M.gguf"), 1500)
	gguf(t, filepath.Join(models, "Small-Llama", "Q6_K.gguf"), 2000)
	gguf(t, filepath.Join(models, "Huge-Model-Q8_0", "Huge-Model-00001-of-00003.gguf"), 1000)
	gguf(t, filepath.Join(models, "Huge-Model-Q8_0", "Huge-Model-00002-of-00003.gguf"), 1000)
	gguf(t, filepath.Join(models, "Huge-Model-Q8_0", "Huge-Model-00003-of-00003.gguf"), 500)
	gguf(t, filepath.Join(models, "Vision-GGUF", "Q8_0", "Vision.Q8_0.gguf"), 3000)
	gguf(t, filepath.Join(models, "Vision-GGUF", "Q4_K_M", "Vision.Q4_K_M.gguf"), 4000)
	gguf(t, filepath.Join(models, "Part-GGUF", "Q4_K_M", "part-00001-of-00002.gguf"), 600)
	gguf(t, filepath.Join(models, "Part-GGUF", "Q4_K_M", "part-00002-of-00002.gguf"), 600)
	ownLlamaFolders(t, models, cache)
	disk := llamaModelFiles()
	for _, model := range []struct {
		id   string
		size int64
	}{
		{"someone/Tiny-GGUF:Q4_K_M", 1000},
		{"someone/Flat-GGUF:Q5_K_M", 1500},
		{"acme/Small-Llama-GGUF:Q6_K", 2000},
		{"bigorg/Huge-Model:Q8_0", 2500},
		{"me/Vision-GGUF:Q8_0", 3000},
		{"me/Vision-GGUF:Q4_K_M", 4000},
		{"me/Part-GGUF:Q4_K_M", 1200},
	} {
		total, ok := disk.sizeFor(model.id)
		if !ok || total != model.size {
			t.Fatalf("%s is %d, not %d: %v", model.id, total, model.size, disk)
		}
	}
	for _, id := range []string{"no/such-model:Q4_K_M", "someone/Tiny-GGUF:Q8_0", "me/Part-GGUF:Q8_0"} {
		if total, ok := disk.sizeFor(id); ok {
			t.Fatalf("%s found %d bytes of files that aren't its", id, total)
		}
	}
}

func TestLlamaSizeCountsEachRootOnceAndFollowsWeightLinks(t *testing.T) {
	root := t.TempDir()
	gguf(t, filepath.Join(root, "Tiny-Q4_0.gguf"), 1234)
	ownLlamaFolders(t, root, root)
	if total, ok := llamaModelFiles().sizeFor("Tiny:Q4_0"); !ok || total != 1234 {
		t.Fatalf("the same model/cache folder was counted twice: %d, %v", total, ok)
	}
	t.Run("symlinks", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "models")
		if err := os.Symlink(root, alias); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		weight := filepath.Join(t.TempDir(), "weights.bin")
		gguf(t, weight, 2345)
		if err := os.Symlink(weight, filepath.Join(root, "Linked-Q4_0.gguf")); err != nil {
			t.Fatal(err)
		}
		ownLlamaFolders(t, root, alias)
		disk := llamaModelFiles()
		for id, want := range map[string]int64{"Tiny:Q4_0": 1234, "Linked:Q4_0": 2345} {
			if total, ok := disk.sizeFor(id); !ok || total != want {
				t.Fatalf("%s: %d, %v; want %d", id, total, ok, want)
			}
		}
	})
}

func TestLlamaPullRelaysTheDownloadsProgress(t *testing.T) {
	fake := router(t, "k3y", `{"model":"ggml-org/new-GGUF:Q4_K_M","event":"download_finished","data":{}}`)
	manager := &Manager{Machine: on}
	var updates []map[string]any
	answer := manager.Handle(Request{Op: "pull", Kind: "llamacpp", Port: fake.port(), Model: "ggml-org/new-GGUF:Q4_K_M", Key: "k3y"}, func(update map[string]any) { updates = append(updates, update) })
	if answer["ok"] != true {
		t.Fatalf("%v", answer)
	}
	if fake.bodies["POST /models"]["model"] != "ggml-org/new-GGUF:Q4_K_M" {
		t.Fatalf("asked %v", fake.bodies["POST /models"])
	}
	// Another model's events aren't this pull's.
	if len(updates) != 3 || updates[1]["completed"] != int64(50) || updates[1]["total"] != int64(200) || updates[2]["status"] != "success" {
		t.Fatalf("%v", updates)
	}
	// Without the container's key, the router refuses.
	if answer := manager.Handle(Request{Op: "status", Kind: "llamacpp", Port: fake.port()}, nil); answer["state"].(map[string]any)["online"] != false {
		t.Fatalf("%v", answer)
	}
}

func TestLlamaPullSaysWhyADownloadFailed(t *testing.T) {
	fake := router(t, "", `{"model":"ggml-org/new-GGUF:Q4_K_M","event":"download_failed","data":{"error":"repo not found"}}`)
	answer := (&Manager{Machine: on}).Handle(Request{Op: "pull", Kind: "llamacpp", Port: fake.port(), Model: "ggml-org/new-GGUF:Q4_K_M"}, nil)
	if answer["ok"] != false || answer["error"] != "repo not found" {
		t.Fatalf("%v", answer)
	}
	for _, bad := range []string{"not-a-repo", "a/b/c", "a/b:Q4?x=1", "../x"} {
		if answer := (&Manager{Machine: on}).Handle(Request{Op: "pull", Kind: "llamacpp", Port: fake.port(), Model: bad}, nil); answer["ok"] != false {
			t.Fatalf("%s was pulled", bad)
		}
	}
}

func TestLlamaPullSaysAnOldRouterIsTooOld(t *testing.T) {
	t.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	// llama.cpp's routers before mid-2026 list and load models but have no downloads.
	old := serve(t, map[string]http.HandlerFunc{"GET /models": reply(map[string]any{"data": []any{}})})
	answer := (&Manager{Machine: on}).Handle(Request{Op: "pull", Kind: "llamacpp", Port: old.port(), Model: "ggml-org/new-GGUF:Q4_K_M"}, nil)
	if text, _ := answer["error"].(string); answer["ok"] != false || !strings.Contains(text, "too old") || !strings.Contains(text, "pkg upgrade llama-cpp") {
		t.Fatalf("%v", answer)
	}
	for _, call := range old.calls {
		if call == "POST /models" {
			t.Fatal("started a download it can't follow")
		}
	}
	// A llama-server run with -m lists its one model without a status: that one needs router mode.
	single := serve(t, map[string]http.HandlerFunc{"GET /models": reply(map[string]any{"data": []any{map[string]any{"id": "qwen.gguf"}}})})
	answer = (&Manager{Machine: on}).Handle(Request{Op: "pull", Kind: "llamacpp", Port: single.port(), Model: "ggml-org/new-GGUF:Q4_K_M"}, nil)
	if text, _ := answer["error"].(string); !strings.Contains(text, "router mode") {
		t.Fatalf("%v", answer)
	}
}

func TestLlamaDeleteLoadUnload(t *testing.T) {
	fake := router(t, "", "")
	manager := &Manager{Machine: on}
	if answer := manager.Handle(Request{Op: "delete", Kind: "llamacpp", Port: fake.port(), Model: "ggml-org/qwen-GGUF:Q8_0"}, nil); answer["ok"] != true {
		t.Fatalf("%v", answer)
	}
	if answer := manager.Handle(Request{Op: "load", Kind: "llamacpp", Port: fake.port(), Model: "ggml-org/qwen-GGUF:Q8_0"}, nil); answer["ok"] != true || fake.bodies["POST /models/load"]["model"] != "ggml-org/qwen-GGUF:Q8_0" {
		t.Fatalf("%v", answer)
	}
	if answer := manager.Handle(Request{Op: "unload", Kind: "llamacpp", Port: fake.port(), Model: "x/y"}, nil); answer["ok"] != false || !strings.Contains(answer["error"].(string), "not loaded") {
		t.Fatalf("a refused unload looked fine: %v", answer)
	}
	if answer := (&Manager{Machine: off}).Handle(Request{Op: "delete", Kind: "llamacpp", Port: fake.port(), Model: "x/y"}, nil); answer["ok"] != false {
		t.Fatalf("changed models with servers off: %v", answer)
	}
}

func TestLlamaLogIsAgentMachines(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	manager := &Manager{Machine: off}
	if answer := manager.Handle(Request{Op: "log", Kind: "llamacpp", Port: 8080}, nil); answer["ok"] != false {
		t.Fatalf("%v", answer)
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("AI_PROXY_MACHINE_HOME"), "llamacpp.log"), []byte("main: server is listening"), 0o600)
	if answer := manager.Handle(Request{Op: "log", Kind: "llamacpp", Port: LlamaPort}, nil); answer["text"] != "main: server is listening" {
		t.Fatalf("%v", answer)
	}
}
