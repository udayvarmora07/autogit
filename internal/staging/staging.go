// Package staging builds an AutoGit-owned candidate without touching the
// caller's index. Snapshots are intentionally content-addressable values.
package staging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Snapshot is a content observation at a baseline/current boundary. A caller
// that cannot prove a path was clean at baseline should leave it out of the
// owned set; BuildPlan then conservatively blocks changed pre-existing paths.
type Snapshot map[string]string
type Change struct {
	Path, Operation, PreviousPath string
	Content                       string
}
type Plan struct {
	Paths    []string
	Changes  []Change
	Digest   string
	BaseTree string
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
		if had && exists && old != now {
			return Plan{}, fmt.Errorf("ambiguous ownership for %q", name)
		}
		if !exists && !had {
			continue
		}
		op := "added"
		if had && !exists {
			op = "deleted"
		} else if had {
			op = "modified"
		}
		p.Paths = append(p.Paths, name)
		p.Changes = append(p.Changes, Change{Path: name, Operation: op, Content: now})
	}
	sort.Strings(p.Paths)
	sort.Slice(p.Changes, func(i, j int) bool { return p.Changes[i].Path < p.Changes[j].Path })
	h := sha256.New()
	for _, c := range p.Changes {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", c.Path, c.Operation, c.Content)
	}
	p.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return p, nil
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
