package process

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestAndroidNativeEnvironmentUsesChildExecutableIdentity(t *testing.T) {
	base := []string{"TERMUX_EXEC__PROC_SELF_EXE=/usr/bin/agent-machine", "LD_PRELOAD=/usr/lib/libtermux-exec.so", "MODEL=existing", "TERMUX_EXEC__PROC_SELF_EXE=/another/stale/parent"}
	for _, linker := range []string{"/system/bin/linker", "/system/bin/linker64"} {
		cmd := &exec.Cmd{Path: linker, Args: []string{linker, "/usr/bin/llama-server", "--host", "127.0.0.1"}}
		got := executableEnvironment(cmd, base, "android")
		want := []string{"LD_PRELOAD=/usr/lib/libtermux-exec.so", "MODEL=existing", "TERMUX_EXEC__PROC_SELF_EXE=/usr/bin/llama-server"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("child environment: %v", got)
		}
		if base[0] != "TERMUX_EXEC__PROC_SELF_EXE=/usr/bin/agent-machine" {
			t.Fatal("changed the parent's environment")
		}
	}
	cmd := &exec.Cmd{Path: "/system/bin/sh", Args: []string{"/system/bin/sh", "script"}}
	got := executableEnvironment(cmd, nil, "android")
	if got[len(got)-1] != "TERMUX_EXEC__PROC_SELF_EXE=/system/bin/sh" {
		t.Fatal(got)
	}
	if got := executableEnvironment(cmd, base, "linux"); !reflect.DeepEqual(got, base) {
		t.Fatal("changed a non-Android environment")
	}
}
