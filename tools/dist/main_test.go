package main

import (
	"bytes"
	"debug/elf"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAndroidARM64BuildUsesAndroidLoader(t *testing.T) {
	if _, err := androidCompiler("arm64"); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	program := filepath.Join(root, "cmd", "probe")
	if err := os.MkdirAll(program, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/probe\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(program, "main.go"), []byte("package main\nimport \"net\"\nfunc main() { net.LookupHost(\"example.com\") }\n"), 0o644); err != nil {
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
	symbols, err := binary.ImportedSymbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range symbols {
		if symbol.Name == "getaddrinfo" && symbol.Library == "libc.so" {
			return
		}
	}
	t.Fatal("Android binary does not use libc getaddrinfo; DNS would fall back to localhost")
}

func TestAndroidCompiler(t *testing.T) {
	for _, variable := range []string{"CC", "ANDROID_NDK_HOME", "ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		t.Setenv(variable, "")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := androidCompiler("arm64"); err == nil {
		t.Fatal("missing NDK must fail rather than build with broken DNS")
	}
	t.Setenv("CC", "custom-android-clang")
	if cc, err := androidCompiler("arm64"); err != nil || cc != "custom-android-clang" {
		t.Fatalf("explicit CC = %q, %v", cc, err)
	}
	t.Setenv("CC", "")
	sdk := t.TempDir()
	t.Setenv("ANDROID_HOME", sdk)
	ndk := filepath.Join(sdk, "ndk", "27.3")
	// Match the host's prebuilt directory without requiring a real NDK in unit tests.
	host := map[string]string{"linux": "linux-x86_64", "darwin": "darwin-x86_64", "windows": "windows-x86_64"}[runtime.GOOS]
	name := "aarch64-linux-android24-clang"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	want := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", host, "bin", name)
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []string{"", ndk} {
		t.Setenv("ANDROID_NDK_HOME", explicit)
		if cc, err := androidCompiler("arm64"); err != nil || cc != want {
			t.Fatalf("NDK %q: compiler = %q, %v; want %q", explicit, cc, err, want)
		}
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
