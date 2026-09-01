// Package publication contains the local, side-effect-free publication
// preflight. It deliberately has no provider, Git, or network dependency.
package publication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	ModePublic  = "public"
	ModePrivate = "private"
	ModeLocal   = "local"

	VisibilityPublic  = "public"
	VisibilityPrivate = "private"

	ScanCandidate = "candidate"
	ScanHistory   = "current-history"

	StatusUnknown = "unknown"
	StatusAbsent  = "absent"
	StatusPresent = "present"
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	WorkflowSafe  = "safe"
	WorkflowSolo  = "solo"

	MaxReportedFiles = 128
	MaxReasonCodes   = 64
	maxReportedPath  = 256
	maxREADMEBytes   = 1 << 20
	maxInputFiles    = MaxReportedFiles * 32
)

// Reason codes are stable machine-readable publication outcomes.
const (
	ReasonInvalidMode                 = "PUB_INVALID_MODE"
	ReasonPublicConsentRequired       = "PUB_PUBLIC_CONSENT_REQUIRED"
	ReasonDestinationIdentityRequired = "PUB_DESTINATION_IDENTITY_REQUIRED"
	ReasonDestinationUnconfirmed      = "PUB_DESTINATION_UNCONFIRMED"
	ReasonDestinationMismatch         = "PUB_DESTINATION_MISMATCH"
	ReasonCandidateRequired           = "PUB_CANDIDATE_REQUIRED"
	ReasonCandidateScanRequired       = "PUB_CANDIDATE_SCAN_REQUIRED"
	ReasonCandidateScanStale          = "PUB_CANDIDATE_SCAN_STALE"
	ReasonCandidateScanFailed         = "PUB_CANDIDATE_SCAN_FAILED"
	ReasonHistoryScanRequired         = "PUB_HISTORY_SCAN_REQUIRED"
	ReasonHistoryScanStale            = "PUB_HISTORY_SCAN_STALE"
	ReasonHistoryScanFailed           = "PUB_HISTORY_SCAN_FAILED"
	ReasonVerificationRequired        = "PUB_VERIFICATION_REQUIRED"
	ReasonVerificationStale           = "PUB_VERIFICATION_STALE"
	ReasonVerificationFailed          = "PUB_VERIFICATION_FAILED"
	ReasonVerificationIncomplete      = "PUB_VERIFICATION_INCOMPLETE"
	ReasonLicenseRequired             = "PUB_LICENSE_REQUIRED"
	ReasonREADMERequired              = "PUB_README_REQUIRED"
	ReasonREADMEPlaceholder           = "PUB_README_PLACEHOLDER"
	ReasonREADMEUsageMissing          = "PUB_README_USAGE_MISSING"
	ReasonFileSummaryRequired         = "PUB_FILE_SUMMARY_REQUIRED"
	ReasonFileSummaryLimit            = "PUB_FILE_SUMMARY_LIMIT"
	ReasonDigestInvalid               = "PUB_DIGEST_INVALID"
	ReasonFeatureBranchRequired       = "PUB_FEATURE_BRANCH_REQUIRED"
	ReasonStatusChecksRequired        = "PUB_STATUS_CHECKS_REQUIRED"
	ReasonDestinationVisibility       = "PUB_DESTINATION_VISIBILITY_REQUIRED"
	ReasonFileMetadataInvalid         = "PUB_FILE_METADATA_INVALID"
	ReasonTestsReadiness              = "PUB_TESTS_READINESS_GAP"
	ReasonCIReadiness                 = "PUB_CI_READINESS_GAP"
)

var sha256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)
var identityRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
var refRE = regexp.MustCompile(`^refs/heads/[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// Destination is the complete destination identity shown in a preflight.
// Every field is required for first public publication.
type Destination struct {
	Provider   string `json:"provider"`
	Host       string `json:"host"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Visibility string `json:"visibility"`
	Ref        string `json:"ref"`
}

