package publication

import (
	"encoding/json"
	"strings"
	"testing"
)

func digest(ch byte) string {
	if ch == 'z' {
		ch = 'c'
	}
	if ch < 'a' || ch > 'f' {
		ch = 'a' + (ch-'a')%6
	}
	return "sha256:" + strings.Repeat(string(ch), 64)
}

func validRequest() Request {
	return Request{
		Mode:                 ModePublic,
		FirstPublication:     true,
		PublicConsent:        true,
		CandidateDigest:      digest('a'),
		BaseDigest:           digest('e'),
		PolicyDigest:         digest('b'),
		GuardDigest:          digest('f'),
		VerifierSetDigest:    digest('g'),
		Destination:          Destination{Provider: "github", Host: "github.com", Owner: "acme", Repository: "widget", Visibility: VisibilityPublic, Ref: "refs/heads/main"},
		ObservedDestination:  Destination{Provider: "github", Host: "github.com", Owner: "acme", Repository: "widget", Visibility: VisibilityPublic, Ref: "refs/heads/main"},
		DestinationConfirmed: true,
		Files:                []FileMetadata{{Path: "README.md", Bytes: 120}, {Path: "LICENSE", Bytes: 80}, {Path: "cmd/main.go", Bytes: 200}},
		CandidateScan:        ScanEvidence{Scope: ScanCandidate, CandidateDigest: digest('a'), PolicyDigest: digest('b'), Passed: true, Digest: digest('c')},
		HistoryScan:          ScanEvidence{Scope: ScanHistory, CandidateDigest: digest('a'), PolicyDigest: digest('b'), Passed: true, Digest: digest('d')},
		Verification:         VerificationEvidence{CandidateDigest: digest('a'), BaseDigest: digest('e'), PolicyDigest: digest('b'), GuardDigest: digest('f'), VerifierSetDigest: digest('g'), Passed: true, Required: 2, PassedCount: 2, Digest: digest('h')},
		License:              LicenseEvidence{Selected: "MIT", FilePath: "LICENSE", Present: true},
		README:               READMEInput{Path: "README.md", Content: []byte("# Widget\n\n## Usage\nRun `widget`.\n")},
		Readiness:            Readiness{Tests: StatusPassed, CI: StatusPresent},
		WorkflowSolo:         true,
	}
}

func TestPreflightAllowsCompleteFirstPublicPublication(t *testing.T) {
	r := Evaluate(validRequest())
	if !r.Ready || !r.PublicAuthorized || len(r.ReasonCodes) != 0 {
		t.Fatalf("expected ready public report, got %+v", r)
	}
	if !strings.HasPrefix(r.Digest, "sha256:") {
		t.Fatalf("missing canonical digest: %q", r.Digest)
	}
}

func TestPreflightReportUsesStableLowercaseJSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(Evaluate(validRequest()))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"mode"`, `"ready"`, `"public_authorized"`, `"destination"`, `"reason_codes"`, `"digest"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("field %s missing from report JSON: %s", field, text)
		}
	}
	if strings.Contains(text, `"Mode"`) || strings.Contains(text, `"PublicAuthorized"`) {
		t.Fatalf("report exposed Go field names: %s", text)
	}
}

func TestPreflightRequiresExplicitPublicConsent(t *testing.T) {
	req := validRequest()
	req.PublicConsent = false
	r := Evaluate(req)
	if r.Ready || r.PublicAuthorized || !hasReason(r, ReasonPublicConsentRequired) {
		t.Fatalf("consent was not fail-closed: %+v", r)
	}
}

func TestPreflightRejectsMissingDestinationIdentity(t *testing.T) {
	req := validRequest()
	req.Destination.Repository = ""
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonDestinationIdentityRequired) {
		t.Fatalf("missing destination accepted: %+v", r)
	}
}

func TestPreflightRejectsWrongDestination(t *testing.T) {
	req := validRequest()
	req.ObservedDestination.Repository = "other"
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonDestinationMismatch) {
		t.Fatalf("wrong destination accepted: %+v", r)
	}
}

func TestPreflightRejectsUnconfirmedDestination(t *testing.T) {
	req := validRequest()
	req.DestinationConfirmed = false
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonDestinationUnconfirmed) {
		t.Fatalf("unconfirmed destination accepted: %+v", r)
	}
}

