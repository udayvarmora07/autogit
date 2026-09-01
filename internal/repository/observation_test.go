package repository

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type observationRunner struct {
	outputs map[string]string
	calls   [][]string
	dirs    []string
}

func (r *observationRunner) Run(_ context.Context, dir string, _ map[string]string, args ...string) (CommandResult, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	r.dirs = append(r.dirs, dir)
	key := strings.Join(args, "\x00")
	output, ok := r.outputs[key]
	if !ok {
		return CommandResult{}, os.ErrNotExist
	}
	return CommandResult{Output: output}, nil
}

func TestCaptureBaselineRecordsHeadIndexStatusAndOwnedFileFingerprints(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("candidate\n"), 0755); err != nil {
		t.Fatal(err)
	}
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, ".git", "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "?? new.txt\x00",
	}}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "index"), []byte("index-bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	baseline, err := CaptureBaseline(context.Background(), runner, root)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Head != "0123456789012345678901234567890123456789" || baseline.IndexDigest == "" || baseline.StatusDigest == "" || baseline.PathsDigest == "" {
		t.Fatalf("baseline=%+v", baseline)
	}
	f, ok := baseline.Files["new.txt"]
	wantMode := os.FileMode(0755)
	if runtime.GOOS == "windows" {
		wantMode = 0666
	}
	if !ok || !f.Present || f.Mode.Perm() != wantMode || string(f.Content) != "candidate\n" {
		t.Fatalf("file observation=%+v present=%v", f, ok)
	}
}

func TestCaptureBaselineCanonicalizesRootBeforeRunningGit(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, ".git", "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "",
	}}
	if _, err := CaptureBaseline(context.Background(), runner, alias); err != nil {
		t.Fatal(err)
	}
	for _, dir := range runner.dirs {
		if dir != root {
			t.Fatalf("git ran in non-canonical root %q", dir)
		}
	}
}

func TestCaptureBaselineIncludesDeletedAndRenamedPathsWithoutReadingOutsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, ".git", "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "D  deleted.txt\x00R  renamed.txt\x00old.txt\x00",
	}}
	baseline, err := CaptureBaseline(context.Background(), runner, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deleted.txt", "renamed.txt", "old.txt"} {
		if _, ok := baseline.Files[name]; !ok {
			t.Fatalf("baseline omitted %q: %#v", name, baseline.Files)
		}
	}
	if baseline.Files["deleted.txt"].Present {
		t.Fatal("deleted path marked present")
	}
}

func TestCaptureBaselineRejectsStatusPathThatEscapesRoot(t *testing.T) {
	root := t.TempDir()
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "?? ../outside\x00",
	}}
	if _, err := CaptureBaseline(context.Background(), runner, root); err == nil {
		t.Fatal("escaping status path accepted")
	}
}

func TestBaselineEventPayloadContainsOnlyBoundedEvidence(t *testing.T) {
	baseline := Baseline{Head: "0123456789012345678901234567890123456789", IndexDigest: "sha256:" + strings.Repeat("a", 64), StatusDigest: "sha256:" + strings.Repeat("b", 64), PathsDigest: "sha256:" + strings.Repeat("c", 64), Paths: []string{"secret.txt"}}
	payload := baseline.EventPayload()
	if len(payload) != 4 || payload["baseline_head"] != baseline.Head || payload["baseline_index"] != baseline.IndexDigest || payload["status_digest"] != baseline.StatusDigest || payload["baseline_paths_digest"] != baseline.PathsDigest {
		t.Fatalf("payload=%#v", payload)
	}
	if _, leaked := payload["paths"]; leaked {
		t.Fatal("baseline event payload leaked raw paths")
	}
}
