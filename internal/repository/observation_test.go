package repository

import (
	"context"
	"os"
	"os/exec"
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

func TestObservationEnvironmentDoesNotInheritCredentials(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	env := strings.Join(observationEnvironment(nil), "\n")
	for _, name := range []string{"GH_TOKEN=", "AWS_SECRET_ACCESS_KEY="} {
		if strings.Contains(env, name) {
			t.Fatalf("credential leaked to observation environment: %s", name)
		}
	}
	for _, name := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull} {
		if !strings.Contains(env, name) {
			t.Fatalf("git config isolation missing: %s", name)
		}
	}
}

func TestCaptureCommittedFilesReadsOnlyRequestedRegularTreeEntries(t *testing.T) {
	root := t.TempDir()
	head := strings.Repeat("a", 40)
	object := strings.Repeat("b", 40)
	runner := &observationRunner{outputs: map[string]string{
		"ls-tree\x00-z\x00--full-tree\x00" + head + "\x00--\x00owned.txt": "100755 blob " + object + "\towned.txt\x00",
		"cat-file\x00blob\x00" + object:                                   "committed content\n",
	}}
	files, err := CaptureCommittedFiles(context.Background(), runner, root, head, []string{"owned.txt"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := files["owned.txt"]
	if !ok || !file.Present || string(file.Content) != "committed content\n" || file.Mode.Perm() != 0755 {
		t.Fatalf("files=%+v", files)
	}
}

func TestCaptureCommittedFilesRejectsUnsafeTreeEntries(t *testing.T) {
	root := t.TempDir()
	head := strings.Repeat("a", 40)
	object := strings.Repeat("b", 40)
	for name, tree := range map[string]string{
		"symlink":   "120000 blob " + object + "\towned.txt\x00",
		"directory": "040000 tree " + object + "\towned.txt\x00",
	} {
		t.Run(name, func(t *testing.T) {
			runner := &observationRunner{outputs: map[string]string{
				"ls-tree\x00-z\x00--full-tree\x00" + head + "\x00--\x00owned.txt": tree,
			}}
			if _, err := CaptureCommittedFiles(context.Background(), runner, root, head, []string{"owned.txt"}, 1<<20); err == nil {
				t.Fatal("unsafe tree entry accepted")
			}
		})
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

func TestCaptureBaselineWithOptionsRejectsOversizedObservedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("123456"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, ".git", "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "?? large.txt\x00",
	}}
	if _, err := CaptureBaselineWithOptions(context.Background(), runner, root, BaselineOptions{MaxFileSize: 5}); err == nil {
		t.Fatal("oversized baseline file accepted")
	}
}

func TestCaptureBaselineWithOptionsCapturesExplicitCleanPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, ".git", "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "",
	}}
	baseline, err := CaptureBaselineWithOptions(context.Background(), runner, root, BaselineOptions{Paths: []string{"tracked.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	file, ok := baseline.Files["tracked.txt"]
	if !ok || !file.Present || string(file.Content) != "baseline\n" {
		t.Fatalf("explicit baseline file=%+v present=%v", file, ok)
	}
}

func TestCaptureBaselineRejectsExplicitIgnoredPath(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("not for the candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureBaselineWithOptions(context.Background(), SystemRunner{}, root, BaselineOptions{Paths: []string{"ignored.txt"}}); err == nil {
		t.Fatal("explicit ignored path was accepted")
	}
}

func TestCaptureBaselineRejectsFileReplacementDuringRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("replacement race fixture requires Unix rename semantics")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "race.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &observationRunner{outputs: map[string]string{
		"rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":       "0123456789012345678901234567890123456789\n",
		"rev-parse\x00--git-path\x00index":                        filepath.Join(root, ".git", "index") + "\n",
		"status\x00--porcelain=v1\x00-z\x00--untracked-files=all": "?? race.txt\x00",
	}}
	if _, err := CaptureBaselineWithOptions(context.Background(), runner, root, BaselineOptions{BeforeRead: func(string) {
		replacement := filepath.Join(root, "replacement.txt")
		if writeErr := os.WriteFile(replacement, []byte("replacement\n"), 0600); writeErr != nil {
			t.Fatalf("replacement write: %v", writeErr)
		}
		if renameErr := os.Rename(replacement, path); renameErr != nil {
			t.Fatalf("replacement rename: %v", renameErr)
		}
	}}); err == nil {
		t.Fatal("file replacement during baseline capture was accepted")
	}
}

func TestSystemRunnerExecutesReadOnlyGitWithArguments(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	runner := SystemRunner{}
	result, err := runner.Run(context.Background(), root, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got := filepath.Clean(filepath.FromSlash(strings.TrimSpace(result.Output)))
	want := filepath.Clean(canonical)
	if got != want {
		t.Fatalf("output=%q want=%q", got, want)
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
