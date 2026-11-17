package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Command preserves os/exec behavior while supporting native programs and
// script interpreters in Android app storage, where direct exec is restricted.
func Command(name string, args ...string) *exec.Cmd {
	return prepareCommand(exec.Command(name, args...), runtime.GOOS, runtime.GOARCH)
}

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return prepareCommand(exec.CommandContext(ctx, name, args...), runtime.GOOS, runtime.GOARCH)
}

// ExecutableEnvironment updates Termux's libc executable identity after a direct
// linker launch. Otherwise native readlink("/proc/self/exe") returns the parent's
// saved identity, so model routers respawn agent-machine instead of themselves.
// Call after building a custom environment, which replaces Cmd.Env wholesale.
func ExecutableEnvironment(cmd *exec.Cmd, base []string) []string {
	return executableEnvironment(cmd, base, runtime.GOOS)
}

func executableEnvironment(cmd *exec.Cmd, base []string, goos string) []string {
	if goos != "android" {
		return base
	}
	if base == nil {
		base = os.Environ()
	}
	binary := cmd.Path
	if (binary == "/system/bin/linker" || binary == "/system/bin/linker64") && len(cmd.Args) > 1 {
		binary = cmd.Args[1]
	}
	if binary == "" {
		return base
	}
	const name = "TERMUX_EXEC__PROC_SELF_EXE="
	env := make([]string, 0, len(base)+1)
	for _, value := range base {
		if !strings.HasPrefix(value, name) {
			env = append(env, value)
		}
	}
	return append(env, name+binary)
}

func prepareCommand(cmd *exec.Cmd, goos, goarch string) *exec.Cmd {
	if goos != "android" || cmd.Err != nil {
		return cmd
	}
	path, args := androidCommand(cmd.Path, cmd.Args[1:], goarch, 0)
	cmd.Path, cmd.Args = path, append([]string{path}, args...)
	return cmd
}

func androidCommand(path string, args []string, arch string, depth int) (string, []string) {
	if depth >= 4 {
		return path, args
	}
	file, err := os.Open(path)
	if err != nil {
		return path, args
	}
	buffer := make([]byte, 4096)
	n, _ := file.Read(buffer)
	_ = file.Close()
	header := string(buffer[:n])
	if strings.HasPrefix(header, "\x7fELF") {
		linker := "/system/bin/linker"
		if arch == "arm64" || arch == "amd64" {
			linker += "64"
		}
		return linker, append([]string{path}, args...)
	}
	if !strings.HasPrefix(header, "#!") {
		return path, args
	}
	line := strings.TrimSpace(strings.SplitN(header[2:], "\n", 2)[0])
	interpreter, option := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		interpreter, option = line[:i], strings.TrimSpace(line[i:])
	}
	if interpreter == "" {
		return path, args
	}
	// Termux scripts commonly name /bin/sh or /usr/bin/env. Resolve those
	// inside its prefix first; explicit interpreter paths remain explicit.
	if filepath.Dir(interpreter) == "/bin" || filepath.Dir(interpreter) == "/usr/bin" {
		candidates := []string{}
		if prefix := os.Getenv("PREFIX"); prefix != "" {
			candidates = append(candidates, filepath.Join(prefix, "bin", filepath.Base(interpreter)))
		}
		candidates = append(candidates, filepath.Join("/system/bin", filepath.Base(interpreter)))
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				interpreter = candidate
				break
			}
		}
	}
	childArgs := []string{}
	// Like the kernel's shebang handling, the optional argument is one value.
	if option != "" {
		childArgs = append(childArgs, option)
	}
	childArgs = append(childArgs, path)
	return androidCommand(interpreter, append(childArgs, args...), arch, depth+1)
}
