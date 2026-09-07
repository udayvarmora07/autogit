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

func TestRunTerminatesAChildThatExceedsTheOutputLimit(t *testing.T) {
	if os.Getenv("AUTOGIT_PROCESS_OUTPUT_HELPER") == "1" {
		chunk := make([]byte, 4096)
		_, _ = os.Stdout.Write(chunk)
		time.Sleep(30 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := Run(ctx, Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunTerminatesAChildThatExceedsTheOutputLimit$"},
		Env:        append(os.Environ(), "AUTOGIT_PROCESS_OUTPUT_HELPER=1"),
		MaxOutput:  1024,
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("Run() error = %v result=%+v, want ErrOutputLimit", err, result)
	}
}

func TestRunHandlesConcurrentStdoutAndStderrOverflow(t *testing.T) {
	if os.Getenv("AUTOGIT_PROCESS_DUAL_OUTPUT_HELPER") == "1" {
		chunk := make([]byte, 4096)
		_, _ = os.Stdout.Write(chunk)
		_, _ = os.Stderr.Write(chunk)
		time.Sleep(30 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := Run(ctx, Options{
		Executable:     os.Args[0],
		Args:           []string{"-test.run=^TestRunHandlesConcurrentStdoutAndStderrOverflow$"},
		Env:            append(os.Environ(), "AUTOGIT_PROCESS_DUAL_OUTPUT_HELPER=1"),
		MaxOutput:      1024,
		SeparateOutput: true,
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("Run() error = %v result=%+v, want ErrOutputLimit", err, result)
	}
}
