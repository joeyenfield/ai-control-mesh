package settings

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Executable identifies our binary when Termux launched it through Android's linker.
// /proc/self/exe identifies the linker in that case, so os.Executable cannot restart us.
func Executable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return executablePath(self, os.Args, runtime.GOOS)
}

func executablePath(self string, argv []string, goos string) (string, error) {
	if goos != "android" || (filepath.Base(self) != "linker" && filepath.Base(self) != "linker64") {
		return self, nil
	}
	if len(argv) == 0 || argv[0] == "" {
		return "", fmt.Errorf("cannot identify executable launched by Android's linker")
	}
	name := argv[0]
	if filepath.Base(name) == name {
		found, err := exec.LookPath(name)
		if err != nil {
			return "", err
		}
		name = found
	}
	return filepath.Abs(name)
}
