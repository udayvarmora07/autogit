//go:build linux || darwin

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestRunTerminatesDescendantsOnCancellation(t *testing.T) {
	if os.Getenv("AUTOGIT_PROCESS_TREE_HELPER") == "1" {
		child := exec.Command("sleep", "30")
		if err := child.Start(); err != nil {
			panic(err)
		}
		if err := os.WriteFile(os.Getenv("AUTOGIT_PROCESS_TREE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			panic(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}

	pidPath := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunTerminatesDescendantsOnCancellation$"},
		Env:        append(os.Environ(), "AUTOGIT_PROCESS_TREE_HELPER=1", "AUTOGIT_PROCESS_TREE_PID="+pidPath),
		MaxOutput:  1 << 20,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline", err)
	}
	var raw []byte
	var readErr error
	for attempt := 0; attempt < 20; attempt++ {
		raw, readErr = os.ReadFile(pidPath)
		if readErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 200; attempt++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		} else if err != nil {
			t.Fatalf("checking descendant %d: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant process %d survived cancellation", pid)
}
