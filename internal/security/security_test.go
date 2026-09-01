package security

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestScanFindsSecretsWithoutReturningSecretValue(t *testing.T) {
	findings := Scan(map[string][]byte{"config.env": []byte("GH_TOKEN=super-secret-token")})
	if len(findings) != 1 || findings[0].Path != "config.env" || findings[0].Category != "secret" {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].Detail != "secret detected" {
		t.Fatalf("secret leaked in detail: %#v", findings[0])
	}
}

func TestRedactRemovesCredentialsURLsAndControlCharacters(t *testing.T) {
	got := Redact("https://user:password@example.com/repo\nTOKEN=abc123")
	if got == "" || got == "https://user:password@example.com/repo" || got == "TOKEN=abc123" {
		t.Fatalf("not redacted: %q", got)
	}
}

func TestScannerUsesExplicitSnapshotAndDeterministicFindings(t *testing.T) {
	snapshot := CandidateSnapshot{Files: []CandidateFile{
		{Path: "z.env", Content: []byte("AWS_SECRET_ACCESS_KEY=not-a-real-secret")},
		{Path: "a.txt", Content: []byte("<<<<<<< ours\n=======\n>>>>>>> theirs\n")},
	}}
	result := (Scanner{}).Scan(context.Background(), snapshot)
	if !result.Blocked {
		t.Fatalf("expected blocked result: %#v", result)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("findings = %#v", result.Findings)
	}
	if result.Findings[0].Path != "a.txt" || result.Findings[0].Code != ReasonConflictMarker {
		t.Fatalf("findings are not sorted or coded: %#v", result.Findings)
	}
	if result.Findings[1].Path != "z.env" || result.Findings[1].Code != ReasonSecretFilename {
		t.Fatalf("findings are not sorted or coded: %#v", result.Findings)
	}
	if strings.Contains(result.Findings[1].Detail, "not-a-real-secret") {
		t.Fatal("finding exposed content")
	}
}

func TestScannerRejectsDuplicateTraversalAndSymlinkPaths(t *testing.T) {
	cases := []CandidateSnapshot{
		{Files: []CandidateFile{{Path: "a", Content: []byte("x")}, {Path: "a", Content: []byte("y")}}},
		{Files: []CandidateFile{{Path: "../outside", Content: []byte("x")}}},
		{Files: []CandidateFile{{Path: "link", Symlink: true, Content: []byte("x")}}},
	}
	for i, snapshot := range cases {
		result := (Scanner{}).Scan(context.Background(), snapshot)
		if !result.Blocked || len(result.ReasonCodes) == 0 {
			t.Errorf("case %d was accepted: %#v", i, result)
		}
	}
}

func TestScannerEnforcesFileAndTotalByteLimits(t *testing.T) {
	snapshot := CandidateSnapshot{Files: []CandidateFile{
		{Path: "a", Content: []byte("1234")},
		{Path: "b", Content: []byte("5678")},
	}}
	result := (Scanner{Limits: Limits{MaxFiles: 1, MaxFileBytes: 3, MaxTotalBytes: 5}}).Scan(context.Background(), snapshot)
	if !result.Blocked || len(result.ReasonCodes) == 0 {
		t.Fatalf("limits were not enforced: %#v", result)
	}
	if result.ReasonCodes[0] != ReasonFileBytes {
		t.Fatalf("expected stable first limit reason, got %#v", result.ReasonCodes)
	}
}

