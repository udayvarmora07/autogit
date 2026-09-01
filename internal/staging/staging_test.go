package staging

import (
	"context"
	"strings"
	"testing"
)

func TestOwnershipExcludesPreexistingAndReportsOverlap(t *testing.T) {
	baseline := Snapshot{"keep.txt": "old", "shared.txt": "old"}
	current := Snapshot{"keep.txt": "old", "shared.txt": "new", "new.txt": "new"}
	plan, err := BuildPlan(baseline, current, []string{"new.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Paths) != 1 || plan.Paths[0] != "new.txt" {
		t.Fatalf("paths %#v", plan.Paths)
	}
	if _, err := BuildPlan(baseline, current, []string{"shared.txt"}); err == nil {
		t.Fatal("overlap was silently owned")
	}
}

func TestCandidateUsesIsolatedIndexAndArgumentSafePaths(t *testing.T) {
	r := &recordRunner{outputs: []string{"", "", "0123456789abcdef0123456789abcdef01234567"}}
	plan, err := BuildPlan(Snapshot{}, Snapshot{"weird name.txt": "x"}, []string{"weird name.txt"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := BuildCandidate(context.Background(), r, "/repo", "/tmp/autogit-index", plan)
	if err != nil {
		t.Fatal(err)
	}
	if c.Digest == "" || len(r.args) == 0 {
		t.Fatal("candidate not built")
	}
	if r.env["GIT_INDEX_FILE"] != "/tmp/autogit-index" {
		t.Fatalf("env %#v", r.env)
	}
	seenSep := false
	for _, call := range r.calls {
		for _, a := range call {
			if a == "--" {
				seenSep = true
			}
			if a == "weird name.txt" && !seenSep {
				t.Fatal("path appeared before argument separator")
			}
		}
	}
	if len(r.calls) < 3 || r.calls[0][0] != "read-tree" || r.calls[1][0] != "add" || r.calls[2][0] != "write-tree" {
		t.Fatalf("unexpected candidate argv: %#v", r.calls)
	}
	if !seenSep {
		t.Fatalf("missing argument separator: %#v", r.args)
	}
}

func TestCandidateRejectsNonGitObjectWidths(t *testing.T) {
	for _, width := range []int{39, 41, 63, 65} {
		r := &recordRunner{outputs: []string{"", "", strings.Repeat("a", width)}}
		plan, err := BuildPlan(Snapshot{}, Snapshot{"a.txt": "x"}, []string{"a.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := BuildCandidate(context.Background(), r, "/repo", "/tmp/index", plan); err == nil {
			t.Errorf("tree width %d accepted", width)
		}
	}
}

type recordRunner struct {
	args    []string
	env     map[string]string
	outputs []string
	calls   [][]string
}

func (r *recordRunner) Run(_ context.Context, _ string, env map[string]string, args ...string) (Result, error) {
	r.args = args
	r.calls = append(r.calls, append([]string(nil), args...))
	r.env = env
	var out string
	if len(r.outputs) > 0 {
		out = r.outputs[0]
		r.outputs = r.outputs[1:]
	}
	return Result{Output: out}, nil
}
