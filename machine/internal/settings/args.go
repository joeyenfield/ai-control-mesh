// Command-line handling shared by this module's executables.
package settings

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// CommandArgs removes the extra executable path inserted by Termux's Android
// linker before dispatching commands. Ordinary user arguments must stay intact.
func CommandArgs() []string {
	return commandArgs(os.Args, runtime.GOOS)
}

func commandArgs(argv []string, goos string) []string {
	if len(argv) == 0 {
		return nil
	}
	args := argv[1:]
	if goos == "android" && len(args) > 0 && filepath.IsAbs(args[0]) {
		name := argv[0]
		if filepath.Base(name) == name {
			if found, err := exec.LookPath(name); err == nil {
				name = found
			}
		}
		self, err := filepath.Abs(name)
		if err == nil && filepath.Clean(self) == filepath.Clean(args[0]) {
			return args[1:]
		}
	}
	return args
}
