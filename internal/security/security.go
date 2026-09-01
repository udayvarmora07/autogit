package security

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Finding deliberately contains only metadata. In particular, a finding must
// never include a matching value or a source excerpt.
type Finding struct {
	Path, Category, Detail string
	Code                   string
}

const (
	ReasonSecretFilename   = "SEC_SECRET_FILENAME"
	ReasonSecretPattern    = "SEC_SECRET_PATTERN"
	ReasonSecretEntropy    = "SEC_SECRET_ENTROPY"
	ReasonConflictMarker   = "SEC_CONFLICT_MARKER"
	ReasonBinaryContent    = "SEC_BINARY_CONTENT"
	ReasonDuplicatePath    = "SEC_DUPLICATE_PATH"
	ReasonUnsafePath       = "SEC_UNSAFE_PATH"
	ReasonSymlink          = "SEC_SYMLINK"
	ReasonFileCount        = "SEC_FILE_COUNT_LIMIT"
	ReasonFileBytes        = "SEC_FILE_BYTES_LIMIT"
	ReasonTotalBytes       = "SEC_TOTAL_BYTES_LIMIT"
	ReasonFindingCount     = "SEC_FINDING_LIMIT"
	ReasonCancelled        = "SEC_CANCELLED"
	ReasonTimeBudget       = "SEC_TIME_BUDGET"
	ReasonMode             = "SEC_UNSAFE_MODE"
	ReasonBinarySkipped    = "SEC_BINARY_UNSCANNED"
	ReasonBinaryUnverified = "SEC_BINARY_UNVERIFIED"
)

// CandidateFile is an already materialized candidate entry. Scanner never
// opens paths and never walks a directory; callers must provide the complete,
// immutable snapshot they want checked.
type CandidateFile struct {
	Path    string
	Content []byte
	Mode    uint32
	Symlink bool
}

type CandidateSnapshot struct{ Files []CandidateFile }

// Snapshot and File are concise aliases for coordinator and adapter code.
// CandidateSnapshot/CandidateFile remain the canonical names in documentation.
type Snapshot = CandidateSnapshot
type File = CandidateFile

type BinaryPolicy uint8

const (
	// BinaryReject is the fail-closed default when a binary blob cannot be
	// reliably interpreted as source text.
	BinaryReject BinaryPolicy = iota
	BinaryScan
	BinarySkip
)

type Limits struct {
	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxFindings   int
	TimeBudget    time.Duration
}

type ScanResult struct {
	Findings     []Finding
	ReasonCodes  []string
	FilesScanned int
	TotalBytes   int64
	Blocked      bool
}

// Safe is convenient for coordinator callers and intentionally requires that
// no policy/limit/cancellation reason was produced.
func (r ScanResult) Safe() bool { return !r.Blocked }

type Scanner struct {
	Limits       Limits
	BinaryPolicy BinaryPolicy
	// Clock is injectable for deterministic budget tests. A nil clock uses
	// time.Now.
	Clock func() time.Time
}

// CandidateScanner is the narrow dependency boundary consumed by workflow
// code; implementations receive immutable values rather than repository paths.
type CandidateScanner interface {
	Scan(context.Context, CandidateSnapshot) ScanResult
}