func (d Destination) valid() bool {
	if d.Provider == "" || d.Host == "" || d.Owner == "" || d.Repository == "" || d.Ref == "" {
		return false
	}
	if d.Provider != "github" || d.Host != "github.com" || !identityRE.MatchString(d.Owner) || !identityRE.MatchString(d.Repository) {
		return false
	}
	if d.Visibility != VisibilityPublic && d.Visibility != VisibilityPrivate {
		return false
	}
	if strings.Contains(d.Owner, "..") || strings.Contains(d.Repository, "..") || strings.HasSuffix(strings.ToLower(d.Repository), ".lock") {
		return false
	}
	if !refRE.MatchString(d.Ref) || len(d.Ref) <= len("refs/heads/") || strings.Contains(d.Ref, "..") || strings.Contains(d.Ref, "//") || strings.Contains(d.Ref, "@{") || strings.HasSuffix(strings.ToLower(d.Ref), ".lock") || strings.HasPrefix(strings.TrimPrefix(d.Ref, "refs/heads/"), "-") {
		return false
	}
	for _, value := range []string{d.Provider, d.Host, d.Owner, d.Repository, d.Visibility, d.Ref} {
		for _, r := range value {
			if unicode.IsControl(r) || r == '\x00' {
				return false
			}
		}
	}
	return true
}

func (d Destination) equal(other Destination) bool {
	return d.Provider == other.Provider && d.Host == other.Host && d.Owner == other.Owner &&
		d.Repository == other.Repository && d.Visibility == other.Visibility && d.Ref == other.Ref
}

// FileMetadata is a bounded, content-free file summary entry.
type FileMetadata struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Mode  uint32 `json:"mode,omitempty"`
}

// ScanEvidence is bound to exactly one candidate and policy revision. Scope
// must be ScanCandidate or ScanHistory, and Digest is the evidence digest.
type ScanEvidence struct {
	Scope           string
	CandidateDigest string
	PolicyDigest    string
	Passed          bool
	Digest          string
	Findings        int
	ReasonCodes     []string
}

// VerificationEvidence binds all required verifier inputs, including guards
// and the complete verifier set.
type VerificationEvidence struct {
	CandidateDigest   string
	BaseDigest        string
	PolicyDigest      string
	GuardDigest       string
	VerifierSetDigest string
	Passed            bool
	Required          int
	PassedCount       int
	Digest            string
	ReasonCodes       []string
}

// LicenseEvidence records a deliberate license choice and the selected file.
// Selected="none" is explicit but is not sufficient for public publication.
type LicenseEvidence struct {
	Selected string
	FilePath string
	Present  bool
}

// READMEInput is transient input. Content is never copied to Report.
type READMEInput struct {
	Path    string
	Content []byte
}

// Readiness contains caller-reported portfolio signals. The evaluator does
// not infer passing tests, CI, descriptions, or topics from repository text.
type Readiness struct {
	Tests              string
	CI                 string
	DescriptionPresent bool
	TopicsPresent      bool
}

// Request is a value input to Evaluate. Evaluate snapshots slices and bytes
// before examining them, so later caller mutation cannot alter its report.
type Request struct {
	Mode                  string
	FirstPublication      bool
	PublicConsent         bool
	CandidateDigest       string
	CandidateSHA          string // compatibility alias for CandidateDigest
	BaseDigest            string
	PolicyDigest          string
	GuardDigest           string
	VerifierSetDigest     string
	Destination           Destination
	ObservedDestination   Destination
	DestinationConfirmed  bool
	Files                 []FileMetadata
	CandidateScan         ScanEvidence
	HistoryScan           ScanEvidence
	CurrentHistoryScan    ScanEvidence // compatibility alias for HistoryScan
	Verification          VerificationEvidence
	License               LicenseEvidence
	README                READMEInput
	Readiness             Readiness
	WorkflowSafe          bool
	WorkflowSolo          bool
	FeatureBranchApproved bool
	ProtectedBranch       bool
	StatusChecksRequired  bool
	StatusChecksPassed    bool
	inputFilesTruncated   bool
}

// Descriptive aliases keep the publication boundary easy to discover without
// introducing duplicate mutable records.
type PreflightRequest = Request
type PreflightReport = Report
type CandidateScanEvidence = ScanEvidence
type HistoryScanEvidence = ScanEvidence
type VerifierEvidence = VerificationEvidence

// READMEStatus is metadata-only and contains no README text.
type READMEStatus struct {
	Path           string `json:"path,omitempty"`
	Present        bool   `json:"present"`
	Placeholder    bool   `json:"placeholder"`
	NonPlaceholder bool   `json:"non_placeholder"`
	UsageGuidance  bool   `json:"usage_guidance"`
	ContentBytes   int    `json:"content_bytes"`
	ContentDigest  string `json:"content_digest,omitempty"`
}

