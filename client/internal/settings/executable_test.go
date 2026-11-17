package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExecutableUnderAndroidLinker(t *testing.T) {
	name := "agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	self := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(self, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(self))
	for _, argv := range [][]string{{self, self, "start"}, {name, self, "start"}, {self, "run"}} {
		for _, linker := range []string{"/system/bin/linker64", "/apex/com.android.runtime/bin/linker64"} {
			got, err := executablePath(linker, argv, "android")
			if err != nil || got != self {
				t.Fatalf("executablePath(%q, %q) = %q, %v; want %q", linker, argv, got, err, self)
			}
		}
	}
	for _, goos := range []string{"linux", "windows", "android"} {
		got, err := executablePath(self, nil, goos)
		if err != nil || got != self {
			t.Fatalf("ordinary executable on %s = %q, %v", goos, got, err)
		}
	}
	if _, err := executablePath("/system/bin/linker64", nil, "android"); err == nil {
		t.Fatal("missing executable identity must fail")
	}
}
