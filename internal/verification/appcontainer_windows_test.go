//go:build windows

package verification

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autogit/internal/process"
)

func TestTrustedVerificationRecordsWindowsAppContainerAttestation(t *testing.T) {
	if !process.AppContainerAvailable() {
		t.Fatal("Windows AppContainer APIs are unavailable")
	}
	work := t.TempDir()
	allowed := filepath.Join(work, "allowed.txt")
	if err := os.WriteFile(allowed, []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	registry, err := NewVerifierRegistry([]TrustedVerifierSpec{{
		Name: "windows-appcontainer", Version: "1", Applicable: true,
		Argv:          []string{executable, "-test.run=^TestWindowsTrustedAppContainerProbe$"},
		IsolationTier: TierFilesystemNetworkIsolated, FilesystemAllowlist: []string{filepath.Clean(work)}, NetworkDisabled: true,
		Environment: map[string]string{
			"AUTOGIT_TRUSTED_APPCONTAINER_PROBE":   "1",
			"AUTOGIT_TRUSTED_APPCONTAINER_ALLOWED": allowed,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := TrustedRequest{CandidateDigest: digest('a'), BaseDigest: digest('b'), PolicyDigest: digest('c'), GuardDigest: digest('d'), Dir: filepath.Clean(work), OverallTimeout: time.Second}
	result, err := registry.Verify(context.Background(), VerificationPolicy{Visibility: "private"}, request, ExecRunner{})
	if err != nil || !result.Passed {
		t.Fatalf("trusted AppContainer verification result=%+v err=%v", result, err)
	}
	evidence := result.Evidence[0]
	if evidence.IsolationPrimitive != "appcontainer+job-object" || evidence.IsolationObservations["appcontainer_observed"] != true || evidence.IsolationObservations["package_sid_match"] != true || evidence.IsolationObservations["low_integrity_observed"] != true || evidence.IsolationObservations["network_denied_observed"] != true || evidence.IsolationObservations["capability_count"] != 0 {
		t.Fatalf("trusted evidence omitted AppContainer attestation: %+v", evidence)
	}
	if !result.ValidFor(request, VerificationPolicy{Visibility: "private"}, registry) {
		t.Fatal("trusted AppContainer evidence did not validate for reuse")
	}
}

func TestWindowsTrustedAppContainerProbe(t *testing.T) {
	if os.Getenv("AUTOGIT_TRUSTED_APPCONTAINER_PROBE") != "1" {
		return
	}
	value, err := os.ReadFile(os.Getenv("AUTOGIT_TRUSTED_APPCONTAINER_ALLOWED"))
	if err != nil || string(value) != "allowed" {
		t.Fatalf("allowlisted verifier input unavailable: %v", err)
	}
	fmt.Println("TRUSTED_APPCONTAINER_PROBE_OK")
}
