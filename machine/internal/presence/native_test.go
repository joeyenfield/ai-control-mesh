package presence

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-control-mesh/machine/internal/settings"
)

const nativeAgentHelper = "MESH_TEST_NATIVE_AGENT"

func TestMain(m *testing.M) {
	if path := os.Getenv(nativeAgentHelper); path != "" {
		cwd, _ := os.Getwd()
		data, _ := json.Marshal(map[string]any{"args": settings.CommandArgs(), "cwd": cwd, "machine": os.Getenv("AI_PROXY_MACHINE_ID")})
		if err := os.WriteFile(path, data, 0o600); err != nil {
			os.Exit(2)
		}
		fmt.Println(`{"session_id":"native-shell-session"}`)
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestShellLaunchStartsNativeAgent(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	self, err := settings.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_PROXY_AGENT", self)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(t.TempDir(), "agent-start.json")
	t.Setenv(nativeAgentHelper, proof)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// A reserved, non-HTTP port prevents probing a real agent-proxy during this test.
	t.Setenv("AI_PROXY_LOCAL_PORT", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port))
	_ = listener.Close()
	machine := settings.LoadMachine()
	machine.Commands, machine.Roots = true, []string{root}
	if err := machine.Save(); err != nil {
		t.Fatal(err)
	}
	p := &Presence{Upstream: settings.Upstream{Fixed: "http://127.0.0.1:29992"}}
	id, err := p.shellHeadless(Launch{ID: "native-shell", Project: root})
	if err != nil || id != "native-shell-session" {
		t.Fatalf("shell agent launch: %q, %v", id, err)
	}
	data, err := os.ReadFile(proof)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args    []string
		Cwd     string
		Machine string
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cwd != root || got.Machine != machine.ID || len(got.Args) != 5 || got.Args[0] != "--shell" || got.Args[1] != root || got.Args[2] != "--headless" || got.Args[4] != "native-shell" {
		t.Fatalf("shell launch arguments or identity changed: %+v", got)
	}
}
