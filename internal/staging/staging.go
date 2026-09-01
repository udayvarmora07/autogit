// Package staging builds an AutoGit-owned candidate without touching the
// caller's index. Snapshots are intentionally content-addressable values.
package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"autogit/internal/gittransaction"
	"autogit/internal/repository"
)

// Snapshot is a content observation at a baseline/current boundary. A caller
// that cannot prove a path was clean at baseline should leave it out of the
// owned set; BuildPlan then conservatively blocks changed pre-existing paths.
type Snapshot map[string]string

// ObservedFile is an immutable filesystem observation captured by a trusted
// caller. Mode is retained so an owned executable is not silently committed as
// a non-executable file. A zero mode is normalized to 0644 for callers that
// only have content observations.
type ObservedFile struct {
	Content []byte
	Mode    os.FileMode
	Present bool
}

// ObservedSnapshot is the richer baseline/current form used when filesystem
// mode is available at the observation boundary.
type ObservedSnapshot map[string]ObservedFile

type Change struct {
	Path, Operation, PreviousPath string
	Content                       string
}

// SnapshotEntry is the transaction-owned candidate representation. Keeping
// this alias means a derived snapshot can be passed directly to workflow
// without a second, lossy conversion boundary.
type SnapshotEntry = gittransaction.SnapshotEntry

type Plan struct {
	Paths           []string
	Changes         []Change
	Digest          string
	BaseTree        string
	candidate       []SnapshotEntry
	ownershipDigest string
}
type Candidate struct {
	Digest                string // canonical digest derived from TreeOID
	TreeOID               string
	BaseDigest, IndexPath string
	Paths                 []string
}
type Result struct{ Output string }

type CaptureOptions struct {
	// BeforeRead is intended for deterministic fault-injection tests and is
	// never needed by normal callers.
	BeforeRead  func(path string)
	MaxFileSize int64
}

const defaultMaxCaptureSize int64 = 16 << 20

type Runner interface {
	Run(context.Context, string, map[string]string, ...string) (Result, error)
}

func BuildPlan(baseline, current Snapshot, requested []string) (Plan, error) {
	return BuildObservedPlan(asObserved(baseline), asObserved(current), requested)
}

// BuildObservedPlan derives an owned candidate from captured baseline/current
// observations. Baseline entries represent pre-existing user state: any edit
// or deletion of one remains ambiguous and is never included automatically.
func BuildObservedPlan(baseline, current ObservedSnapshot, requested []string) (Plan, error) {
	seen := map[string]bool{}
	p := Plan{}
	for _, name := range requested {
		if err := safePath(name); err != nil {
			return Plan{}, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		old, had := baseline[name]
		now, observed := current[name]
		exists := observed && observedPresent(now)
		if had {
			if !exists || !sameObservation(old, now) {
				// Baseline entries denote pre-existing changed paths. A
				// later edit or deletion cannot be attributed to this
				// candidate without an explicit ownership decision.
				return Plan{}, fmt.Errorf("ambiguous ownership for %q", name)
			}
			// A path that was present and unchanged at the boundary is
			// not candidate work.
			continue
		}
		if !exists && !had {
			continue
		}
		op := "added"
		p.Paths = append(p.Paths, name)
		p.Changes = append(p.Changes, Change{Path: name, Operation: op, Content: string(now.Content)})
		p.candidate = append(p.candidate, SnapshotEntry{Path: name, Content: append([]byte(nil), now.Content...), Mode: normalizedMode(now.Mode)})
	}
	sort.Strings(p.Paths)
	sort.Slice(p.Changes, func(i, j int) bool { return p.Changes[i].Path < p.Changes[j].Path })
	sort.Slice(p.candidate, func(i, j int) bool { return p.candidate[i].Path < p.candidate[j].Path })
	h := sha256.New()
	for _, entry := range p.candidate {
		fmt.Fprintf(h, "%s\x00%o\x00%t\x00", entry.Path, uint32(entry.Mode), entry.Delete)
		if !entry.Delete {
			_, _ = h.Write(entry.Content)
		}
		_, _ = h.Write([]byte{0})
	}
	p.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	p.ownershipDigest = p.Digest
	return p, nil
}

// OwnershipDigest returns the plan identity derived from its private immutable
// candidate snapshot. Unlike the public Digest display field, it cannot be
// altered by a caller after plan construction.
func (p Plan) OwnershipDigest() string { return p.ownershipDigest }

func asObserved(snapshot Snapshot) ObservedSnapshot {
	observed := make(ObservedSnapshot, len(snapshot))
	for path, content := range snapshot {
		observed[path] = ObservedFile{Content: []byte(content), Mode: 0644, Present: true}
	}
	return observed
}

func sameObservation(left, right ObservedFile) bool {
	return normalizedMode(left.Mode) == normalizedMode(right.Mode) && bytes.Equal(left.Content, right.Content)
}

func normalizedMode(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0644
	}
	return mode
}