var (
	secretName       = regexp.MustCompile(`(?i)(^|[._-])(credentials?|secrets?|id_rsa|id_dsa|private[_-]?key)([._-]|$)|\.env($|\.)|\.(pem|p12|pfx|key)$`)
	secretToken      = regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9_]{16,}|github_pat_[A-Za-z0-9_]{16,}|glpat-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{16}|(?:sk|rk)-[A-Za-z0-9]{16,}|-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----)`)
	secretAssignment = regexp.MustCompile(`(?im)^\s*(GH_TOKEN|GITHUB_TOKEN|AWS_SECRET_ACCESS_KEY|AWS_SESSION_TOKEN|API[_-]?KEY|ACCESS[_-]?TOKEN|AUTH[_-]?TOKEN|PASSWORD|PASSWD|SECRET|CREDENTIALS?)\s*[:=]\s*([^\s]+)`)
	secretContext    = regexp.MustCompile(`(?im)^\s*([A-Za-z0-9_.-]*(?:token|password|secret|api[_-]?key|credential)[A-Za-z0-9_.-]*)\s*[:=]\s*([^\s]+)`)
	conflictMarker   = regexp.MustCompile(`(?m)^[ \t]*(?:<<<<<<<|=======|>>>>>>>)`)
	base64Candidate  = regexp.MustCompile(`^[A-Za-z0-9+/]{24,}={0,2}$`)
)

const (
	defaultMaxFiles      = 10000
	defaultMaxFileBytes  = 4 << 20
	defaultMaxTotalBytes = 64 << 20
	defaultMaxFindings   = 256
	sha256HexLength      = 64
)

func (s Scanner) limits() Limits {
	l := s.Limits
	if l.MaxFiles <= 0 {
		l.MaxFiles = defaultMaxFiles
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = defaultMaxFileBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = defaultMaxTotalBytes
	}
	if l.MaxFindings <= 0 {
		l.MaxFindings = defaultMaxFindings
	}
	return l
}

// ScanOptions is separate so policy can be passed through a coordinator
// without exposing filesystem access to the scanner.
type ScanOptions struct {
	Limits       Limits
	BinaryPolicy BinaryPolicy
}

// ScanCandidate scans only snapshot.Files and is deterministic for a fixed
// snapshot and policy. All failures are blocking reason codes; this prevents a
// caller from accidentally treating a scanner error as safe.
func ScanCandidate(ctx context.Context, snapshot CandidateSnapshot, options ScanOptions) ScanResult {
	return Scanner{Limits: options.Limits, BinaryPolicy: options.BinaryPolicy}.Scan(ctx, snapshot)
}

func (s Scanner) Scan(parent context.Context, snapshot CandidateSnapshot) ScanResult {
	result := ScanResult{}
	limits := s.limits()
	if parent == nil {
		parent = context.Background()
	}
	clock := s.Clock
	if clock == nil {
		clock = time.Now
	}
	started := clock()
	budgetReason := func() string {
		if err := parent.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return ReasonCancelled
			}
			return ReasonTimeBudget
		}
		if limits.TimeBudget > 0 && clock().Sub(started) >= limits.TimeBudget {
			return ReasonTimeBudget
		}
		return ""
	}
	if reason := budgetReason(); reason != "" {
		if reason == ReasonCancelled {
			result.addReason(ReasonCancelled)
		} else {
			result.addReason(ReasonTimeBudget)
		}
		result.Blocked = true
		return result
	}

	files := append([]CandidateFile(nil), snapshot.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	seen := make(map[string]struct{}, len(files))
	for i, file := range files {
		if reason := budgetReason(); reason != "" {
			result.addReason(reason)
			break
		}
		if err := validCandidatePath(file.Path); err != nil {
			result.addReason(ReasonUnsafePath)
			continue
		}
		if _, ok := seen[file.Path]; ok {
			result.addReason(ReasonDuplicatePath)
			continue
		}
		seen[file.Path] = struct{}{}
		if file.Symlink || isSymlinkMode(file.Mode) {
			result.addReason(ReasonSymlink)
			continue
		}
		if unsafeMode(file.Mode) {
			result.addReason(ReasonMode)
			continue
		}
		if i >= limits.MaxFiles {
			result.addReason(ReasonFileCount)
			break
		}
		if int64(len(file.Content)) > limits.MaxFileBytes {
			result.addReason(ReasonFileBytes)
			continue
		}
		if result.TotalBytes+int64(len(file.Content)) > limits.MaxTotalBytes {
			result.addReason(ReasonTotalBytes)
			break
		}
		result.TotalBytes += int64(len(file.Content))
		result.FilesScanned++
		if binaryContent(file.Content) {
			switch s.BinaryPolicy {
			case BinaryReject:
				result.addFinding(limits, Finding{Path: file.Path, Category: "binary", Code: ReasonBinaryContent, Detail: "binary content requires an explicit scan policy"})
				continue
			case BinarySkip:
				result.addReason(ReasonBinarySkipped)
				continue
			case BinaryScan:
				s.scanFile(&result, limits, file)
				result.addReason(ReasonBinaryUnverified)
				continue
			default:
				result.addReason(ReasonBinaryUnverified)
				continue
			}
		}
		s.scanFile(&result, limits, file)
		if len(result.Findings) >= limits.MaxFindings {
			result.addReason(ReasonFindingCount)
			break
		}
	}
	sort.Slice(result.Findings, func(i, j int) bool {
		if result.Findings[i].Path != result.Findings[j].Path {
			return result.Findings[i].Path < result.Findings[j].Path
		}
		return result.Findings[i].Code < result.Findings[j].Code
	})
	for _, finding := range result.Findings {
		result.addReason(finding.Code)
	}
	sort.Strings(result.ReasonCodes)
	result.Blocked = len(result.Findings) > 0 || len(result.ReasonCodes) > 0
	return result
}

func (r *ScanResult) addReason(code string) {
	for _, old := range r.ReasonCodes {
		if old == code {
			return
		}
	}
	r.ReasonCodes = append(r.ReasonCodes, code)
}

func (r *ScanResult) addFinding(l Limits, finding Finding) {
	if len(r.Findings) < l.MaxFindings {
		r.Findings = append(r.Findings, finding)
	}
}

func (s Scanner) scanFile(result *ScanResult, limits Limits, file CandidateFile) {
	if secretName.MatchString(file.Path) {
		result.addFinding(limits, Finding{Path: file.Path, Category: "secret", Code: ReasonSecretFilename, Detail: "secret-bearing filename detected"})
		// A filename finding is sufficient and avoids duplicate content findings.
		return
	}
	text := string(file.Content)
	if conflictMarker.MatchString(text) {
		result.addFinding(limits, Finding{Path: file.Path, Category: "conflict", Code: ReasonConflictMarker, Detail: "merge conflict marker detected"})
	}
	if secretToken.Match(file.Content) || secretAssignment.MatchString(text) {
		result.addFinding(limits, Finding{Path: file.Path, Category: "secret", Code: ReasonSecretPattern, Detail: "credential pattern detected"})
	} else if hasHighEntropySecret(text) {
		result.addFinding(limits, Finding{Path: file.Path, Category: "secret", Code: ReasonSecretEntropy, Detail: "high-entropy credential candidate detected"})
	}
	// Encoded values are checked without ever returning the decoded value.
	for _, line := range strings.Fields(text) {
		if !base64Candidate.MatchString(line) {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(line)
		if err == nil && (secretToken.Match(decoded) || secretAssignment.Match(decoded)) {
			result.addFinding(limits, Finding{Path: file.Path, Category: "secret", Code: ReasonSecretPattern, Detail: "encoded credential pattern detected"})
			break
		}
	}
}

func hasHighEntropySecret(text string) bool {
	for _, match := range secretContext.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 {
			continue
		}
		value := strings.Trim(match[2], "\"'`")
		if isPlaceholder(value) || len(value) < 20 || hexLike(value) || entropy(value) < 3.5 || characterClasses(value) < 3 {
			continue
		}
		return true
	}
	// Keep the context check intentionally line-oriented as well. This covers
	// common `token = value` forms without treating arbitrary high-entropy
	// hashes, IDs, or prose as credentials.
	for _, line := range strings.Split(text, "\n") {
		sep := strings.IndexAny(line, "=:")
		if sep < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:sep]))
		if !strings.Contains(key, "token") && !strings.Contains(key, "password") && !strings.Contains(key, "secret") && !strings.Contains(key, "credential") && !strings.Contains(key, "api_key") && !strings.Contains(key, "api-key") && !strings.Contains(key, "apikey") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(line[sep+1:]), "\"'`")
		if !isPlaceholder(value) && len(value) >= 20 && !hexLike(value) && entropy(value) >= 3.5 && characterClasses(value) >= 3 {
			return true
		}
	}
	return false
}