// Report is deterministic, bounded, and safe to serialize as a user-facing
// preflight. PublicAuthorized is intentionally false for private/local modes.
type Report struct {
	Mode                  string
	FirstPublication      bool
	Ready                 bool
	PublicAuthorized      bool
	Destination           Destination
	Files                 []FileMetadata
	FileCount             int
	TotalBytes            int64
	CandidateScan         ScanSummary
	HistoryScan           ScanSummary
	Verification          VerificationSummary
	README                READMEStatus
	License               LicenseSummary
	Readiness             Readiness
	WorkflowSafe          bool
	WorkflowSolo          bool
	FeatureBranchApproved bool
	ProtectedBranch       bool
	StatusChecksRequired  bool
	StatusChecksPassed    bool
	ReasonCodes           []string
	Digest                string
}

type ScanSummary struct {
	Scope           string `json:"scope"`
	Passed          bool   `json:"passed"`
	Findings        int    `json:"findings"`
	CandidateDigest string `json:"candidate_digest,omitempty"`
	PolicyDigest    string `json:"policy_digest,omitempty"`
	EvidenceDigest  string `json:"evidence_digest,omitempty"`
}
type VerificationSummary struct {
	Passed            bool   `json:"passed"`
	Required          int    `json:"required"`
	PassedCount       int    `json:"passed_count"`
	CandidateDigest   string `json:"candidate_digest,omitempty"`
	BaseDigest        string `json:"base_digest,omitempty"`
	PolicyDigest      string `json:"policy_digest,omitempty"`
	GuardDigest       string `json:"guard_digest,omitempty"`
	VerifierSetDigest string `json:"verifier_set_digest,omitempty"`
	EvidenceDigest    string `json:"evidence_digest,omitempty"`
}
type LicenseSummary struct {
	Selected string `json:"selected,omitempty"`
	FilePath string `json:"file_path,omitempty"`
	Present  bool   `json:"present"`
}