func observedPresent(file ObservedFile) bool {
	return file.Present || file.Mode != 0 || file.Content != nil
}

// BuildCapturedPlanFromBaseline converts the real repository observation into
// the staging ownership boundary and captures only the explicitly requested
// current paths. Baseline file bytes remain in memory and are not persisted by
// this package.
func BuildCapturedPlanFromBaseline(root string, baseline repository.Baseline, requested []string) (Plan, error) {
	ownedBaseline := make(ObservedSnapshot, len(baseline.Files))
	for name, file := range baseline.Files {
		ownedBaseline[name] = ObservedFile{Content: append([]byte(nil), file.Content...), Mode: file.Mode, Present: file.Present}
	}
	return BuildCapturedPlan(root, ownedBaseline, requested)
}

// CaptureObservedFiles reads an explicit set of regular files beneath root
// into immutable observations. It does not walk a directory or infer paths;
// callers must establish ownership before asking it to capture bytes.
func CaptureObservedFiles(root string, paths []string) (ObservedSnapshot, error) {
	return CaptureObservedFilesWithOptions(root, paths, CaptureOptions{})
}

// CaptureObservedFilesWithOptions is the race-aware capture boundary. It
// checks the same path before and after reading, rejects replacement by a
// different inode or file kind, and bounds bytes read into memory.
func CaptureObservedFilesWithOptions(root string, paths []string, options CaptureOptions) (ObservedSnapshot, error) {
	if root == "" {
		return nil, errors.New("capture root is required")
	}
	canonicalRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonicalRoot, err = filepath.EvalSymlinks(canonicalRoot)
	if err != nil {
		return nil, fmt.Errorf("capture root: %w", err)
	}
	rootInfo, err := os.Stat(canonicalRoot)
	if err != nil || !rootInfo.IsDir() {
		return nil, errors.New("capture root is not a directory")
	}
	captured := make(ObservedSnapshot, len(paths))
	maxFileSize := options.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = defaultMaxCaptureSize
	}
	for _, name := range paths {
		if err := safePath(name); err != nil {
			return nil, err
		}
		if _, exists := captured[name]; exists {
			continue
		}
		absolute := filepath.Join(canonicalRoot, filepath.FromSlash(name))
		relative, err := filepath.Rel(canonicalRoot, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return nil, errors.New("capture path escapes root")
		}
		if err := rejectSymlinkComponents(canonicalRoot, filepath.FromSlash(name)); err != nil {
			return nil, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, fmt.Errorf("capture %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("capture %q: not a regular file", name)
		}
		if info.Size() > maxFileSize {
			return nil, fmt.Errorf("capture %q exceeds capture limit", name)
		}
		if options.BeforeRead != nil {
			options.BeforeRead(name)
		}
		beforeRead, err := os.Lstat(absolute)
		if err != nil {
			return nil, fmt.Errorf("capture %q changed during capture: %w", name, err)
		}
		if !sameFileObservation(info, beforeRead) || !beforeRead.Mode().IsRegular() {
			return nil, fmt.Errorf("capture %q changed during capture", name)
		}
		content, err := os.ReadFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("capture %q: %w", name, err)
		}
		afterRead, err := os.Lstat(absolute)
		if err != nil || afterRead == nil || !sameFileObservation(beforeRead, afterRead) || !afterRead.Mode().IsRegular() {
			return nil, fmt.Errorf("capture %q changed during capture", name)
		}
		if int64(len(content)) > maxFileSize {
			return nil, fmt.Errorf("capture %q exceeds capture limit", name)
		}
		captured[name] = ObservedFile{Content: append([]byte(nil), content...), Mode: info.Mode(), Present: true}
	}
	return captured, nil
}