func isPlaceholder(value string) bool {
	v := strings.ToLower(value)
	for _, word := range []string{"changeme", "change-me", "example", "sample", "dummy", "replace_me", "your_", "redacted", "not-a-real", "<secret>", "xxxx"} {
		if strings.Contains(v, word) {
			return true
		}
	}
	return false
}

func hexLike(s string) bool {
	if len(s) != 32 && len(s) != 40 && len(s) != sha256HexLength {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func entropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	h := 0.0
	for _, count := range counts {
		if count == 0 {
			continue
		}
		p := float64(count) / float64(len(s))
		h -= p * math.Log2(p)
	}
	return h
}

func characterClasses(s string) int {
	classes := 0
	for _, test := range []func(rune) bool{
		func(r rune) bool { return r >= 'a' && r <= 'z' },
		func(r rune) bool { return r >= 'A' && r <= 'Z' },
		func(r rune) bool { return r >= '0' && r <= '9' },
		func(r rune) bool { return strings.ContainsRune("!@#$%^&*()-_=+[]{}:;,.?/\\", r) },
	} {
		for _, r := range s {
			if test(r) {
				classes++
				break
			}
		}
	}
	return classes
}

func binaryContent(b []byte) bool { return bytesContainNUL(b) || !utf8.Valid(b) }

func bytesContainNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

func validCandidatePath(name string) error {
	if name == "" || strings.ContainsRune(name, 0) || strings.ContainsRune(name, '\\') || strings.HasPrefix(name, "/") || path.IsAbs(name) {
		return errors.New("unsafe candidate path")
	}
	clean := path.Clean(name)
	if clean != name || clean == "." {
		return errors.New("unsafe candidate path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("unsafe candidate path")
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f {
				return errors.New("unsafe candidate path")
			}
		}
	}
	return nil
}

func isSymlinkMode(mode uint32) bool {
	return mode&0120000 == 0120000 || os.FileMode(mode)&os.ModeSymlink != 0
}

func unsafeMode(mode uint32) bool {
	if mode == 0 {
		return false
	}
	fileMode := os.FileMode(mode)
	if fileMode&(os.ModeDir|os.ModeNamedPipe|os.ModeSocket|os.ModeDevice|os.ModeCharDevice|os.ModeIrregular|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return true
	}
	// Git's regular modes are 0100644 and 0100755. Reject special files and
	// set-id/sticky bits in either Git or os.FileMode representations.
	return mode&07000 != 0 || mode&0170000 != 0100000 && mode&0170000 != 0
}

// Scan is the compatibility API. It retains the original metadata-only shape
// while making output ordering and content handling deterministic.
func Scan(files map[string][]byte) []Finding {
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	snapshot := CandidateSnapshot{Files: make([]CandidateFile, 0, len(paths))}
	for _, name := range paths {
		snapshot.Files = append(snapshot.Files, CandidateFile{Path: name, Content: files[name]})
	}
	result := (Scanner{BinaryPolicy: BinaryScan, Limits: Limits{MaxFiles: len(paths) + 1, MaxFileBytes: int64(defaultMaxFileBytes), MaxTotalBytes: defaultMaxTotalBytes}}).Scan(context.Background(), snapshot)
	out := make([]Finding, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if finding.Category == "binary" {
			continue
		}
		out = append(out, Finding{Path: finding.Path, Category: finding.Category, Detail: finding.Category + " detected", Code: finding.Code})
	}
	return out
}

var url = regexp.MustCompile(`https?://[^\s]+`)
var absolutePath = regexp.MustCompile(`(?:^|[\s=:])/(?:[^\s]+)`)
var token = regexp.MustCompile(`(?i)(token|password|secret|api[_-]?key)=([^\s&]+)`)

func Redact(s string) string {
	s = url.ReplaceAllString(s, "[remote redacted]")
	s = absolutePath.ReplaceAllString(s, " [path redacted]")
	s = token.ReplaceAllString(s, "$1=[REDACTED]")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || r >= ' ' && r != '\x7f' {
			b.WriteRune(r)
		} else {
			b.WriteRune('�')
		}
	}
	return b.String()
}
