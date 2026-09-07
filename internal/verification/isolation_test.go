package verification

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"autogit/internal/process"
)

func TestIsolationCapabilitiesAreExplicitAndFailClosed(t *testing.T) {
	if got := DefaultIsolationTier(); got != TierProcessBounded {
		t.Fatalf("default isolation=%q", got)
	}
	for _, tier := range []IsolationTier{TierNone, TierProcessBounded, TierFilesystemIsolated, TierFilesystemNetworkIsolated, TierRemoteHermetic} {
		capability := CapabilityFor(tier)
		if capability.Tier != tier || capability.Reason == "" {
			t.Fatalf("incomplete capability for %q: %+v", tier, capability)
		}
	}
	if _, err := RequireCapability(TierFilesystemNetworkIsolated); err == nil {
		t.Fatal("unavailable filesystem/network tier accepted")
	}
}

func TestTrustedRegistryRecordsAchievedProcessBoundedTier(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "verifier")
	if err := os.WriteFile(executable, []byte("not executed"), 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "bounded", Version: "1", Argv: []string{executable}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := registry.Select(VerificationPolicy{Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Specs) != 1 || plan.Specs[0].IsolationTier != TierProcessBounded {
		t.Fatalf("spec tier=%+v", plan.Specs)
	}

	request := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir(), OverallTimeout: time.Second}
	runner := &trustedRecordingRunner{}
	result, err := registry.Verify(context.Background(), VerificationPolicy{Visibility: "private"}, request, runner)
	if err != nil || !result.Passed || result.Evidence[0].IsolationTier != TierProcessBounded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProcessResourceLimitsDoNotChangeUnboundedCompatibility(t *testing.T) {
	result, err := process.Run(context.Background(), process.Options{Executable: "true", Limits: process.ResourceLimits{CPUTime: time.Second, MemoryBytes: 64 << 20, FileBytes: 1 << 20, Processes: 8}})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("bounded process result=%+v err=%v", result, err)
	}
}
