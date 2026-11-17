package servers

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"ai-control-mesh/machine/internal/process"
	"ai-control-mesh/machine/internal/settings"
)

const nativeOllamaHelper = "MESH_TEST_NATIVE_OLLAMA"

// The test executable doubles as a native Ollama stand-in, including on Android
// where Python fixtures do not exercise the system-linker launch path.
func TestMain(m *testing.M) {
	if os.Getenv(nativeOllamaHelper) == "1" {
		if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "serve" {
			os.Exit(2)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"version":"native-test"}`)
		})
		if err := http.ListenAndServe(os.Getenv("OLLAMA_HOST"), mux); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		return
	}
	os.Exit(m.Run())
}

func TestOllamaStartsNativeBinaryAndStopsIt(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv(nativeOllamaHelper, "1")
	self, err := settings.Executable()
	if err != nil {
		t.Fatal(err)
	}
	setCommand(t, OllamaEnv, []string{self})
	manager := &Manager{Machine: on, Port: freePort(t)}
	if err := manager.startOllama("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.stopOllama() })
	pid := loadState().OllamaPID
	if pid == 0 || !process.Alive(pid) || manager.ollamaVersion() != "native-test" {
		t.Fatalf("native Ollama did not become ready (pid %d)", pid)
	}
	if err := manager.stopOllama(); err != nil {
		t.Fatal(err)
	}
	if process.Alive(pid) || manager.ollamaVersion() != "" {
		t.Fatal("native Ollama remained running after stop")
	}
}
