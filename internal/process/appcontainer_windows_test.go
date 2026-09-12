//go:build windows

package process

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsAppContainerEnforcesAllowlistAndAttestsFromParent(t *testing.T) {
	if !AppContainerAvailable() {
		t.Fatal("Windows AppContainer APIs are unavailable")
	}
	work := t.TempDir()
	deniedRoot := t.TempDir()
	allowedPath := filepath.Join(work, "allowed.txt")
	deniedPath := filepath.Join(deniedRoot, "denied.txt")
	if err := os.WriteFile(allowedPath, []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deniedPath, []byte("denied"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeDACL, err := windows.GetNamedSecurityInfo(work, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	preflight, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("parent could not reach the preflight listener: %v", err)
	}
	_ = preflight.Close()

	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	env := []string{
		"AUTOGIT_APPCONTAINER_PROBE=1",
		"AUTOGIT_APPCONTAINER_ALLOWED=" + allowedPath,
		"AUTOGIT_APPCONTAINER_DENIED=" + deniedPath,
		"AUTOGIT_APPCONTAINER_NETWORK=" + listener.Addr().String(),
		"PATH=" + filepath.Dir(executable),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := Run(ctx, Options{
		Executable:          executable,
		Dir:                 filepath.Clean(work),
		Env:                 env,
		Args:                []string{"-test.run=^TestWindowsAppContainerProbe$"},
		MaxOutput:           1 << 20,
		SeparateOutput:      true,
		FilesystemAllowlist: []string{filepath.Clean(work)},
		NetworkDisabled:     true,
		AppContainer:        true,
	})
	if err != nil {
		t.Fatalf("AppContainer run failed: %v; stdout=%q; stderr=%q", err, result.Stdout, result.Stderr)
	}
	if result.ExitCode != 0 || result.Stdout != "APPCONTAINER_PROBE_OK\n" {
		t.Fatalf("unexpected AppContainer probe result: %+v", result)
	}
	if result.IsolationAttestation == nil || !result.IsolationAttestation.IndependentlyObserved() {
		t.Fatalf("missing independent parent attestation: %+v", result.IsolationAttestation)
	}
	attestation := result.IsolationAttestation
	if !attestation.AppContainerObserved || !attestation.PackageSIDMatch || !attestation.LowIntegrityObserved || !attestation.NetworkDeniedObserved || attestation.CapabilityCount != 0 {
		t.Fatalf("incomplete AppContainer attestation: %+v", *attestation)
	}
	afterDACL, err := windows.GetNamedSecurityInfo(work, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if beforeDACL.String() != afterDACL.String() {
		t.Fatalf("working-directory DACL was not restored exactly: before=%q after=%q", beforeDACL.String(), afterDACL.String())
	}
}

func TestWindowsAppContainerProbe(t *testing.T) {
	if os.Getenv("AUTOGIT_APPCONTAINER_PROBE") != "1" {
		return
	}
	allowed := os.Getenv("AUTOGIT_APPCONTAINER_ALLOWED")
	denied := os.Getenv("AUTOGIT_APPCONTAINER_DENIED")
	network := os.Getenv("AUTOGIT_APPCONTAINER_NETWORK")
	if value, err := os.ReadFile(allowed); err != nil || string(value) != "allowed" {
		t.Fatalf("allowlisted read failed: %v", err)
	}
	if _, err := os.ReadFile(denied); err == nil {
		t.Fatal("read outside the AppContainer allowlist succeeded")
	}
	writePath := filepath.Join(filepath.Dir(allowed), "write-attempt.txt")
	if err := os.WriteFile(writePath, []byte("must fail"), 0600); err == nil {
		t.Fatal("write in the read-only AppContainer directory succeeded")
	}
	connection, err := net.DialTimeout("tcp", network, time.Second)
	if err == nil {
		_ = connection.Close()
		t.Fatal("network access escaped the AppContainer denial")
	}
	fmt.Println("APPCONTAINER_PROBE_OK")
}
