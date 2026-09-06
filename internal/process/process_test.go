package process

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestRunReturnsDeadlineAndStopsTheChildProcess(t *testing.T) {
	if os.Getenv("AUTOGIT_PROCESS_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	result, err := Run(ctx, Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunReturnsDeadlineAndStopsTheChildProcess$", "-test.v"},
		Env:        append(os.Environ(), "AUTOGIT_PROCESS_HELPER=1"),
		MaxOutput:  1 << 20,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v output=%q, want context deadline", err, result.Output)
	}
}

func TestRunBoundsCombinedOutput(t *testing.T) {
	result, err := Run(context.Background(), Options{
		Executable: "printf",
		Args:       []string{"hello"},
		MaxOutput:  4,
	})
	if err == nil || !result.Truncated || result.Output != "hell" {
		t.Fatalf("result=%+v err=%v, want truncated output", result, err)
	}
}
