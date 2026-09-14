//go:build windows

package process

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate the AppContainer test source")
	}
	probeSource := filepath.Join(filepath.Dir(testFile), "testdata", "appcontainerprobe", "main.go")
	probeExecutable := filepath.Join(work, "autogit-appcontainer-probe.exe")
	build := exec.Command("go", "build", "-o", probeExecutable, probeSource)
	build.Dir = filepath.Dir(testFile)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build AppContainer probe: %v; output=%q", err, output)
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

	executable := probeExecutable
	env := []string{
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
		Args:                nil,
		MaxOutput:           1 << 20,
		SeparateOutput:      true,
		FilesystemAllowlist: []string{filepath.Clean(work)},
		NetworkDisabled:     true,
		AppContainer:        true,
	})
	if err != nil {
		t.Fatalf("AppContainer run failed: %v; stdout=%q; stderr=%q", err, result.Stdout, result.Stderr)
	}
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "APPCONTAINER_PROBE_OK\nPASS" {
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
