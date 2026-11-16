package main

import (
	"bytes"
	"debug/elf"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestAndroidARM64BuildUsesAndroidLoader(t *testing.T) {
	root := t.TempDir()
	program := filepath.Join(root, "cmd", "probe")
	if err := os.MkdirAll(program, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/probe\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(program, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := build(root, root, []string{"probe"}, "test", "android", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := elf.Open(filepath.Join(root, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	if binary.Type != elf.ET_DYN {
		t.Errorf("Android ARM64 binary type = %v, want ET_DYN", binary.Type)
	}
	loaderFound := false
	for _, segment := range binary.Progs {
		if segment.Type == elf.PT_INTERP {
			loaderFound = true
			loader, err := io.ReadAll(segment.Open())
			if err != nil || string(loader) != "/system/bin/linker64\x00" {
				t.Errorf("Android ARM64 loader = %q: %v", loader, err)
			}
		}
	}
	if !loaderFound {
		t.Error("Android ARM64 binary has no interpreter")
	}
}

// The archives carry the server's own uninstallers, uninstall.sh with LF whatever the checkout's endings.
func TestWriteUninstaller(t *testing.T) {
	root, folder := t.TempDir(), t.TempDir()
	source := filepath.Join(root, "server", "agent_server", "install")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"uninstall.sh": "#!/bin/sh\r\nset -eu\r\n", "uninstall.ps1": "param()\r\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeUninstaller(root, folder, name); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"uninstall.sh": "#!/bin/sh\nset -eu\n", "uninstall.ps1": "param()\r\n"} {
		got, err := os.ReadFile(filepath.Join(folder, name))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, want)
		}
	}
	// The archives' install.sh too: a CR after `set -eu` stops sh with "Illegal option -".
	if got := string(scriptText("install.sh", []byte("#!/bin/sh\r\nset -eu\r\n"))); got != "#!/bin/sh\nset -eu\n" {
		t.Errorf("install.sh: %q", got)
	}
	if bytes.Contains(scriptText("install.sh", installSh), []byte("\r")) {
		t.Error("the embedded install.sh would go out with CR in it")
	}
	if got := string(scriptText("install.ps1", []byte("param()\r\n"))); got != "param()\r\n" {
		t.Errorf("install.ps1: %q", got)
	}
	// The real ones are where the build looks for them.
	for _, name := range []string{"uninstall.sh", "uninstall.ps1"} {
		if err := writeUninstaller(filepath.Join("..", ".."), t.TempDir(), name); err != nil {
			t.Errorf("the checkout's %s: %v", name, err)
		}
	}
}
