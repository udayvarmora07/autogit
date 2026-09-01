// Package historyscan performs a read-only, exact-SHA scan of the history that
// would be published. It intentionally does not inspect refs, the index, or
// the worktree: the supplied commit is the complete scope of the operation.
package historyscan

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"autogit/internal/gittransaction"
	"autogit/internal/security"
)

// CommandResult is the bounded result returned by a Git Runner. It is an
// alias so existing argument-safe system runners can be injected directly.
type CommandResult = gittransaction.Result

// Runner is the only process boundary used by this package. Implementations
// must execute argv directly and bound combined stdout/stderr. The env map is
// controlled by historyscan and must not be augmented with ambient variables.
type Runner = gittransaction.Runner

type boundedRunner interface {
	RunBounded(context.Context, string, map[string]string, int, ...string) (CommandResult, error)
}

// Limits bounds every untrusted Git result and the amount of history read.
// Zero values select conservative defaults.
type Limits struct {
	MaxObjects    int
	MaxCommits    int
	MaxEntries    int
	MaxBlobBytes  int64
	MaxTotalBytes int64
	MaxFindings   int
	MaxOutput     int
	Timeout       time.Duration
}

// Request identifies exactly one immutable candidate. CandidateSHA is
// required. CandidateRef, when supplied, is checked against that SHA; it is
// never used as the history traversal scope.
type Request struct {
	RepoRoot string
	Root     string // Root is accepted as a descriptive alias for RepoRoot.

	CandidateSHA    string
	CandidateCommit string // CandidateCommit is accepted as an alias for SHA.
	CandidateRef    string
	Ref             string // Ref is accepted as an alias for CandidateRef.

	PolicyDigest string
	Limits       Limits
}

// Finding is deliberately metadata-only. Digest is the verified Git object
// ID, never the object contents or a matching secret.
type Finding struct {
	Path     string
	Category string
	Reason   string
	Digest   string
}

// Evidence is bound to the exact candidate, policy, and scanner version. A
// blocked result is still useful evidence, but can never authorize publish.
type Evidence struct {
	CandidateSHA string
	CandidateRef string
	PolicyDigest string
	Scanner      string
	Digest       string

	Findings       []Finding
	ReasonCodes    []string
	CommitsScanned int
	ObjectsScanned int
	TotalBytes     int64
	Blocked        bool
	findingLimit   int
}

// Result is retained as a readable name for callers that model scan output as
// a result. It is exactly the immutable Evidence record.
type Result = Evidence

// Safe reports whether the evidence can be used as a publication gate.
func (e Evidence) Safe() bool { return !e.Blocked && len(e.Findings) == 0 && len(e.ReasonCodes) == 0 }

// ValidFor prevents evidence reuse across candidate, policy, or scanner
// changes. The digest itself also commits to the complete metadata result.
func (e Evidence) ValidFor(candidateSHA, policyDigest, scannerVersion string) bool {
	return e.Safe() && e.CandidateSHA == candidateSHA && e.PolicyDigest == policyDigest && e.Scanner == scannerVersion && e.Digest != ""
}

const (
	ScannerVersion = "historyscan/v1"

	ReasonInvalidRequest    = "HIST_INVALID_REQUEST"
	ReasonRoot              = "HIST_ROOT_INVALID"
	ReasonCandidate         = "HIST_CANDIDATE_INVALID"
	ReasonRef               = "HIST_REF_MISMATCH"
	ReasonGitFailure        = "HIST_GIT_FAILURE"
	ReasonOutputTruncated   = "HIST_OUTPUT_TRUNCATED"
	ReasonObjectUnavailable = "HIST_OBJECT_UNAVAILABLE"
	ReasonObjectSubstitute  = "HIST_OBJECT_SUBSTITUTION"
	ReasonObjectType        = "HIST_OBJECT_TYPE"
	ReasonObjectSize        = "HIST_OBJECT_SIZE"
	ReasonObjectCount       = "HIST_OBJECT_COUNT_LIMIT"
	ReasonCommitCount       = "HIST_COMMIT_COUNT_LIMIT"
	ReasonEntryCount        = "HIST_ENTRY_COUNT_LIMIT"
	ReasonBlobSize          = "HIST_BLOB_SIZE_LIMIT"
	ReasonTotalSize         = "HIST_TOTAL_BYTES_LIMIT"
	ReasonFindingLimit      = "HIST_FINDING_LIMIT"
	ReasonMalformedPath     = "HIST_MALFORMED_PATH"
	ReasonAmbiguousPath     = "HIST_AMBIGUOUS_PATH"
	ReasonSubmodule         = "HIST_SUBMODULE"
	ReasonSymlink           = "HIST_SYMLINK"
	ReasonLFSPointer        = "HIST_LFS_POINTER"
	ReasonCancelled         = security.ReasonCancelled
	ReasonTimeout           = security.ReasonTimeBudget
)

