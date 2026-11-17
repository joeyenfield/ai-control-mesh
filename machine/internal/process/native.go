package process

import (
	"os/exec"
	"runtime"
)

// NativeCommand starts one of our native binaries. Go executes children with raw
// syscalls, bypassing Termux's libc exec hook, so invoke Android's linker explicitly.
func NativeCommand(binary string, args ...string) *exec.Cmd {
	return nativeCommand(binary, args, runtime.GOOS, runtime.GOARCH)
}

func nativeCommand(binary string, args []string, goos, goarch string) *exec.Cmd {
	if goos == "android" {
		linker := "/system/bin/linker"
		if goarch == "arm64" || goarch == "amd64" {
			linker += "64"
		}
		return exec.Command(linker, append([]string{binary}, args...)...)
	}
	return exec.Command(binary, args...)
}
