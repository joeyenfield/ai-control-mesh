package servers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func waitOllamaInstall(t *testing.T) installJob {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if j, ok := ollamaInstallProgress().(installJob); ok && j.State != "installing" {
			return j
		}
	}
	t.Fatal("Ollama installation did not finish")
	return installJob{}
}

func TestOllamaSetupInstallsMatchingPackagesAndPreservesModels(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv("PREFIX", t.TempDir())
	t.Setenv("TERMUX_VERSION", "test")
	setCommand(t, OllamaAptEnv, fake(t, "fake_ollama_apt.py"))
	setCommand(t, SystemctlEnv, []string{})
	m := &Manager{Machine: on, Port: freePort(t), LlamaPort: freePort(t)}
	config := DefaultConfig()
	config["models_dir"], config["keep_alive"], config["extra_env"] = "my-existing-models", "30m", "OLLAMA_VULKAN=0\nOLLAMA_CONTEXT_LENGTH=65536\nOLLAMA_DEBUG=1"
	if _, err := SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	options := m.Handle(Request{Kind: "ollama", Op: "install_options"})["ollama_install"].(map[string]any)
	if options["source"] != "termux" || len(options["variants"].([]Variant)) != 3 {
		t.Fatal(options)
	}
	for _, variant := range []string{"vulkan-adreno", "vulkan", "cpu"} {
		if answer := m.Handle(Request{Kind: "ollama", Op: "install", Variant: variant}); answer["ok"] != true {
			t.Fatal(answer)
		}
		if job := waitOllamaInstall(t); job.State != "installed" {
			t.Fatal(job)
		}
		saved := LoadConfig()
		if saved["models_dir"] != "my-existing-models" || saved["keep_alive"] != "30m" {
			t.Fatal(saved)
		}
		if variant == "cpu" {
			if saved["vulkan"] != false || saved["gpus"] != "-1" || saved["llm_library"] != "cpu" {
				t.Fatal(saved)
			}
		} else if saved["vulkan"] != true || saved["gpus"] != "" || saved["kv_cache_type"] != "f16" || saved["flash_attention"] != false || saved["context_length"] != int64(8192) {
			t.Fatal(saved)
		}
		if saved["extra_env"] != "OLLAMA_DEBUG=1" {
			t.Fatal(saved)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(os.Getenv("PREFIX"), "apt-calls.jsonl"))
	if !strings.Contains(string(calls), `"ollama", "vulkan-loader-generic", "mesa-vulkan-icd-freedreno"`) || !strings.Contains(string(calls), `"ollama", "vulkan-loader-android"`) {
		t.Fatal(string(calls))
	}
	if answer := m.Handle(Request{Kind: "ollama", Op: "install", Variant: "vulkan; touch /tmp/x"}); answer["ok"] != false {
		t.Fatal(answer)
	}
}

func TestOllamaSetupFailureLeavesSettingsAndReportsLog(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv("PREFIX", t.TempDir())
	t.Setenv("TERMUX_VERSION", "test")
	setCommand(t, OllamaAptEnv, fake(t, "fake_ollama_apt.py"))
	setCommand(t, SystemctlEnv, []string{})
	m := &Manager{Machine: on, Port: freePort(t), LlamaPort: freePort(t)}
	_, _ = SaveConfig(map[string]any{"models_dir": "existing", "context_length": int64(32768)})
	for _, name := range []string{"FAKE_OLLAMA_APT_FAIL", "FAKE_OLLAMA_APT_MISSING"} {
		t.Setenv(name, "1")
		if answer := m.Handle(Request{Kind: "ollama", Op: "install", Variant: "vulkan-adreno"}); answer["ok"] != true {
			t.Fatal(answer)
		}
		if job := waitOllamaInstall(t); job.State != "failed" || job.Error == "" {
			t.Fatal(job)
		}
		if config := LoadConfig(); config["models_dir"] != "existing" || config["context_length"] != int64(32768) || config["vulkan"] != false {
			t.Fatal(config)
		}
		os.Unsetenv(name)
	}
}

func TestOllamaSetupBlocksConcurrentChangesAndRunningServer(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv("PREFIX", t.TempDir())
	t.Setenv("TERMUX_VERSION", "test")
	t.Setenv("FAKE_OLLAMA_APT_DELAY", ".15")
	setCommand(t, OllamaAptEnv, fake(t, "fake_ollama_apt.py"))
	setCommand(t, SystemctlEnv, []string{})
	m := &Manager{Machine: on, Port: freePort(t), LlamaPort: freePort(t)}
	if answer := m.Handle(Request{Kind: "ollama", Op: "install", Variant: "cpu"}); answer["ok"] != true {
		t.Fatal(answer)
	}
	for _, op := range []string{"start", "restart", "configure", "install"} {
		if answer := m.Handle(Request{Kind: "ollama", Op: op, Variant: "cpu"}); answer["ok"] != false || !strings.Contains(answer["error"].(string), "being installed") {
			t.Fatal(answer)
		}
	}
	if job := waitOllamaInstall(t); job.State != "installed" {
		t.Fatal(job)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"version":"test"}`) }))
	defer server.Close()
	port, _ := strconv.Atoi(strings.Split(server.URL, ":")[2])
	m.Port = port
	if answer := m.Handle(Request{Kind: "ollama", Op: "install", Variant: "cpu"}); answer["ok"] != false || !strings.Contains(answer["error"].(string), "stop Ollama") {
		t.Fatal(answer)
	}
}