const (
	defaultMaxObjects   = 100000
	defaultMaxCommits   = 10000
	defaultMaxEntries   = 1000000
	defaultMaxBlobBytes = 4 << 20
	defaultMaxTotal     = 64 << 20
	defaultMaxFindings  = 256
	defaultMaxOutput    = 8 << 20
)

var (
	shaRE     = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	refPartRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	modeRE    = regexp.MustCompile(`^[0-7]{6}$`)
	policyRE  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// HistoryScanner is safe to share between scans when its Runner is safe to
// share. Scanner is copied by value and receives one immutable snapshot at a
// time.
type HistoryScanner struct {
	Runner         Runner
	Scanner        security.Scanner
	ScannerVersion string
}

// Scanner is the concise name used by callers that construct the scanner as
// a value. It aliases HistoryScanner so there is one implementation.
type Scanner = HistoryScanner

// New returns a scanner using the direct, controlled Git runner. A custom
// Runner is preferable in tests and may be supplied to HistoryScanner.
func New(r Runner) HistoryScanner {
	if r == nil {
		executable, err := exec.LookPath("git")
		if err != nil {
			executable = "git"
		}
		r = gittransaction.SystemRunner{Executable: executable, MaxOutput: defaultMaxOutput}
	}
	return HistoryScanner{Runner: r, ScannerVersion: ScannerVersion}
}

// NewScanner is an explicit constructor alias for New.
func NewScanner(r Runner) HistoryScanner { return New(r) }

// Scan is the package-level convenience API.
func Scan(ctx context.Context, r Runner, req Request) (Evidence, error) {
	return ScanHistory(ctx, r, req)
}

// ScanHistory is the package-level convenience API.
func ScanHistory(ctx context.Context, r Runner, req Request) (Evidence, error) {
	return New(r).Scan(ctx, req)
}

// Scan enumerates only the exact candidate's commit snapshots and verifies
// each reachable blob before handing its bytes to security.Scanner.
func (s HistoryScanner) Scan(parent context.Context, req Request) (Evidence, error) {
	limits := req.Limits.withDefaults()
	sha := req.CandidateSHA
	if sha == "" {
		sha = req.CandidateCommit
	}
	ref := req.CandidateRef
	if ref == "" {
		ref = req.Ref
	}
	root := req.RepoRoot
	if root == "" {
		root = req.Root
	}
	e := Evidence{CandidateSHA: sha, CandidateRef: ref, PolicyDigest: req.PolicyDigest, Scanner: s.version()}
	e.findingLimit = limits.MaxFindings
	finish := func(err error) (Evidence, error) {
		if e.findingLimit > 0 && len(e.Findings) >= e.findingLimit {
			e.addReason(ReasonFindingLimit)
		}
		e.Findings = append([]Finding(nil), e.Findings...)
		e.ReasonCodes = uniqueSorted(e.ReasonCodes)
		e.Blocked = true
		e.Digest = evidenceDigest(e)
		return e, err
	}
	if parent == nil {
		parent = context.Background()
	}
	if s.Runner == nil {
		return finish(errors.New("historyscan runner is required"))
	}
	if !shaRE.MatchString(sha) || !validDigest(req.PolicyDigest) {
		e.addReason(ReasonInvalidRequest)
		return finish(errors.New("candidate SHA and policy digest are required and must be canonical"))
	}
	canonicalRoot, err := canonicalRoot(root)
	if err != nil {
		e.addReason(ReasonRoot)
		return finish(err)
	}
	if ref != "" {
		if err := validateRef(ref); err != nil {
			e.addReason(ReasonRef)
			return finish(err)
		}
	}
	ctx, cancel := context.WithTimeout(parent, limits.Timeout)
	defer cancel()
	if err := contextFailure(ctx); err != nil {
		e.addReason(reasonForContext(err))
		return finish(err)
	}
	// Every command is read-only. These settings suppress system/global config,
	// credential prompts, object replacement, network protocols, and locks.
	env := controlledGitEnv()
	run := func(args ...string) (CommandResult, error) {
		if err := contextFailure(ctx); err != nil {
			return CommandResult{}, err
		}
		var res CommandResult
		var err error
		if bounded, ok := s.Runner.(boundedRunner); ok {
			res, err = bounded.RunBounded(ctx, canonicalRoot, cloneEnv(env), limits.MaxOutput, args...)
		} else {
			res, err = s.Runner.Run(ctx, canonicalRoot, cloneEnv(env), args...)
		}
		if res.Truncated || errors.Is(err, io.ErrShortBuffer) || len(res.Output) > limits.MaxOutput {
			e.addReason(ReasonOutputTruncated)
			return res, errOutputTruncated
		}
		if err != nil {
			if ctxErr := contextFailure(ctx); ctxErr != nil {
				return res, ctxErr
			}
			return res, err
		}
		return res, nil
	}

	resolved, err := run("rev-parse", "--verify", "--quiet", sha+"^{commit}")
	if err != nil || strings.TrimSpace(resolved.Output) != sha {
		e.addReason(ReasonCandidate)
		if err == nil {
			err = errors.New("candidate does not resolve to the requested commit")
		}
		return finish(err)
	}
	if ref != "" {
		refName := ref
		if !strings.HasPrefix(refName, "refs/") {
			refName = "refs/heads/" + refName
		}
		resolvedRef, refErr := run("rev-parse", "--verify", "--quiet", refName+"^{commit}")
		if refErr != nil || strings.TrimSpace(resolvedRef.Output) != sha {
			e.addReason(ReasonRef)
			if refErr == nil {
				refErr = errors.New("candidate ref does not resolve to the requested commit")
			}
			return finish(refErr)
		}
	}

	commitsResult, err := run("rev-list", "--topo-order", "--reverse", sha)
	if err != nil {
		e.addReason(classifyGitError(err))
		return finish(err)
	}
	commits, err := parseObjectLines(commitsResult.Output)
	if err != nil || len(commits) == 0 {
		e.addReason(ReasonGitFailure)
		if err == nil {
			err = errors.New("candidate history is empty")
		}
		return finish(err)
	}
	sort.Strings(commits)
	containsCandidate := false
	for _, commit := range commits {
		if commit == sha {
			containsCandidate = true
			break
		}
	}
	if !containsCandidate {
		e.addReason(ReasonObjectSubstitute)
		return finish(errors.New("reachable history omitted the requested candidate"))
	}
	if len(commits) > limits.MaxCommits {
		e.addReason(ReasonCommitCount)
		return finish(errors.New("history commit limit exceeded"))
	}
	e.CommitsScanned = len(commits)
	entries := make(map[string][]entry, len(commits))
	seenEntries := make(map[string]bool, len(commits))
	blobObjects := make(map[string]bool, len(commits))
	treeObjects := make(map[string]bool, len(commits))
	entryCount := 0
	for _, commit := range commits {
		if commit != sha {
			_, ancestorErr := run("merge-base", "--is-ancestor", commit, sha)
			if ancestorErr != nil {
				e.addFinding(Finding{Category: "history", Reason: ReasonObjectSubstitute, Digest: commit})
				return finish(ancestorErr)
			}
		}
		commitType, commitTypeErr := run("cat-file", "-t", commit)
		if commitTypeErr != nil || strings.TrimSpace(commitType.Output) != "commit" {
			e.addFinding(Finding{Category: "object", Reason: ReasonObjectType, Digest: commit})
			if commitTypeErr == nil {
				commitTypeErr = errors.New("reachable history object is not a commit")
			}
			return finish(commitTypeErr)
		}
		rootTree, treeErr := run("rev-parse", "--verify", "--quiet", commit+"^{tree}")
		if treeErr != nil || !shaRE.MatchString(strings.TrimSpace(rootTree.Output)) {
			e.addReason(ReasonObjectUnavailable)
			if treeErr == nil {
				treeErr = errors.New("commit tree is unavailable")
			}
			return finish(treeErr)
		}
		treeObjects[strings.TrimSpace(rootTree.Output)] = true
		res, lsErr := run("ls-tree", "-r", "-z", "--full-tree", commit, "--")
		if lsErr != nil {
			e.addReason(classifyGitError(lsErr))
			return finish(lsErr)
		}
		parsed, parseErr := parseTree(res.Output)
		if parseErr != nil {
			e.addReason(parseErr.reason)
			return finish(parseErr)
		}
		for _, item := range parsed {
			entryCount++
			if entryCount > limits.MaxEntries {
				e.addReason(ReasonEntryCount)
				return finish(errors.New("history tree entry limit exceeded"))
			}
			if item.mode == "160000" || item.kind == "commit" {
				e.addFinding(Finding{Path: item.path, Category: "submodule", Reason: ReasonSubmodule, Digest: item.oid})
				continue
			}
			if item.kind == "blob" {
				blobObjects[item.oid] = true
			}
			if item.mode == "120000" {
				e.addFinding(Finding{Path: item.path, Category: "symlink", Reason: ReasonSymlink, Digest: item.oid})
				continue
			}
			if item.kind != "blob" || (item.mode != "100644" && item.mode != "100755") {
				e.addFinding(Finding{Path: item.path, Category: "object", Reason: ReasonObjectType, Digest: item.oid})
				continue
			}
			// A blob can legitimately be shared by several paths (and modes)
			// across history. Preserve every unique path/mode for path-sensitive
			// scanning, while reading/verifying each object only once.
			entryKey := item.oid + "\x00" + item.path + "\x00" + item.mode
			if !seenEntries[entryKey] {
				seenEntries[entryKey] = true
				entries[item.oid] = append(entries[item.oid], item)
			}
		}
		trees, treesErr := run("ls-tree", "-d", "-r", "-z", "--full-tree", commit, "--")
		if treesErr != nil {
			e.addReason(classifyGitError(treesErr))
			return finish(treesErr)
		}
		dirs, dirsErr := parseTree(trees.Output)
		if dirsErr != nil {
			e.addReason(dirsErr.reason)
			return finish(dirsErr)
		}
		for _, item := range dirs {
			if item.kind == "commit" && item.mode == "160000" {
				continue
			}
			if item.kind != "tree" || item.mode != "040000" {
				e.addReason(ReasonObjectType)
				return finish(errors.New("malformed tree directory entry"))
			}
			treeObjects[item.oid] = true
		}
		if len(e.Findings) >= limitsForFindings(limits) {
			return finish(errors.New("history finding limit exceeded"))
		}
	}
	if len(blobObjects)+len(commits)+len(treeObjects) > limits.MaxObjects {
		e.addReason(ReasonObjectCount)
		return finish(errors.New("reachable object limit exceeded"))
	}
	e.ObjectsScanned = len(commits) + len(blobObjects) + len(treeObjects)
	treeIDs := make([]string, 0, len(treeObjects))
	for oid := range treeObjects {
		treeIDs = append(treeIDs, oid)
	}
	sort.Strings(treeIDs)
	for _, oid := range treeIDs {
		typeRes, typeErr := run("cat-file", "-t", oid)
		if typeErr != nil || strings.TrimSpace(typeRes.Output) != "tree" {
			e.addFinding(Finding{Category: "object", Reason: ReasonObjectType, Digest: oid})
			if typeErr == nil {
				typeErr = errors.New("reachable history object is not a tree")
			}
			return finish(typeErr)
		}
	}

	// Sorting by object ID makes scanning and evidence independent of Git's
	// traversal ordering.
	objectIDs := make([]string, 0, len(entries))
	for oid := range entries {
		objectIDs = append(objectIDs, oid)
	}
	sort.Strings(objectIDs)
	scanner := s.Scanner
	scanner.Limits = security.Limits{MaxFiles: 1, MaxFileBytes: limits.MaxBlobBytes, MaxTotalBytes: limits.MaxTotalBytes, MaxFindings: limits.MaxFindings, TimeBudget: limits.Timeout}
	scanner.BinaryPolicy = security.BinaryReject
	type blobMeta struct{ size int64 }
	metadata := make(map[string]blobMeta, len(objectIDs))
	plannedTotal := int64(0)
	// Validate every blob's type and size before reading any blob bytes.
	for _, oid := range objectIDs {
		items := entries[oid]
		if len(items) == 0 {
			continue
		}
		item := items[0]
		if err := contextFailure(ctx); err != nil {
			e.addReason(reasonForContext(err))
			return finish(err)
		}
		typeRes, typeErr := run("cat-file", "-t", oid)
		if typeErr != nil || strings.TrimSpace(typeRes.Output) != "blob" {
			e.addFinding(Finding{Path: item.path, Category: "object", Reason: ReasonObjectUnavailable, Digest: oid})
			if typeErr == nil {
				typeErr = errors.New("reachable object is not an available blob")
			}
			return finish(typeErr)
		}
		sizeRes, sizeErr := run("cat-file", "-s", oid)
		if sizeErr != nil {
			e.addFinding(Finding{Path: item.path, Category: "object", Reason: ReasonObjectUnavailable, Digest: oid})
			return finish(sizeErr)
		}
		size, parseErr := parseSize(sizeRes.Output)
		if parseErr != nil {
			e.addFinding(Finding{Path: item.path, Category: "object", Reason: ReasonObjectSize, Digest: oid})
			return finish(parseErr)
		}
		if size > limits.MaxBlobBytes {
			e.addFinding(Finding{Path: item.path, Category: "size", Reason: ReasonBlobSize, Digest: oid})
			continue
		}
		if plannedTotal+size > limits.MaxTotalBytes {
			e.addFinding(Finding{Path: item.path, Category: "size", Reason: ReasonTotalSize, Digest: oid})
			return finish(errors.New("reachable blob total limit exceeded"))
		}
		plannedTotal += size
		metadata[oid] = blobMeta{size: size}
	}
	e.TotalBytes = plannedTotal
	for _, oid := range objectIDs {
		meta, ok := metadata[oid]
		if !ok {
			continue
		}
		items := entries[oid]
		item := items[0]
		blobRes, blobErr := run("cat-file", "blob", oid)
		if blobErr != nil {
			e.addFinding(Finding{Path: item.path, Category: "object", Reason: ReasonObjectUnavailable, Digest: oid})
			return finish(blobErr)
		}
		blob := []byte(blobRes.Output)
		if int64(len(blob)) != meta.size || !sameObjectID(oid, blob) {
			e.addFinding(Finding{Path: item.path, Category: "object", Reason: ReasonObjectSubstitute, Digest: oid})
			return finish(errors.New("cat-file returned a substituted blob"))
		}
		for _, item := range items {
			if isLFSPointer(blob) {
				e.addFinding(Finding{Path: item.path, Category: "lfs", Reason: ReasonLFSPointer, Digest: oid})
				continue
			}
			scan := scanner.Scan(ctx, security.CandidateSnapshot{Files: []security.CandidateFile{{Path: item.path, Content: blob, Mode: parseMode(item.mode)}}})
			for _, code := range scan.ReasonCodes {
				e.addReason(code)
			}
			for _, f := range scan.Findings {
				e.addFinding(Finding{Path: f.Path, Category: f.Category, Reason: f.Code, Digest: oid})
			}
			if scan.Blocked && len(scan.Findings) == 0 {
				for _, code := range scan.ReasonCodes {
					e.addFinding(Finding{Path: item.path, Category: "scan", Reason: code, Digest: oid})
				}
			}
		}
		if ctxErr := contextFailure(ctx); ctxErr != nil {
			e.addReason(reasonForContext(ctxErr))
			return finish(ctxErr)
		}
	}
	if len(e.Findings) > 0 || len(e.ReasonCodes) > 0 {
		return finish(nil)
	}
	e.Blocked = false
	e.ReasonCodes = nil
	e.Findings = nil
	e.Digest = evidenceDigest(e)
	return e, nil
}

type entry struct{ oid, path, mode, kind string }
type parseTreeError struct {
	reason string
	error
}

func (e *parseTreeError) Error() string {
	if e.error != nil {
		return e.error.Error()
	}
	return e.reason
}

func parseTree(output string) ([]entry, *parseTreeError) {
	if output == "" {
		return nil, nil
	}
	parts := strings.Split(output, "\x00")
	if parts[len(parts)-1] != "" {
		return nil, &parseTreeError{reason: ReasonGitFailure, error: errors.New("unterminated ls-tree output")}
	}
	entries := make([]entry, 0, len(parts)-1)
	for _, record := range parts[:len(parts)-1] {
		fields := strings.SplitN(record, "\t", 2)
		if len(fields) != 2 {
			return nil, &parseTreeError{reason: ReasonGitFailure, error: errors.New("malformed ls-tree record")}
		}
		meta := strings.Fields(fields[0])
		if len(meta) != 3 || !modeRE.MatchString(meta[0]) || !shaRE.MatchString(meta[2]) || (meta[1] != "blob" && meta[1] != "commit" && meta[1] != "tree") {
			return nil, &parseTreeError{reason: ReasonObjectType, error: errors.New("malformed tree object metadata")}
		}
		if err := validPath(fields[1]); err != nil {
			return nil, &parseTreeError{reason: ReasonMalformedPath, error: err}
		}
		entries = append(entries, entry{oid: meta[2], path: fields[1], mode: meta[0], kind: meta[1]})
	}
	return entries, nil
}

func parseObjectLines(output string) ([]string, error) {
	lines := strings.Split(output, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(lines))
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if !shaRE.MatchString(line) {
			return nil, errors.New("malformed rev-list object ID")
		}
		if !seen[line] {
			seen[line] = true
			result = append(result, line)
		}
	}
	return result, nil
}