func sameFileObservation(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func rejectSymlinkComponents(root, relative string) error {
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("capture path contains a symlink")
		}
	}
	return nil
}

// BuildCapturedPlan captures the current bytes for explicit requested paths,
// then applies the same fail-closed baseline ownership rules as
// BuildObservedPlan.
func BuildCapturedPlan(root string, baseline ObservedSnapshot, requested []string) (Plan, error) {
	current, err := CaptureObservedFiles(root, requested)
	if err != nil {
		return Plan{}, err
	}
	return BuildObservedPlan(baseline, current, requested)
}

// CandidateSnapshot returns a deep copy of the exact current observations
// selected by this plan. The returned slice and byte contents are owned by
// the caller; mutating them cannot alter the plan or the source snapshots.
func (p Plan) CandidateSnapshot() []SnapshotEntry {
	entries := make([]SnapshotEntry, len(p.candidate))
	for i, entry := range p.candidate {
		entries[i] = SnapshotEntry{
			Path:    entry.Path,
			Content: append([]byte(nil), entry.Content...),
			Mode:    entry.Mode,
			Delete:  entry.Delete,
		}
	}
	return entries
}

func BuildCandidate(ctx context.Context, r Runner, dir, index string, p Plan) (Candidate, error) {
	if r == nil || dir == "" || index == "" {
		return Candidate{}, errors.New("candidate runner, directory, and index are required")
	}
	env := map[string]string{"GIT_INDEX_FILE": index}
	base := p.BaseTree
	if base == "" {
		base = "HEAD"
	}
	if base != "HEAD" && !gitTreeDigest.MatchString(base) {
		return Candidate{}, errors.New("invalid base tree")
	}
	if _, err := r.Run(ctx, dir, env, "read-tree", "--reset", base); err != nil {
		return Candidate{}, err
	}
	args := []string{"add", "--"}
	args = append(args, p.Paths...)
	if _, err := r.Run(ctx, dir, env, args...); err != nil {
		return Candidate{}, err
	}
	res, err := r.Run(ctx, dir, env, "write-tree")
	if err != nil {
		return Candidate{}, err
	}
	digest := strings.TrimSpace(res.Output)
	if !gitTreeDigest.MatchString(digest) {
		return Candidate{}, errors.New("write-tree returned invalid digest")
	}
	h := sha256.Sum256([]byte("git-tree\x00" + digest))
	return Candidate{Digest: "sha256:" + hex.EncodeToString(h[:]), TreeOID: digest, BaseDigest: base, IndexPath: index, Paths: append([]string(nil), p.Paths...)}, nil
}
func safePath(s string) error {
	if s == "" || strings.IndexByte(s, 0) >= 0 || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "\\") || path.IsAbs(s) {
		return errors.New("unsafe path")
	}
	if len(s) >= 2 && s[1] == ':' {
		return errors.New("unsafe path")
	}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "." || part == ".." {
			return errors.New("unsafe path")
		}
	}
	return nil
}

var gitTreeDigest = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
