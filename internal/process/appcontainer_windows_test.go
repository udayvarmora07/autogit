//go:build windows

package process

import (
	"context"
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
	securityInformation := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	beforeDescriptor, err := windows.GetNamedSecurityInfo(work, windows.SE_FILE_OBJECT, securityInformation)
	if err != nil {
		t.Fatal(err)
	}
	executable := probeExecutable
	env := []string{
		"AUTOGIT_APPCONTAINER_ALLOWED=" + allowedPath,
		"AUTOGIT_APPCONTAINER_DENIED=" + deniedPath,
		"PATH=" + filepath.Dir(executable),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	afterDescriptor, err := windows.GetNamedSecurityInfo(work, windows.SE_FILE_OBJECT, securityInformation)
	if err != nil {
		t.Fatal(err)
	}
	beforeOwner, beforeOwnerDefaulted, err := beforeDescriptor.Owner()
	if err != nil {
		t.Fatalf("read original working-directory owner: %v", err)
	}
	afterOwner, afterOwnerDefaulted, err := afterDescriptor.Owner()
	if err != nil {
		t.Fatalf("read restored working-directory owner: %v", err)
	}
	if appContainerSIDString(beforeOwner) != appContainerSIDString(afterOwner) || beforeOwnerDefaulted != afterOwnerDefaulted {
		t.Fatalf("working-directory owner was not restored: before=%q defaulted=%t after=%q defaulted=%t", appContainerSIDString(beforeOwner), beforeOwnerDefaulted, appContainerSIDString(afterOwner), afterOwnerDefaulted)
	}
	beforeGroup, beforeGroupDefaulted, err := beforeDescriptor.Group()
	if err != nil {
		t.Fatalf("read original working-directory group: %v", err)
	}
	afterGroup, afterGroupDefaulted, err := afterDescriptor.Group()
	if err != nil {
		t.Fatalf("read restored working-directory group: %v", err)
	}
	if appContainerSIDString(beforeGroup) != appContainerSIDString(afterGroup) || beforeGroupDefaulted != afterGroupDefaulted {
		t.Fatalf("working-directory group was not restored: before=%q defaulted=%t after=%q defaulted=%t", appContainerSIDString(beforeGroup), beforeGroupDefaulted, appContainerSIDString(afterGroup), afterGroupDefaulted)
	}
	beforeControl, _, err := beforeDescriptor.Control()
	if err != nil {
		t.Fatalf("read original working-directory security-descriptor control: %v", err)
	}
	afterControl, _, err := afterDescriptor.Control()
	if err != nil {
		t.Fatalf("read restored working-directory security-descriptor control: %v", err)
	}
	const daclControlMask = windows.SECURITY_DESCRIPTOR_CONTROL(windows.SE_DACL_PRESENT | windows.SE_DACL_DEFAULTED | windows.SE_DACL_AUTO_INHERIT_REQ | windows.SE_DACL_PROTECTED)
	if beforeControl&daclControlMask != afterControl&daclControlMask {
		t.Fatalf("working-directory DACL protection state was not restored: before=%#x after=%#x", beforeControl&daclControlMask, afterControl&daclControlMask)
	}
	beforeDACL := normalizeAppContainerDACL(beforeDescriptor.String())
	afterDACL := normalizeAppContainerDACL(afterDescriptor.String())
	if beforeDACL != afterDACL {
		t.Fatalf("working-directory DACL ACEs were not restored: before=%q after=%q", beforeDACL, afterDACL)
	}
}

func appContainerSIDString(sid *windows.SID) string {
	if sid == nil {
		return ""
	}
	return sid.String()
}

func normalizeAppContainerDACL(sddl string) string {
	daclStart := strings.Index(sddl, "D:")
	if daclStart < 0 {
		return sddl
	}
	dacl := sddl[daclStart:]
	if saclStart := strings.Index(dacl, "S:"); saclStart >= 0 {
		dacl = dacl[:saclStart]
	}
	if strings.HasPrefix(dacl, "D:AI") {
		dacl = "D:" + strings.TrimPrefix(dacl, "D:AI")
	}
	return dacl
}