func TestScannerBinaryPolicyIsExplicitAndDoesNotLeakBytes(t *testing.T) {
	snapshot := CandidateSnapshot{Files: []CandidateFile{{Path: "blob.bin", Content: []byte{'x', 0, 'y'}}}}
	rejected := (Scanner{}).Scan(context.Background(), snapshot)
	if !rejected.Blocked || rejected.Findings[0].Code != ReasonBinaryContent {
		t.Fatalf("default binary policy was not fail-closed: %#v", rejected)
	}
	scanned := (Scanner{BinaryPolicy: BinaryScan}).Scan(context.Background(), snapshot)
	if !scanned.Blocked || !hasReason(scanned.ReasonCodes, ReasonBinaryUnverified) {
		t.Fatalf("binary scan must remain blocked until independently verified: %#v", scanned)
	}
	for _, finding := range scanned.Findings {
		if strings.Contains(finding.Detail, "x") || strings.Contains(finding.Detail, "y") {
			t.Fatal("binary finding should contain only stable metadata")
		}
	}
}

func TestScannerFindsEntropyOnlyWithSecretContextAndSuppressesPlaceholders(t *testing.T) {
	secret := "token=Qx7!mN2@pL9#vC4$zR8%kT6^"
	snapshot := CandidateSnapshot{Files: []CandidateFile{
		{Path: "config.txt", Content: []byte(secret)},
		{Path: "example.txt", Content: []byte("token=changeme-example-token")},
		{Path: "hash.txt", Content: []byte("checksum=0123456789abcdef0123456789abcdef")},
	}}
	result := (Scanner{}).Scan(context.Background(), snapshot)
	foundEntropy := false
	for _, finding := range result.Findings {
		if finding.Path == "config.txt" && finding.Code == ReasonSecretEntropy {
			foundEntropy = true
		}
		if finding.Path == "example.txt" || finding.Path == "hash.txt" {
			t.Fatalf("false positive finding: %#v", finding)
		}
	}
	if !foundEntropy {
		t.Fatalf("entropy secret was missed: %#v", result)
	}
}

func TestScannerFindsEncodedCredentialWithoutReturningDecodedValue(t *testing.T) {
	// Base64 of a credential assignment; the decoded bytes must not appear in
	// any result metadata.
	snapshot := CandidateSnapshot{Files: []CandidateFile{{Path: "encoded.txt", Content: []byte("R0hfVE9LRU49c2VjcmV0LWJsb2ItdmFsdWU=")}}}
	result := (Scanner{}).Scan(context.Background(), snapshot)
	if !result.Blocked {
		t.Fatalf("encoded credential was accepted: %#v", result)
	}
	for _, finding := range result.Findings {
		if strings.Contains(finding.Detail, "secret-blob-value") || strings.Contains(finding.Detail, "GH_TOKEN") {
			t.Fatalf("decoded credential leaked in finding: %#v", finding)
		}
	}
}

func TestScannerRejectsSpecialModes(t *testing.T) {
	for _, mode := range []uint32{0120000, uint32(0020000), uint32(04000), uint32(020000000000)} {
		result := (Scanner{}).Scan(context.Background(), CandidateSnapshot{Files: []CandidateFile{{Path: "entry", Mode: mode}}})
		if !result.Blocked || !hasReason(result.ReasonCodes, ReasonMode) && !hasReason(result.ReasonCodes, ReasonSymlink) {
			t.Fatalf("special mode %#o was accepted: %#v", mode, result)
		}
	}
}

func TestScannerHonorsCancellationAndTimeBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := (Scanner{}).Scan(ctx, CandidateSnapshot{Files: []CandidateFile{{Path: "a", Content: []byte("x")}}})
	if !result.Blocked || result.ReasonCodes[0] != ReasonCancelled {
		t.Fatalf("cancellation was not deterministic: %#v", result)
	}
	deadline := time.Now().Add(-time.Second)
	expired, deadlineCancel := context.WithDeadline(context.Background(), deadline)
	defer deadlineCancel()
	result = (Scanner{}).Scan(expired, CandidateSnapshot{Files: []CandidateFile{{Path: "a", Content: []byte("x")}}})
	if !result.Blocked || result.ReasonCodes[0] != ReasonTimeBudget {
		t.Fatalf("expired deadline was not enforced: %#v", result)
	}
}

func hasReason(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}
