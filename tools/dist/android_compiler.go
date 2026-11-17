package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Use the NDK's API 24 compiler so binaries also run on older Termux devices.
func androidCompiler(arch string) (string, error) {
	if arch != "arm64" {
		return "", fmt.Errorf("unsupported Android architecture %q", arch)
	}
	if cc := os.Getenv("CC"); cc != "" {
		return cc, nil
	}
	name := "aarch64-linux-android24-clang"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	if cc, err := exec.LookPath(name); err == nil {
		return cc, nil
	}
	roots := []string{}
	if ndk := os.Getenv("ANDROID_NDK_HOME"); ndk != "" {
		roots = append(roots, ndk)
	} else {
		for _, variable := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
			if sdk := os.Getenv(variable); sdk != "" {
				matches, _ := filepath.Glob(filepath.Join(sdk, "ndk", "*"))
				roots = append(roots, matches...)
			}
		}
	}
	host := map[string]string{"linux": "linux-x86_64", "darwin": "darwin-x86_64", "windows": "windows-x86_64"}[runtime.GOOS]
	for i := len(roots) - 1; i >= 0; i-- {
		cc := filepath.Join(roots[i], "toolchains", "llvm", "prebuilt", host, "bin", name)
		if info, err := os.Stat(cc); err == nil && !info.IsDir() {
			return cc, nil
		}
	}
	return "", fmt.Errorf("Android builds require the NDK for system DNS: set ANDROID_NDK_HOME, install an NDK under ANDROID_HOME/ndk, or set CC to an Android clang compiler")
}
