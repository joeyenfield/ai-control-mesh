package process

import (
	"reflect"
	"testing"
)

func TestNativeCommandThroughAndroidLinker(t *testing.T) {
	binary := "/data/data/com.termux/files/usr/bin/agent-machine"
	args := []string{"run", "--server", "https://mesh.example"}
	for _, arch := range []string{"arm64", "amd64", "arm"} {
		linker := "/system/bin/linker64"
		if arch == "arm" {
			linker = "/system/bin/linker"
		}
		cmd := nativeCommand(binary, args, "android", arch)
		want := append([]string{linker, binary}, args...)
		if cmd.Path != linker || !reflect.DeepEqual(cmd.Args, want) {
			t.Fatalf("Android %s: %q %q, want %q", arch, cmd.Path, cmd.Args, want)
		}
	}
	cmd := nativeCommand(binary, args, "linux", "arm64")
	if cmd.Path != binary || !reflect.DeepEqual(cmd.Args, append([]string{binary}, args...)) {
		t.Fatalf("ordinary native command changed: %q", cmd.Args)
	}
}
