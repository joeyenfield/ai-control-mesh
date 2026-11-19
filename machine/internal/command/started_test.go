package command

import (
	"os"
	"testing"

	"golang.org/x/term"
)

// A service's stdin is /dev/null: a character device, but not a terminal, so its run is a service's,
// which update stops for the service manager to start again on the new binary.
func TestAServiceWithNullStdinIsNotATerminalRun(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if info, err := null.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("the null device isn't a character device here")
	}
	if got := startedFrom(term.IsTerminal(int(null.Fd()))); got != "service" {
		t.Fatalf("stdin of %s counts as %q, want service", os.DevNull, got)
	}
	if got := startedFrom(true); got != "terminal" {
		t.Fatalf("a terminal counts as %q", got)
	}
}
