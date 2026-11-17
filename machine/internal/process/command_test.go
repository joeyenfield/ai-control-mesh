package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const commandHelper = "MESH_TEST_COMMAND_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(commandHelper); mode != "" {
		if mode == "wait" {
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[1:], "cwd": cwd, "value": os.Getenv("MESH_TEST_VALUE")})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testExecutable(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "android" {
		return os.Args[0]
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

func TestCommandPreservesArgumentsDirectoryAndEnvironment(t *testing.T) {
	args := []string{"with spaces", "$(literal)", "semi;colon", "--flag"}
	cmd := Command(testExecutable(t), args...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), commandHelper+"=print", "MESH_TEST_VALUE=kept")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got struct {
		Args  []string
		Cwd   string
		Value string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	wantDir, err := os.Stat(cmd.Dir)
	if err != nil {
		t.Fatal(err)
	}
	gotDir, err := os.Stat(got.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, args) || !os.SameFile(wantDir, gotDir) || got.Value != "kept" {
		t.Fatalf("command changed arguments, working directory or environment: %+v", got)
	}
}

func TestCommandContextStillCancelsChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := CommandContext(ctx, testExecutable(t))
	cmd.Env = append(os.Environ(), commandHelper+"=wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			t.Fatal("cancelled child exited successfully")
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("context cancellation did not stop child")
	}
}

func TestAndroidCommandRunsScriptsAndAppPrivateBinaries(t *testing.T) {
	if runtime.GOOS != "android" {
		t.Skip("Android app sandbox integration test")
	}
	self := testExecutable(t)
	raw := exec.Command(self)
	raw.Env = append(os.Environ(), commandHelper+"=print")
	if out, err := raw.CombinedOutput(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("run this regression in an Android app sandbox; direct exec returned %v: %s", err, out)
	}
	script := filepath.Join(t.TempDir(), "script with spaces")
	if err := os.WriteFile(script, []byte("#!/system/bin/sh\nprintf '%s' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	argument := "$(literal); with spaces"
	out, err := Command(script, argument).CombinedOutput()
	if err != nil || string(out) != argument {
		t.Fatalf("script failed or interpreted its argument: %v, %q", err, out)
	}
}

func TestAndroidShebangUsesTermuxInterpreter(t *testing.T) {
	prefix := t.TempDir()
	t.Setenv("PREFIX", prefix)
	interpreter := filepath.Join(prefix, "bin", "custom")
	if err := os.MkdirAll(filepath.Dir(interpreter), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(interpreter, []byte("\x7fELF"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(script, []byte("#!/usr/bin/custom\toptional argument\nprintf test\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, args := androidCommand(script, []string{"literal;$value"}, "arm64", 0)
	if path != "/system/bin/linker64" || !reflect.DeepEqual(args, []string{interpreter, "optional argument", script, "literal;$value"}) {
		t.Fatalf("interpreter, shebang option or arguments changed: %s %q", path, args)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Command(missing).CombinedOutput(); err == nil || !strings.Contains(fmt.Sprint(err), "no such file") && !os.IsNotExist(err) {
		t.Fatalf("missing executable error lost: %v", err)
	}
}
