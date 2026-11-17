package servers

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"testing"

	"ai-control-mesh/machine/internal/process"
	"ai-control-mesh/machine/internal/settings"
)

const nativeOllamaHelper = "MESH_TEST_NATIVE_OLLAMA"
const nativeLlamaHelper = "MESH_TEST_NATIVE_LLAMA"

// The test executable doubles as a native Ollama stand-in, including on Android
// where Python fixtures do not exercise the system-linker launch path.
func TestMain(m *testing.M) {
	if os.Getenv(nativeOllamaHelper) == "1" || os.Getenv(nativeLlamaHelper) == "1" {
		if expected := os.Getenv("MESH_TEST_EXPECT_EXECUTABLE"); expected != "" && os.Getenv("TERMUX_EXEC__PROC_SELF_EXE") != expected {
			fmt.Fprintln(os.Stderr, "native model server inherited its parent's executable identity")
			os.Exit(4)
		}
		mux := http.NewServeMux()
		address := os.Getenv("OLLAMA_HOST")
		if os.Getenv(nativeLlamaHelper) == "1" {
			args := settings.CommandArgs()
			if len(args) == 1 && args[0] == "--version" {
				fmt.Println("version: 12345 (abcdef123)")
				return
			}
			flags := flag.NewFlagSet("native-llama", flag.ExitOnError)
			host := flags.String("host", "127.0.0.1", "")
			port := flags.String("port", "", "")
			_ = flags.Parse(args)
			address = *host + ":" + *port
			mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"ok"}`) })
		} else if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "serve" {
			os.Exit(2)
		}
		mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"version":"native-test"}`)
		})
		if err := http.ListenAndServe(address, mux); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		return
	}
	os.Exit(m.Run())
}

func TestLlamaStartsNativeBinaryWithCorrectExecutableIdentity(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv(nativeLlamaHelper, "1")
	self, err := settings.Executable()
	if err != nil {
		t.Fatal(err)
	}
	setCommand(t, LlamaEnv, []string{self})
	t.Setenv("TERMUX_EXEC__PROC_SELF_EXE", "/stale/agent-machine")
	expected := "/stale/agent-machine"
	if runtime.GOOS == "android" {
		expected = self
	}
	t.Setenv("MESH_TEST_EXPECT_EXECUTABLE", expected)
	m := &Manager{Machine: on, LlamaPort: freePort(t)}
	if err := m.startLlama(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.stopLlama() })
	if !m.llamaRunning() || llamaPID() == 0 {
		t.Fatal("native llama-server did not become ready")
	}
	if err := m.stopLlama(); err != nil {
		t.Fatal(err)
	}
	if m.llamaRunning() {
		t.Fatal("native llama-server remained running")
	}
}

func TestOllamaStartsNativeBinaryAndStopsIt(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv(nativeOllamaHelper, "1")
	self, err := settings.Executable()
	if err != nil {
		t.Fatal(err)
	}
	setCommand(t, OllamaEnv, []string{self})
	t.Setenv("TERMUX_EXEC__PROC_SELF_EXE", "/stale/agent-machine")
	expected := "/stale/agent-machine"
	if runtime.GOOS == "android" {
		expected = self
	}
	t.Setenv("MESH_TEST_EXPECT_EXECUTABLE", expected)
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
