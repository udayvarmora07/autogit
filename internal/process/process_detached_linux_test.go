//go:build linux

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A repository-controlled verifier can daemonize a helper into a new session
// that outlives both the process-group kill and the /proc descendant scan
// while still holding the inherited output pipes. Run must still return in
// bounded time and must report the force-closed stream instead of treating it
// as complete output.
func TestRunReturnsWhenDetachedChildHoldsTheOutputPipe(t *testing.T) {
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("setsid is not installed")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not installed")
	}
	pidFile := filepath.Join(t.TempDir(), "detached.pid")
	script := fmt.Sprintf("%s sh -c 'echo $$ > %s; exec sleep 30' & sleep 0.2; exit 0", setsid, pidFile)
	t.Cleanup(func() {
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	start := time.Now()
	result, err := Run(context.Background(), Options{Executable: sh, Args: []string{"-c", script}, MaxOutput: 1 << 20})
	elapsed := time.Since(start)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Run() error=%v result=%+v, want ErrWaitDelay", err, result)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("Run() took %v while a detached descendant held the output pipe", elapsed)
	}
}