// Evaluate performs a pure local preflight. No provider or repository action
// is reachable from this function.
func Evaluate(input Request) Report {
	req := snapshot(input)
	if req.Mode == "" {
		req.Mode = ModePrivate
	}
	if req.CandidateDigest == "" {
		req.CandidateDigest = req.CandidateSHA
	}
	if req.HistoryScan.Digest == "" {
		req.HistoryScan = req.CurrentHistoryScan
	}
	r := Report{
		Mode: req.Mode, FirstPublication: req.FirstPublication,
		Destination: safeDestination(req.Destination), Readiness: safeReadiness(req.Readiness),
		WorkflowSafe: req.WorkflowSafe, WorkflowSolo: req.WorkflowSolo,
		FeatureBranchApproved: req.FeatureBranchApproved, ProtectedBranch: req.ProtectedBranch,
		StatusChecksRequired: req.StatusChecksRequired, StatusChecksPassed: req.StatusChecksPassed,
		License: LicenseSummary{Selected: boundedMeta(req.License.Selected), FilePath: boundedPath(req.License.FilePath), Present: req.License.Present},
	}
	r.Files, r.FileCount, r.TotalBytes = summarizeFiles(req.Files, &r)
	r.README = inspectREADME(req.README, &r)
	r.CandidateScan = scanSummary(req.CandidateScan, ScanCandidate)
	r.HistoryScan = scanSummary(req.HistoryScan, ScanHistory)
	r.Verification = VerificationSummary{Passed: req.Verification.Passed, Required: nonnegative(req.Verification.Required), PassedCount: nonnegative(req.Verification.PassedCount), CandidateDigest: boundedMeta(req.Verification.CandidateDigest), BaseDigest: boundedMeta(req.Verification.BaseDigest), PolicyDigest: boundedMeta(req.Verification.PolicyDigest), GuardDigest: boundedMeta(req.Verification.GuardDigest), VerifierSetDigest: boundedMeta(req.Verification.VerifierSetDigest), EvidenceDigest: boundedMeta(req.Verification.Digest)}

	if req.Mode != ModePublic && req.Mode != ModePrivate && req.Mode != ModeLocal {
		r.add(ReasonInvalidMode)
	}
	public := req.Mode == ModePublic
	gate := public // all public requests use the same fail-closed preflight
	if gate {
		if !req.PublicConsent {
			r.add(ReasonPublicConsentRequired)
		}
		if !req.Destination.valid() {
			r.add(ReasonDestinationIdentityRequired)
		}
		if req.Destination.Visibility != VisibilityPublic {
			r.add(ReasonDestinationVisibility)
		}
		if !req.DestinationConfirmed {
			r.add(ReasonDestinationUnconfirmed)
		}
		if !req.ObservedDestination.valid() {
			r.add(ReasonDestinationIdentityRequired)
		} else if !req.Destination.equal(req.ObservedDestination) {
			r.add(ReasonDestinationMismatch)
		}
		if !req.WorkflowSafe && !req.WorkflowSolo {
			r.add(ReasonFeatureBranchRequired)
		}
		if req.WorkflowSafe && (!req.FeatureBranchApproved || strings.HasSuffix(req.Destination.Ref, "/main") || strings.HasSuffix(req.Destination.Ref, "/master")) {
			r.add(ReasonFeatureBranchRequired)
		}
		if req.ProtectedBranch && req.StatusChecksRequired && !req.StatusChecksPassed {
			r.add(ReasonStatusChecksRequired)
		}
		if !validDigest(req.CandidateDigest) {
			r.add(ReasonCandidateRequired)
		}
		if !validDigest(req.PolicyDigest) || !validDigest(req.BaseDigest) {
			r.add(ReasonDigestInvalid)
		}
		checkScan(req.CandidateScan, ScanCandidate, req.CandidateDigest, req.PolicyDigest, &r, false)
		checkScan(req.HistoryScan, ScanHistory, req.CandidateDigest, req.PolicyDigest, &r, true)
		checkVerification(req.Verification, req.CandidateDigest, req.BaseDigest, req.PolicyDigest, req.GuardDigest, req.VerifierSetDigest, &r)
		if req.License.Selected == "" || strings.EqualFold(req.License.Selected, "none") || req.License.FilePath == "" || !req.License.Present {
			r.add(ReasonLicenseRequired)
		}
		if !hasFile(req.Files, req.README.Path) || !hasFile(req.Files, req.License.FilePath) {
			r.add(ReasonFileSummaryRequired)
		}
		if req.Readiness.Tests != StatusPassed {
			r.add(ReasonTestsReadiness)
		}
		if req.Readiness.CI != StatusPresent && req.Readiness.CI != StatusPassed {
			r.add(ReasonCIReadiness)
		}
		if !r.README.Present {
			r.add(ReasonREADMERequired)
		}
		if r.README.Placeholder {
			r.add(ReasonREADMEPlaceholder)
		}
		if !r.README.UsageGuidance {
			r.add(ReasonREADMEUsageMissing)
		}
		if r.FileCount == 0 {
			r.add(ReasonFileSummaryRequired)
		}
	}
	r.ReasonCodes = uniqueReasons(r.ReasonCodes)
	// Scope markers describe reports but never become a public grant.
	r.Ready = len(r.ReasonCodes) == 0
	r.PublicAuthorized = public && r.Ready
	if !public {
		r.PublicAuthorized = false
	}
	r.Digest = reportDigest(r)
	return r
}

// Preflight and BuildReport are descriptive aliases for callers.
func Preflight(req Request) Report   { return Evaluate(req) }
func BuildReport(req Request) Report { return Evaluate(req) }

// CanPublishPublic is deliberately narrower than Ready: a private or local
// report can be ready for its own scope but can never authorize public work.
func (r Report) CanPublishPublic() bool { return r.PublicAuthorized }
func (r Report) Safe() bool             { return r.Ready }

func (r *Report) add(code string) { r.ReasonCodes = append(r.ReasonCodes, code) }

func checkScan(e ScanEvidence, scope, candidate, policy string, r *Report, history bool) {
	required, stale, failed := ReasonCandidateScanRequired, ReasonCandidateScanStale, ReasonCandidateScanFailed
	if history {
		required, stale, failed = ReasonHistoryScanRequired, ReasonHistoryScanStale, ReasonHistoryScanFailed
	}
	if e.Digest == "" {
		r.add(required)
		return
	}
	if !validDigest(e.Digest) || !validDigest(e.CandidateDigest) || !validDigest(e.PolicyDigest) || e.Scope != scope || e.CandidateDigest != candidate || e.PolicyDigest != policy {
		r.add(stale)
		return
	}
	if !e.Passed || e.Findings > 0 || len(e.ReasonCodes) > 0 {
		r.add(failed)
	}
}