func TestPreflightRejectsStaleCandidateScan(t *testing.T) {
	req := validRequest()
	req.CandidateScan.CandidateDigest = digest('z')
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonCandidateScanStale) {
		t.Fatalf("stale candidate scan accepted: %+v", r)
	}
}

func TestPreflightRejectsStaleHistoryScan(t *testing.T) {
	req := validRequest()
	req.HistoryScan.PolicyDigest = digest('z')
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonHistoryScanStale) {
		t.Fatalf("stale history scan accepted: %+v", r)
	}
}

func TestPreflightRejectsFailedScan(t *testing.T) {
	req := validRequest()
	req.CandidateScan.Passed = false
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonCandidateScanFailed) {
		t.Fatalf("failed scan accepted: %+v", r)
	}
}

func TestPreflightRejectsStaleVerifierEvidence(t *testing.T) {
	req := validRequest()
	req.Verification.GuardDigest = digest('z')
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonVerificationStale) {
		t.Fatalf("stale verification accepted: %+v", r)
	}
}

func TestPreflightRejectsIncompleteVerifierSet(t *testing.T) {
	req := validRequest()
	req.Verification.PassedCount = 1
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonVerificationIncomplete) {
		t.Fatalf("incomplete verification accepted: %+v", r)
	}
}

func TestPreflightRequiresExplicitLicenseAndFile(t *testing.T) {
	req := validRequest()
	req.License = LicenseEvidence{Selected: "", FilePath: "", Present: false}
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonLicenseRequired) {
		t.Fatalf("license omission accepted: %+v", r)
	}
}

func TestPreflightRejectsExplicitNoLicense(t *testing.T) {
	req := validRequest()
	req.License = LicenseEvidence{Selected: "none", FilePath: "", Present: false}
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonLicenseRequired) {
		t.Fatalf("explicit no-license accepted: %+v", r)
	}
}

func TestPreflightDetectsPlaceholderREADME(t *testing.T) {
	req := validRequest()
	req.README.Content = []byte("# TODO\nDescribe your project here.\n")
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonREADMEPlaceholder) || !r.README.Placeholder {
		t.Fatalf("placeholder not blocked/detected: %+v", r)
	}
}

func TestPreflightRequiresREADMEUsageGuidance(t *testing.T) {
	req := validRequest()
	req.README.Content = []byte("# Widget\nA useful tool.\n")
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonREADMEUsageMissing) {
		t.Fatalf("README without usage accepted: %+v", r)
	}
}

func TestPreflightRequiresNonemptyFileSummary(t *testing.T) {
	req := validRequest()
	req.Files = nil
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonFileSummaryRequired) {
		t.Fatalf("empty file summary accepted: %+v", r)
	}
}

func TestPrivateReportCannotAuthorizePublic(t *testing.T) {
	req := validRequest()
	req.Mode = ModePrivate
	req.FirstPublication = false
	req.PublicConsent = false
	r := Evaluate(req)
	if !r.Ready || r.PublicAuthorized || r.Mode != ModePrivate {
		t.Fatalf("private report crossed public boundary: %+v", r)
	}
}

func TestLocalReportCannotAuthorizePublic(t *testing.T) {
	req := validRequest()
	req.Mode = ModeLocal
	req.FirstPublication = false
	req.PublicConsent = false
	r := Evaluate(req)
	if !r.Ready || r.PublicAuthorized || r.Mode != ModeLocal {
		t.Fatalf("local report crossed public boundary: %+v", r)
	}
	public := Evaluate(validRequest())
	if r.Digest == public.Digest {
		t.Fatal("local and public reports reused an authorization digest")
	}
}

func TestPreflightDeterministicOrderingAndDigest(t *testing.T) {
	a := validRequest()
	a.Files[0], a.Files[1] = a.Files[1], a.Files[0]
	b := validRequest()
	ra, rb := Evaluate(a), Evaluate(b)
	if ra.Digest != rb.Digest || strings.Join(ra.ReasonCodes, ",") != strings.Join(rb.ReasonCodes, ",") {
		t.Fatalf("reports are not deterministic: %q vs %q", ra.Digest, rb.Digest)
	}
}

func TestPreflightReportIsBoundedAndMetadataOnly(t *testing.T) {
	req := validRequest()
	req.README.Content = []byte("# Widget\n## Usage\nsecret-source-content")
	r := Evaluate(req)
	if len(r.Files) > MaxReportedFiles || len(r.ReasonCodes) > MaxReasonCodes {
		t.Fatalf("report not bounded: %+v", r)
	}
	if strings.Contains(string(r.README.ContentDigest), "secret-source-content") || strings.Contains(r.Digest, "secret-source-content") {
		t.Fatal("raw content leaked")
	}
}

