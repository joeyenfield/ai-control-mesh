//go:build !windows

package terminal

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPTYStartsShellInAppStorage(t *testing.T) {
	shell := "/bin/sh"
	if runtime.GOOS == "android" {
		shell = "/system/bin/sh"
	}
	data, err := os.ReadFile(shell)
	if err != nil {
		t.Fatal(err)
	}
	privateShell := filepath.Join(t.TempDir(), "shell with spaces")
	if err := os.WriteFile(privateShell, data, 0o700); err != nil {
		t.Fatal(err)
	}
	terminal, err := startChild([]string{privateShell, "-c", "printf 'ready\\n'; read value; printf 'reply:%s\\n' \"$value\""}, t.TempDir(), os.Environ(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	terminal.Write([]byte("hello\n"))
	done := make(chan struct {
		output string
		code   int
	}, 1)
	go func() {
		output, _ := io.ReadAll(terminal)
		done <- struct {
			output string
			code   int
		}{string(output), terminal.Wait()}
	}()
	select {
	case result := <-done:
		if result.code != 0 || !strings.Contains(result.output, "ready") || !strings.Contains(result.output, "reply:hello") {
			t.Fatalf("shell did not exchange terminal input/output: exit %d, %q", result.code, result.output)
		}
	case <-time.After(5 * time.Second):
		terminal.Close()
		t.Fatal("shell did not answer terminal input")
	}
}