func checkVerification(e VerificationEvidence, candidate, base, policy, guard, verifierSet string, r *Report) {
	if e.Digest == "" {
		r.add(ReasonVerificationRequired)
		return
	}
	if !validDigest(e.Digest) || !validDigest(e.CandidateDigest) || !validDigest(e.BaseDigest) || !validDigest(e.PolicyDigest) || !validDigest(e.GuardDigest) || !validDigest(e.VerifierSetDigest) || e.CandidateDigest != candidate || e.BaseDigest != base || e.PolicyDigest != policy || e.GuardDigest != guard || e.VerifierSetDigest != verifierSet {
		r.add(ReasonVerificationStale)
		return
	}
	if e.Required <= 0 || e.PassedCount < e.Required {
		r.add(ReasonVerificationIncomplete)
	}
	if !e.Passed || len(e.ReasonCodes) > 0 {
		r.add(ReasonVerificationFailed)
	}
}

func scanSummary(e ScanEvidence, expected string) ScanSummary {
	return ScanSummary{Scope: expected, Passed: e.Passed, Findings: nonnegative(e.Findings), CandidateDigest: boundedMeta(e.CandidateDigest), PolicyDigest: boundedMeta(e.PolicyDigest), EvidenceDigest: boundedMeta(e.Digest)}
}

func summarizeFiles(files []FileMetadata, r *Report) ([]FileMetadata, int, int64) {
	copyFiles := append([]FileMetadata(nil), files...)
	sort.Slice(copyFiles, func(i, j int) bool {
		if copyFiles[i].Path != copyFiles[j].Path {
			return copyFiles[i].Path < copyFiles[j].Path
		}
		if copyFiles[i].Bytes != copyFiles[j].Bytes {
			return copyFiles[i].Bytes < copyFiles[j].Bytes
		}
		return copyFiles[i].Mode < copyFiles[j].Mode
	})
	if len(copyFiles) > MaxReportedFiles || len(copyFiles) >= maxInputFiles {
		r.add(ReasonFileSummaryLimit)
	}
	out := make([]FileMetadata, 0, min(len(copyFiles), MaxReportedFiles))
	var total int64
	for _, f := range copyFiles {
		originalPath := f.Path
		f.Path = boundedPath(originalPath)
		if f.Path == "<invalid-path>" || len(originalPath) > maxReportedPath {
			r.add(ReasonFileMetadataInvalid)
		}
		if f.Bytes < 0 || f.Bytes > math.MaxInt64-total {
			r.add(ReasonFileMetadataInvalid)
			f.Bytes = 0
		}
		total += f.Bytes
		if len(out) < MaxReportedFiles {
			out = append(out, f)
		}
	}
	return out, len(copyFiles), total
}

func hasFile(files []FileMetadata, wanted string) bool {
	wanted = strings.ReplaceAll(wanted, "\\", "/")
	if wanted == "" || boundedPath(wanted) == "<invalid-path>" {
		return false
	}
	for _, file := range files {
		if strings.ReplaceAll(file.Path, "\\", "/") == wanted {
			return true
		}
	}
	return false
}

func inspectREADME(in READMEInput, r *Report) READMEStatus {
	s := READMEStatus{Path: boundedPath(in.Path), ContentBytes: len(in.Content)}
	if in.Path == "" || len(in.Path) > maxReportedPath || s.Path == "<invalid-path>" || len(in.Content) == 0 || len(in.Content) > maxREADMEBytes {
		return s
	}
	s.Present = true
	h := sha256.Sum256(in.Content)
	s.ContentDigest = "sha256:" + hex.EncodeToString(h[:])
	text := strings.ToLower(strings.TrimSpace(string(in.Content)))
	s.Placeholder = obviousPlaceholder(text)
	s.NonPlaceholder = !s.Placeholder
	s.UsageGuidance = usageGuidance(text)
	return s
}

func obviousPlaceholder(text string) bool {
	if text == "" {
		return true
	}
	markers := []string{"describe your project", "your project description", "project title", "replace this", "replace with", "write your project", "coming soon"}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	lines := strings.Fields(text)
	return len(lines) <= 3 && (strings.Contains(text, "todo") || strings.TrimSpace(text) == "readme")
}

func usageGuidance(text string) bool {
	for _, marker := range []string{"usage", "how to use", "getting started", "quick start", "installation", "install", "example", "run"} {
		if containsWord(text, marker) {
			return true
		}
	}
	return false
}

