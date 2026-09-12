package verification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	capability := CapabilityFor(TierFilesystemNetworkIsolated)
	if runtime.GOOS == "linux" && process.NamespaceSandboxAvailable() {
		if !capability.Available || !capability.Enforced {
			t.Fatalf("Linux namespace tier was not advertised as enforced: %+v", capability)
		}
		wantPrimitive := "bubblewrap-user-pid-namespace+network-namespace"
		if process.LandlockAvailable() {
			wantPrimitive += "+landlock-ruleset"
			if capability.Observations["landlock_enforced"] != true {
				t.Fatalf("Landlock was not recorded as enforced: %+v", capability.Observations)
			}
		} else if capability.Observations["landlock_enforced"] != false {
			t.Fatalf("Landlock was overstated: %+v", capability.Observations)
		}
		if capability.Primitive != wantPrimitive {
			t.Fatalf("Linux namespace primitive=%q, want %q", capability.Primitive, wantPrimitive)
		}
		if capability.Observations["nested_user_namespaces"] != "disabled-and-asserted" {
			t.Fatalf("nested user-namespace lockout was not recorded: %+v", capability.Observations)
		}
	} else if runtime.GOOS == "windows" && process.AppContainerAvailable() {
		if !capability.Available || !capability.Enforced || capability.Primitive != "appcontainer+job-object" || capability.Observations["parent_token_attestation"] != "required" {
			t.Fatalf("Windows AppContainer tier was not advertised with independent attestation: %+v", capability)
		}
	} else if _, err := RequireCapability(TierFilesystemNetworkIsolated); err == nil {
		t.Fatal("unavailable filesystem/network tier accepted")
	}
}

func TestIsolationCapabilityReportsPlatformFallbacks(t *testing.T) {
	processBounded := CapabilityFor(TierProcessBounded)
	if processBounded.Primitive == "" || processBounded.Reason == "" {
		t.Fatalf("process-bounded capability omitted its primitive: %+v", processBounded)
	}
	switch runtime.GOOS {
	case "darwin":
		if len(processBounded.Limitations) == 0 {
			t.Fatal("macOS process-bounded fallback omitted its limitations")
		}
	case "windows":
		if processBounded.Primitive != "job-object" || len(processBounded.Limitations) == 0 {
			t.Fatalf("Windows job/AppContainer boundary was not explicit: %+v", processBounded)
		}
	case "linux":
		if processBounded.Primitive != "process-group+prlimit" {
			t.Fatalf("Linux process-bounded primitive=%q", processBounded.Primitive)
		}
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
	wantPrimitive := map[string]string{
		"linux":   "process-group+prlimit",
		"darwin":  "process-group",
		"windows": "job-object",
	}[runtime.GOOS]
	if wantPrimitive == "" {
		t.Fatalf("test does not define a process primitive for %s", runtime.GOOS)
	}
	if result.Evidence[0].IsolationPrimitive != wantPrimitive {
		t.Fatalf("recording runner primitive=%q, want %q", result.Evidence[0].IsolationPrimitive, wantPrimitive)
	}
	if result.Evidence[0].ExecutableBinding != "digest-rechecked-path" {
		t.Fatalf("recording runner binding=%q", result.Evidence[0].ExecutableBinding)
	}
}

func TestProcessResourceLimitsPreserveUnboundedCompatibility(t *testing.T) {
	requested := process.ResourceLimits{CPUTime: time.Second, MemoryBytes: 64 << 20, FileBytes: 1 << 20, Processes: 8}
	if runtime.GOOS != "linux" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		result, runErr := process.Run(context.Background(), process.Options{Executable: executable, Args: []string{"-test.run=^$"}})
		if runErr != nil || result.ExitCode != 0 {
			t.Fatalf("unbounded process result=%+v err=%v", result, runErr)
		}
		if limitErr := process.ValidateResourceLimits(requested); !errors.Is(limitErr, process.ErrUnsupportedResourceLimit) {
			t.Fatalf("platform accepted unsupported resource limits: %v", limitErr)
		}
		return
	}
	result, err := process.Run(context.Background(), process.Options{Executable: "true", Limits: requested})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("bounded process result=%+v err=%v", result, err)
	}
}

func TestVerifierUsesFilesystemIsolatedRunnerForConfiguredTier(t *testing.T) {
	if runtime.GOOS != "linux" || !process.NamespaceSandboxAvailable() {
		t.Skip("Linux bubblewrap sandbox unavailable")
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
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewVerifierRegistry([]TrustedVerifierSpec{{
		Name: "isolated", Version: "1", Applicable: true,
		Argv:          []string{executable, "-test.run=^TestVerifierSandboxProbe$"},
		IsolationTier: TierFilesystemIsolated, FilesystemAllowlist: []string{work},
		Environment: map[string]string{
			"AUTOGIT_VERIFIER_SANDBOX": "1", "AUTOGIT_VERIFIER_ALLOWED": allowed, "AUTOGIT_VERIFIER_DENIED": denied,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	request := TrustedRequest{CandidateDigest: digest('a'), BaseDigest: digest('b'), PolicyDigest: digest('c'), GuardDigest: digest('d'), Dir: work}
	result, err := registry.Verify(context.Background(), VerificationPolicy{Visibility: "private"}, request, ExecRunner{})
	if err != nil || !result.Passed || result.Evidence[0].IsolationTier != TierFilesystemIsolated {
		t.Fatalf("isolated verifier result=%+v err=%v", result, err)
	}
	if result.Evidence[0].ExecutableBinding != "opened-executable-descriptor" {
		t.Fatalf("isolated verifier binding=%q", result.Evidence[0].ExecutableBinding)
	}
}

func TestVerifierEvidenceReportsDescriptorBindingWhenRunnerSupportsIt(t *testing.T) {
	if runtime.GOOS != "linux" || !process.ExecutableBindingAvailable() {
		t.Skip("opened executable descriptors are implemented on Linux only")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewVerifierRegistry([]TrustedVerifierSpec{{
		Name: "descriptor", Version: "1", Applicable: true,
		Argv: []string{executable, "-test.run=^TestVerifierSandboxProbe$"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	request := TrustedRequest{CandidateDigest: digest('a'), BaseDigest: digest('b'), PolicyDigest: digest('c'), GuardDigest: digest('d'), Dir: t.TempDir()}
	result, err := registry.Verify(context.Background(), VerificationPolicy{Visibility: "private"}, request, ExecRunner{})
	if err != nil || !result.Passed || result.Evidence[0].ExecutableBinding != "opened-executable-descriptor" {
		t.Fatalf("descriptor evidence=%+v err=%v", result, err)
	}
}

func TestVerifierSandboxProbe(t *testing.T) {
	if os.Getenv("AUTOGIT_VERIFIER_SANDBOX") != "1" {
		return
	}
	allowed, err := os.ReadFile(os.Getenv("AUTOGIT_VERIFIER_ALLOWED"))
	if err != nil || string(allowed) != "allowed" {
		t.Fatalf("allowed file unavailable: %q %v", allowed, err)
	}
	if _, err := os.ReadFile(os.Getenv("AUTOGIT_VERIFIER_DENIED")); err == nil {
		t.Fatal("verifier filesystem sandbox exposed an unallowlisted file")
	}
}

func TestRegistryRejectsIsolationControlsOnProcessBoundedTier(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewVerifierRegistry([]TrustedVerifierSpec{{Name: "bad", Version: "1", Applicable: true, Argv: []string{executable}, FilesystemAllowlist: []string{t.TempDir()}}})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("process-bounded verifier accepted namespace controls: %v", err)
	}
}
