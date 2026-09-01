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
	"regexp"
	"sort"
	"strings"

	"autogit/internal/gittransaction"
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
	Paths     []string
	Changes   []Change
	Digest    string
	BaseTree  string
	candidate []SnapshotEntry
}
type Candidate struct {
	Digest                string // canonical digest derived from TreeOID
	TreeOID               string
	BaseDigest, IndexPath string
	Paths                 []string
}
type Result struct{ Output string }
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
		now, exists := current[name]
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
	for _, c := range p.Changes {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", c.Path, c.Operation, c.Content)
	}
	p.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return p, nil
}

func asObserved(snapshot Snapshot) ObservedSnapshot {
	observed := make(ObservedSnapshot, len(snapshot))
	for path, content := range snapshot {
		observed[path] = ObservedFile{Content: []byte(content), Mode: 0644}
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