func containsWord(text, marker string) bool {
	for _, index := range allIndexes(text, marker) {
		beforeOK := index == 0 || !isWord(text[index-1])
		end := index + len(marker)
		afterOK := end == len(text) || !isWord(text[end])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}
func allIndexes(text, marker string) []int {
	var indexes []int
	for offset := 0; ; {
		i := strings.Index(text[offset:], marker)
		if i < 0 {
			return indexes
		}
		i += offset
		indexes = append(indexes, i)
		offset = i + len(marker)
	}
}
func isWord(r byte) bool { return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' }

func snapshot(in Request) Request {
	if len(in.Files) > maxInputFiles {
		in.inputFilesTruncated = true
		in.Files = in.Files[:maxInputFiles]
	}
	in.Files = append([]FileMetadata(nil), in.Files...)
	in.CandidateScan.ReasonCodes = append([]string(nil), in.CandidateScan.ReasonCodes...)
	in.HistoryScan.ReasonCodes = append([]string(nil), in.HistoryScan.ReasonCodes...)
	in.Verification.ReasonCodes = append([]string(nil), in.Verification.ReasonCodes...)
	if len(in.README.Content) > maxREADMEBytes+1 {
		in.README.Content = in.README.Content[:maxREADMEBytes+1]
	}
	return in
}

func reportDigest(r Report) string {
	// A fixed struct with sorted arrays provides canonical JSON without ever
	// including request content or unbounded diagnostic text.
	view := struct {
		Mode                  string
		First                 bool
		Ready                 bool
		Public                bool
		Destination           Destination
		Files                 []FileMetadata
		FileCount             int
		Total                 int64
		Candidate             ScanSummary
		History               ScanSummary
		Verification          VerificationSummary
		README                READMEStatus
		License               LicenseSummary
		Readiness             Readiness
		WorkflowSafe          bool
		WorkflowSolo          bool
		FeatureBranchApproved bool
		ProtectedBranch       bool
		StatusChecksRequired  bool
		StatusChecksPassed    bool
		Reasons               []string
	}{r.Mode, r.FirstPublication, r.Ready, r.PublicAuthorized, r.Destination, r.Files, r.FileCount, r.TotalBytes, r.CandidateScan, r.HistoryScan, r.Verification, r.README, r.License, r.Readiness, r.WorkflowSafe, r.WorkflowSolo, r.FeatureBranchApproved, r.ProtectedBranch, r.StatusChecksRequired, r.StatusChecksPassed, r.ReasonCodes}
	b, _ := json.Marshal(view)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func validDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && sha256RE.MatchString(strings.TrimPrefix(value, "sha256:"))
}
func boundedPath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	for _, r := range value {
		if unicode.IsControl(r) || r == '\x00' {
			return "<invalid-path>"
		}
	}
	if value == "" || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") {
		return "<invalid-path>"
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." || part == "" {
			return "<invalid-path>"
		}
	}
	if len(value) > maxReportedPath {
		return value[:maxReportedPath]
	}
	return value
}
func boundedMeta(value string) string {
	for _, r := range value {
		if unicode.IsControl(r) || r == '\x00' {
			return "<invalid>"
		}
	}
	if len(value) > maxReportedPath {
		return value[:maxReportedPath]
	}
	return value
}
func safeDestination(d Destination) Destination {
	d.Provider, d.Host, d.Owner, d.Repository, d.Visibility, d.Ref = boundedMeta(d.Provider), boundedMeta(d.Host), boundedMeta(d.Owner), boundedMeta(d.Repository), boundedMeta(d.Visibility), boundedPath(d.Ref)
	return d
}
func safeReadiness(in Readiness) Readiness {
	in.Tests, in.CI = normalizeStatus(in.Tests), normalizeStatus(in.CI)
	return in
}
func normalizeStatus(value string) string {
	switch value {
	case StatusUnknown, StatusAbsent, StatusPresent, StatusPassed, StatusFailed:
		return value
	default:
		return StatusUnknown
	}
}
func uniqueReasons(in []string) []string {
	seen := map[string]bool{}
	for _, code := range in {
		seen[code] = true
	}
	out := make([]string, 0, min(len(seen), MaxReasonCodes))
	for code := range seen {
		out = append(out, code)
	}
	sort.Strings(out)
	if len(out) > MaxReasonCodes {
		out = out[:MaxReasonCodes]
	}
	return out
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func nonnegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
