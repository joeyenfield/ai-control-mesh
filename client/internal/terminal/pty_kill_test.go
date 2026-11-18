//go:build !windows

package terminal

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The console's Kill ends the agent and what it started, here a background sleep a shell waits on.
func TestKillEndsTheAgentAndItsProcessGroup(t *testing.T) {
	agent, err := startChild([]string{"/bin/sh", "-c", "sleep 60 & echo child:$!; wait"}, t.TempDir(), os.Environ(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	pids := make(chan int, 1)
	go func() {
		lines := bufio.NewScanner(readerFunc(agent.Read))
		for lines.Scan() {
			if after, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "child:"); ok {
				if pid, err := strconv.Atoi(after); err == nil {
					pids <- pid
				}
			}
		}
	}()
	var sleeper int
	select {
	case sleeper = <-pids:
	case <-time.After(5 * time.Second):
		t.Fatal("the shell didn't start its child")
	}
	agent.Kill()
	exited := make(chan int, 1)
	go func() { exited <- agent.Wait() }()
	select {
	case status := <-exited:
		if status != 128+int(syscall.SIGKILL) {
			t.Fatalf("exit status %d, want %d", status, 128+int(syscall.SIGKILL))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Kill didn't end the agent")
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(sleeper, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(sleeper, syscall.SIGKILL)
			t.Fatal("Kill left the agent's child running")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type readerFunc func([]byte) (int, error)

func (read readerFunc) Read(buffer []byte) (int, error) { return read(buffer) }
