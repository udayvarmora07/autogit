package staging

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/gittransaction"
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

func TestPlanCandidateSnapshotIsolatedFromFilesystemObservations(t *testing.T) {
	baseline := Snapshot{
		"unchanged.txt": "same",
		"deleted.txt":   "before",
	}
	current := Snapshot{
		"unchanged.txt": "same",
		"deleted.txt":   "after",
		"new.txt":       "candidate",
	}

	plan, err := BuildPlan(baseline, current, []string{"unchanged.txt", "deleted.txt", "new.txt"})
	if err == nil {
		t.Fatal("ambiguous pre-existing edit was accepted")
	}

	delete(current, "deleted.txt")
	if _, err = BuildPlan(baseline, current, []string{"unchanged.txt", "deleted.txt", "new.txt"}); err == nil {
		t.Fatal("deletion of a pre-existing path was accepted")
	}
	current["deleted.txt"] = "before"
	plan, err = BuildPlan(baseline, current, []string{"unchanged.txt", "deleted.txt", "new.txt"})
	if err != nil {
		t.Fatal(err)
	}
	entries := plan.CandidateSnapshot()
	if len(entries) != 1 || entries[0].Path != "new.txt" || string(entries[0].Content) != "candidate" || entries[0].Delete {
		t.Fatalf("candidate snapshot = %#v", entries)
	}
	var workflowEntries []gittransaction.SnapshotEntry = entries
	if len(workflowEntries) != 1 || workflowEntries[0].Path != "new.txt" {
		t.Fatalf("snapshot is not workflow-consumable: %#v", workflowEntries)
	}

	// The derived bytes must not alias the current observation or a prior
	// caller-owned result.
	current["new.txt"] = "mutated after derivation"
	entries[0].Content[0] = 'X'
	plan.Changes[0].Content = "mutated exported plan"
	again := plan.CandidateSnapshot()
	if len(again) != 1 || string(again[0].Content) != "candidate" {
		t.Fatalf("candidate snapshot was not isolated: %#v", again)
	}
}

func TestBuildObservedPlanPreservesExecutableModeInCandidateSnapshot(t *testing.T) {
	plan, err := BuildObservedPlan(nil, ObservedSnapshot{
		"script.sh": {Content: []byte("#!/bin/sh\necho ok\n"), Mode: 0755},
	}, []string{"script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	entries := plan.CandidateSnapshot()
	if len(entries) != 1 || entries[0].Path != "script.sh" || entries[0].Mode != os.FileMode(0755) {
		t.Fatalf("candidate snapshot = %#v", entries)
	}
}

func TestBuildObservedPlanDigestBindsObservedMode(t *testing.T) {
	regular, err := BuildObservedPlan(nil, ObservedSnapshot{
		"script.sh": {Content: []byte("echo same\n"), Mode: 0644},
	}, []string{"script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	executable, err := BuildObservedPlan(nil, ObservedSnapshot{
		"script.sh": {Content: []byte("echo same\n"), Mode: 0755},
	}, []string{"script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if regular.Digest == executable.Digest {
		t.Fatalf("mode change reused ownership digest %q", regular.Digest)
	}
}

func TestCaptureObservedFilesCopiesRegularFileContentAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho initial\n"), 0755); err != nil {
		t.Fatal(err)
	}
	captured, err := CaptureObservedFiles(root, []string{"script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if got := captured["script.sh"]; string(got.Content) != "#!/bin/sh\necho initial\n" || got.Mode.Perm() != 0755 {
		t.Fatalf("captured file = %#v", got)
	}
	if err := os.WriteFile(path, []byte("changed later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := captured["script.sh"]; string(got.Content) != "#!/bin/sh\necho initial\n" || got.Mode.Perm() != 0755 {
		t.Fatalf("captured file changed with filesystem: %#v", got)
	}
}

func TestCaptureObservedFilesRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("target\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := CaptureObservedFiles(root, []string{"link.txt"}); err == nil {
		t.Fatal("symlink was accepted as a candidate file")
	}
}

func TestCaptureObservedFilesRejectsSymlinkComponent(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	if err := os.Mkdir(targetDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("target\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := CaptureObservedFiles(root, []string{"linked/file.txt"}); err == nil {
		t.Fatal("symlinked parent directory was accepted")
	}
}

func TestBuildCapturedPlanDerivesOwnedSnapshotFromFilesystem(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho owned\n"), 0755); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildCapturedPlan(root, nil, []string{"new.sh"})
	if err != nil {
		t.Fatal(err)
	}
	entries := plan.CandidateSnapshot()
	if len(entries) != 1 || entries[0].Path != "new.sh" || entries[0].Mode.Perm() != 0755 || string(entries[0].Content) != "#!/bin/sh\necho owned\n" {
		t.Fatalf("candidate snapshot = %#v", entries)
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
