//go:build windows

package process

import (
	"context"
	"fmt"
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
	executable := probeExecutable
	env := []string{
		"AUTOGIT_APPCONTAINER_ALLOWED=" + allowedPath,
		"AUTOGIT_APPCONTAINER_DENIED=" + deniedPath,
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
		annotateAppContainerFailure(err, result)
		t.Fatalf("AppContainer run failed: %v; stdout=%q; stderr=%q", err, result.Stdout, result.Stderr)
	}
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "APPCONTAINER_PROBE_OK\nPASS" {
		annotateAppContainerFailure(nil, result)
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

func annotateAppContainerFailure(err error, result Result) {
	detail := fmt.Sprintf("error=%v exit=%d stdout=%q stderr=%q", err, result.ExitCode, result.Stdout, result.Stderr)
	detail = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(detail)
	_, _ = fmt.Fprintf(os.Stderr, "::error title=AppContainer probe::%s\n", detail)
}