func (l Limits) withDefaults() Limits {
	if l.MaxObjects <= 0 {
		l.MaxObjects = defaultMaxObjects
	}
	if l.MaxCommits <= 0 {
		l.MaxCommits = defaultMaxCommits
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = defaultMaxEntries
	}
	if l.MaxBlobBytes <= 0 {
		l.MaxBlobBytes = defaultMaxBlobBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = defaultMaxTotal
	}
	if l.MaxFindings <= 0 {
		l.MaxFindings = defaultMaxFindings
	}
	if l.MaxOutput <= 0 {
		l.MaxOutput = defaultMaxOutput
	}
	if l.Timeout <= 0 {
		l.Timeout = 2 * time.Minute
	}
	return l
}

func (s HistoryScanner) version() string {
	if s.ScannerVersion != "" {
		return s.ScannerVersion
	}
	return ScannerVersion
}
func (e *Evidence) addReason(code string) {
	if code != "" {
		e.ReasonCodes = append(e.ReasonCodes, code)
	}
}
func (e *Evidence) addFinding(f Finding) bool {
	if e.findingLimit > 0 && len(e.Findings) >= e.findingLimit {
		e.addReason(ReasonFindingLimit)
		return false
	}
	e.Findings = append(e.Findings, f)
	e.addReason(f.Reason)
	return true
}
func limitsForFindings(l Limits) int {
	if l.MaxFindings > 0 {
		return l.MaxFindings
	}
	return defaultMaxFindings
}
func uniqueSorted(values []string) []string {
	m := map[string]bool{}
	for _, v := range values {
		if v != "" {
			m[v] = true
		}
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func validDigest(s string) bool {
	return policyRE.MatchString(s)
}

func evidenceDigest(e Evidence) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", e.CandidateSHA, e.CandidateRef, e.PolicyDigest, e.Scanner)
	for _, code := range uniqueSorted(e.ReasonCodes) {
		fmt.Fprintf(h, "r:%s\x00", code)
	}
	findings := append([]Finding(nil), e.Findings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].Reason != findings[j].Reason {
			return findings[i].Reason < findings[j].Reason
		}
		return findings[i].Digest < findings[j].Digest
	})
	for _, f := range findings {
		fmt.Fprintf(h, "f:%s\x00%s\x00%s\x00%s\x00", f.Path, f.Category, f.Reason, f.Digest)
	}
	fmt.Fprintf(h, "c:%d\x00o:%d\x00b:%d\x00t:%t\x00", e.CommitsScanned, e.ObjectsScanned, e.TotalBytes, e.Blocked)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func canonicalRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", errors.New("repository root must be absolute")
	}
	abs := filepath.Clean(root)
	eval, err := filepath.EvalSymlinks(abs)
	if err != nil || eval != abs {
		return "", errors.New("repository root must be canonical")
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", errors.New("repository root must be a directory")
	}
	return abs, nil
}
func validateRef(ref string) error {
	if ref == "" || strings.ContainsAny(ref, "\\\x00\r\n") || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || strings.HasSuffix(ref, "/") {
		return errors.New("invalid candidate ref")
	}
	if strings.HasPrefix(ref, "refs/") && !strings.HasPrefix(ref, "refs/heads/") {
		return errors.New("candidate ref must be a branch")
	}
	if !refPartRE.MatchString(strings.TrimPrefix(ref, "refs/heads/")) {
		return errors.New("invalid candidate ref")
	}
	return nil
}
func validPath(p string) error {
	if p == "" || !utf8.ValidString(p) || filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00\r\n") || strings.HasSuffix(p, "/") {
		return errors.New("malformed Git path")
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("control character in Git path")
		}
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("unsafe Git path")
		}
	}
	return nil
}
func parseSize(s string) (int64, error) {
	n := strings.TrimSpace(s)
	if n == "" || strings.ContainsAny(n, "+-. \\t\r\n") {
		return 0, errors.New("invalid object size")
	}
	v, err := strconv.ParseInt(n, 10, 64)
	if err != nil || v < 0 {
		return 0, errors.New("invalid object size")
	}
	return v, nil
}
func parseMode(m string) uint32 { n, _ := strconv.ParseUint(m, 8, 32); return uint32(n) }
func sameObjectID(oid string, b []byte) bool {
	var h hash.Hash
	if len(oid) == 40 {
		h = sha1.New()
	} else if len(oid) == 64 {
		h = sha256.New()
	} else {
		return false
	}
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)) == oid
}
func isLFSPointer(b []byte) bool {
	s := string(b)
	return strings.HasPrefix(s, "version https://git-lfs.github.com/spec/v1\n") && strings.Contains(s, "\noid sha256:") && strings.Contains(s, "\nsize ")
}
func controlledGitEnv() map[string]string {
	return map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_TERMINAL_PROMPT": "0", "GIT_OPTIONAL_LOCKS": "0", "GIT_NO_REPLACE_OBJECTS": "1", "GIT_PROTOCOL_FROM_USER": "0", "GIT_ALLOW_PROTOCOL": "none", "GIT_ATTR_NOSYSTEM": "1"}
}

func cloneEnv(env map[string]string) map[string]string {
	copy := make(map[string]string, len(env))
	for key, value := range env {
		copy[key] = value
	}
	return copy
}
func contextFailure(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
func reasonForContext(err error) string {
	if errors.Is(err, context.Canceled) {
		return ReasonCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	return ReasonGitFailure
}
func classifyGitError(err error) string {
	if errors.Is(err, errOutputTruncated) {
		return ReasonOutputTruncated
	}
	if errors.Is(err, context.Canceled) {
		return ReasonCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	return ReasonGitFailure
}

var errOutputTruncated = errors.New("git output truncated")