func TestSafeWorkflowRequiresApprovedFeatureBranch(t *testing.T) {
	req := validRequest()
	req.WorkflowSolo = false
	req.WorkflowSafe = true
	req.Destination.Ref = "refs/heads/main"
	req.ObservedDestination.Ref = req.Destination.Ref
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonFeatureBranchRequired) {
		t.Fatalf("safe workflow allowed protected current branch: %+v", r)
	}
}

func TestSafeWorkflowAcceptsApprovedFeatureBranch(t *testing.T) {
	req := validRequest()
	req.WorkflowSolo = false
	req.WorkflowSafe = true
	req.FeatureBranchApproved = true
	req.Destination.Ref = "refs/heads/autogit/feature-1"
	req.ObservedDestination.Ref = req.Destination.Ref
	r := Evaluate(req)
	if !r.Ready {
		t.Fatalf("approved feature branch blocked: %+v", r)
	}
}

func TestProtectedBranchStatusFailureBlocks(t *testing.T) {
	req := validRequest()
	req.ProtectedBranch = true
	req.StatusChecksRequired = true
	req.StatusChecksPassed = false
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonStatusChecksRequired) {
		t.Fatalf("status failure bypassed: %+v", r)
	}
}

func TestDestinationRejectsOptionLikeRefAndTraversal(t *testing.T) {
	for _, ref := range []string{"--all", "refs/heads/a/../b", "refs/heads/a//b"} {
		req := validRequest()
		req.Destination.Ref = ref
		req.ObservedDestination.Ref = ref
		r := Evaluate(req)
		if r.Ready || !hasReason(r, ReasonDestinationIdentityRequired) {
			t.Fatalf("unsafe ref accepted (%q): %+v", ref, r)
		}
	}
}

func TestEvidenceDigestFieldsMustBeCanonical(t *testing.T) {
	req := validRequest()
	req.Verification.GuardDigest = "not-a-digest"
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonVerificationStale) {
		t.Fatalf("noncanonical guard digest accepted: %+v", r)
	}
}

func TestPublicReadinessGapsBlockFirstPublication(t *testing.T) {
	for _, readiness := range []Readiness{{Tests: StatusAbsent, CI: StatusPresent}, {Tests: StatusPassed, CI: StatusFailed}} {
		req := validRequest()
		req.Readiness = readiness
		r := Evaluate(req)
		if r.Ready {
			t.Fatalf("readiness gap was not blocked: %+v", r)
		}
	}
}

func TestLicenseAndREADMEMustBeInFileSummary(t *testing.T) {
	req := validRequest()
	req.Files = []FileMetadata{{Path: "cmd/main.go", Bytes: 2}}
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonFileSummaryRequired) {
		t.Fatalf("uncorroborated metadata accepted: %+v", r)
	}
}

func TestEvaluateSnapshotsMutableRequest(t *testing.T) {
	req := validRequest()
	r := Evaluate(req)
	req.Files[0].Path = "changed"
	req.README.Content[0] = 'X'
	for _, file := range r.Files {
		if file.Path == "changed" {
			t.Fatal("report retained mutable file input")
		}
	}
	if Evaluate(validRequest()).Digest != r.Digest {
		t.Fatal("baseline unexpectedly changed")
	}
}

func TestReportJSONContainsOnlyMetadata(t *testing.T) {
	req := validRequest()
	secret := "TOP-SECRET-SOURCE-CONTENT"
	req.README.Content = []byte("# Widget\n## Usage\n" + secret)
	b, err := json.Marshal(Evaluate(req))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatal("report serialized raw README content")
	}
}

func TestUnsafeFileMetadataBlocksPublicPreflight(t *testing.T) {
	req := validRequest()
	req.Files = append(req.Files, FileMetadata{Path: "../outside", Bytes: 1})
	r := Evaluate(req)
	if r.Ready || !hasReason(r, ReasonFileMetadataInvalid) {
		t.Fatalf("unsafe path accepted: %+v", r)
	}
}

func hasReason(r Report, want string) bool {
	for _, got := range r.ReasonCodes {
		if got == want {
			return true
		}
	}
	return false
}
