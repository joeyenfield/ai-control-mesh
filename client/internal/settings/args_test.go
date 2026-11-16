package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestCommandArgs(t *testing.T) {
	name := "agent-machine"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	self := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(self, []byte("test executable"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(self))
	for _, tc := range []struct {
		name string
		goos string
		argv []string
		want []string
	}{
		{"termux login", "android", []string{self, self, "login", "https://mesh.example"}, []string{"login", "https://mesh.example"}},
		{"termux start", "android", []string{self, self, "start"}, []string{"start"}},
		{"termux from PATH", "android", []string{name, self, "start"}, []string{"start"}},
		{"termux no command", "android", []string{self, self}, []string{}},
		{"android normal", "android", []string{self, "login"}, []string{"login"}},
		{"another executable", "android", []string{self, self + "-other", "login"}, []string{self + "-other", "login"}},
		{"relative agent name", "android", []string{"agent", "agent", "."}, []string{"agent", "."}},
		{"linux preserves all", "linux", []string{self, self, "login"}, []string{self, "login"}},
		{"empty", "android", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandArgs(tc.argv, tc.goos); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("commandArgs = %q, want %q", got, tc.want)
			}
		})
	}
}
