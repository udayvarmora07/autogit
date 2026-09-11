package process

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunUsesOpenedExecutableWhenThePathIsReplaced(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor execution is implemented on Linux only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "autogit-helper")
	contents, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, []byte("#!/bin/sh\nexit 97\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Options{
		Executable:     path,
		ExecutableFile: file,
		Args:           []string{"-test.run=^$"},
		Env:            []string{"GO_TESTING=1"},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("opened executable was not used: result=%+v err=%v", result, err)
	}
}

func TestRunEnforcesFilesystemAllowlistAndNetworkDenial(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux namespace enforcement is required for this test")
	}
	if _, err := sandboxExecutable(); err != nil {
		t.Skipf("filesystem/network sandbox unavailable: %v", err)
	}
	work := t.TempDir()
	allowed := filepath.Join(work, "allowed.txt")
	denied := filepath.Join(t.TempDir(), "denied.txt")
	if err := os.WriteFile(allowed, []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(denied, []byte("denied"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result, err := Run(context.Background(), Options{
		Executable: os.Args[0],
		Dir:        work,
		Env: []string{
			"AUTOGIT_SANDBOX_PROBE=1",
			"AUTOGIT_SANDBOX_ALLOWED=" + allowed,
			"AUTOGIT_SANDBOX_DENIED=" + denied,
			"AUTOGIT_SANDBOX_TARGET=" + listener.Addr().String(),
		},
		Args:                []string{"-test.run=^TestSandboxProbe$"},
		FilesystemAllowlist: []string{work, filepath.Dir(os.Args[0])},
		NetworkDisabled:     true,
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("sandbox probe failed: result=%+v err=%v", result, err)
	}
}

func TestRunRejectsNestedUserNamespaceCreation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("nested user-namespace lockout is Linux-specific")
	}
	if _, err := sandboxExecutable(); err != nil || !NamespaceSandboxAvailable() {
		t.Skip("Linux namespace sandbox unavailable")
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare is not installed")
	}
	unshare, err = filepath.EvalSymlinks(unshare)
	if err != nil {
		t.Skipf("unshare path is unavailable: %v", err)
	}
	work := t.TempDir()
	result, runErr := Run(context.Background(), Options{
		Executable:          unshare,
		Dir:                 work,
		Args:                []string{"--user", "--map-root-user", "/usr/bin/true"},
		FilesystemAllowlist: []string{work, "/usr"},
	})
	if runErr == nil || result.ExitCode == 0 {
		t.Fatalf("nested user namespace creation was not rejected: result=%+v err=%v", result, runErr)
	}
}

func TestSandboxProbe(t *testing.T) {
	if os.Getenv("AUTOGIT_SANDBOX_PROBE") != "1" {
		return
	}
	allowed, err := os.ReadFile(os.Getenv("AUTOGIT_SANDBOX_ALLOWED"))
	if err != nil || string(allowed) != "allowed" {
		t.Fatalf("allowed file unavailable: %q %v", allowed, err)
	}
	if _, err := os.ReadFile(os.Getenv("AUTOGIT_SANDBOX_DENIED")); err == nil {
		t.Fatal("filesystem allowlist permitted an unlisted file")
	}
	connection, err := net.Dial("tcp", os.Getenv("AUTOGIT_SANDBOX_TARGET"))
	if err == nil {
		_ = connection.Close()
		t.Fatal("network namespace permitted a host connection")
	}
}

func TestValidateResourceLimitsRejectsInvalidCeilings(t *testing.T) {
	for _, limits := range []ResourceLimits{
		{CPUTime: -1},
		{MemoryBytes: 1},
		{FileBytes: 1},
		{Processes: 1025},
	} {
		if err := ValidateResourceLimits(limits); err == nil {
			t.Fatalf("invalid limits accepted: %+v", limits)
		}
	}
}

func TestRunRejectsSandboxWithoutAnAbsoluteWorkingDirectory(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Executable:          os.Args[0],
		FilesystemAllowlist: []string{"/tmp"},
	})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("invalid sandbox request accepted: %v", err)
	}
}
