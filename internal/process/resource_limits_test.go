//go:build linux

package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunEnforcesFileSizeLimit(t *testing.T) {
	if os.Getenv("AUTOGIT_RESOURCE_FILE_HELPER") == "1" {
		path := os.Getenv("AUTOGIT_RESOURCE_FILE_PATH")
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, 2<<20), 0600); err != nil {
			os.Exit(42)
		}
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "limited.bin")
	result, err := Run(context.Background(), Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunEnforcesFileSizeLimit$"},
		Env:        []string{"AUTOGIT_RESOURCE_FILE_HELPER=1", "AUTOGIT_RESOURCE_FILE_PATH=" + path},
		Limits:     ResourceLimits{FileBytes: 1 << 20},
	})
	if err == nil || result.ExitCode == 0 {
		t.Fatalf("file-size ceiling was not enforced: result=%+v err=%v", result, err)
	}
}

func TestRunEnforcesCPUTimeLimit(t *testing.T) {
	if os.Getenv("AUTOGIT_RESOURCE_CPU_HELPER") == "1" {
		var value uint64
		for {
			value++
			if value&0xffff == 0 {
				runtime.Gosched()
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	result, err := Run(ctx, Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunEnforcesCPUTimeLimit$"},
		Env:        []string{"AUTOGIT_RESOURCE_CPU_HELPER=1"},
		Limits:     ResourceLimits{CPUTime: 100 * time.Millisecond},
	})
	if err == nil || result.ExitCode == 0 || time.Since(started) >= 3*time.Second {
		t.Fatalf("CPU ceiling was not enforced promptly: result=%+v err=%v", result, err)
	}
}

func TestRunEnforcesAddressSpaceLimit(t *testing.T) {
	if os.Getenv("AUTOGIT_RESOURCE_MEMORY_HELPER") == "1" {
		defer func() {
			if recover() != nil {
				os.Exit(42)
			}
		}()
		value := make([]byte, 256<<20)
		for i := range value {
			value[i] = byte(i)
		}
		os.Exit(0)
	}
	result, err := Run(context.Background(), Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunEnforcesAddressSpaceLimit$"},
		Env:        []string{"AUTOGIT_RESOURCE_MEMORY_HELPER=1"},
		Limits:     ResourceLimits{MemoryBytes: 64 << 20},
	})
	if err == nil || result.ExitCode == 0 {
		t.Fatalf("address-space ceiling was not enforced: result=%+v err=%v", result, err)
	}
}

func TestRunEnforcesProcessLimit(t *testing.T) {
	if os.Getenv("AUTOGIT_RESOURCE_PROCESS_HELPER") == "1" {
		child := exec.Command(os.Args[0], "-test.run=^TestRunEnforcesProcessLimit$")
		child.Env = append(os.Environ(), "AUTOGIT_RESOURCE_PROCESS_CHILD=1")
		if err := child.Start(); err != nil {
			os.Exit(42)
		}
		_ = child.Process.Kill()
		_ = child.Wait()
		os.Exit(0)
	}
	if os.Getenv("AUTOGIT_RESOURCE_PROCESS_CHILD") == "1" {
		return
	}
	result, err := Run(context.Background(), Options{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestRunEnforcesProcessLimit$"},
		Env:        []string{"AUTOGIT_RESOURCE_PROCESS_HELPER=1"},
		Limits:     ResourceLimits{Processes: 1},
	})
	if err == nil || result.ExitCode == 0 {
		t.Fatalf("process ceiling was not enforced: result=%+v err=%v", result, err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("process limit test was cancelled")
	}
}
