package command

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"ai-control-mesh/machine/internal/settings"
)

func TestConfigSelectsOnlyRequestedFunctionsAndPreservesMachine(t *testing.T) {
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	machine := settings.LoadMachine()
	machine.Name, machine.Roots, machine.ContainerCLI = "workstation", []string{t.TempDir()}, "podman"
	machine.Containers = true
	if err := machine.Save(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := configCommand(nil, strings.NewReader("1,3,terminals\n"), &out); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	got := settings.LoadMachine()
	if !got.Launches || !got.Commands || !got.Files || got.Servers || got.Updates || got.Containers {
		t.Fatalf("wrong permissions: %+v", got)
	}
	if got.ID != machine.ID || got.Name != machine.Name || got.ContainerCLI != "podman" || got.Roots[0] != machine.Roots[0] {
		t.Fatalf("unrelated settings changed: %+v", got)
	}
	for _, detail := range []string{"hidden files", "root privileges", "shell commands can access", "within a minute"} {
		if !strings.Contains(out.String(), detail) {
			t.Fatalf("missing explanation %q", detail)
		}
	}
}

func TestConfigKeepCancelRetryAndDisable(t *testing.T) {
	for _, item := range []struct {
		input    string
		code     int
		disabled bool
	}{
		{"\n", 0, false}, {"", 1, false}, {"files", 1, false},
		{"1,unknown\n", 1, false}, {",\nnone\n", 0, true}, {"7\nnone\n", 0, true},
	} {
		t.Run(item.input, func(t *testing.T) {
			t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
			machine := settings.LoadMachine()
			machine.Servers = true
			if err := machine.Save(); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(settings.MachinePath())
			var out bytes.Buffer
			if code := configCommand(nil, strings.NewReader(item.input), &out); code != item.code {
				t.Fatalf("status %d: %s", code, out.String())
			}
			if item.disabled {
				for _, name := range settings.Toggles {
					if settings.LoadMachine().Toggle(name) {
						t.Fatalf("%s still enabled", name)
					}
				}
			} else {
				after, _ := os.ReadFile(settings.MachinePath())
				if !bytes.Equal(before, after) {
					t.Fatal("unchosen permissions were saved")
				}
			}
		})
	}
}
