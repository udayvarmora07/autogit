package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/gitport"
)

type initRunner struct {
	calls [][]string
	err   error
}

func (r *initRunner) Run(_ context.Context, _ string, args ...string) (gitport.Result, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return gitport.Result{}, r.err
}

func TestResolveUninitializedRootRejectsNestedAndExistingRepositories(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(parent, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveUninitializedRoot(nested); err == nil {
		t.Fatal("nested repository target accepted")
	}

	existing := filepath.Join(t.TempDir(), ".git")
	if err := os.Mkdir(existing, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveUninitializedRoot(filepath.Dir(existing)); err == nil {
		t.Fatal("existing repository target accepted")
	}

	bare := t.TempDir()
	for _, name := range []string{"HEAD", "config", "description"} {
		if err := os.WriteFile(filepath.Join(bare, name), []byte("placeholder\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"objects", "refs"} {
		if err := os.Mkdir(filepath.Join(bare, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ResolveUninitializedRoot(bare); err == nil {
		t.Fatal("bare repository target accepted")
	}
}

func TestInitializeUsesExplicitBranchAndMergesDetectedHygiene(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original := "# user rules\nnode_modules/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &initRunner{}
	if _, err := Initialize(context.Background(), runner, root, "trunk"); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], "\x00") != "init\x00--initial-branch=trunk" {
		t.Fatalf("calls=%#v", runner.calls)
	}
	got, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), original) || !strings.Contains(string(got), "# BEGIN AUTOGIT MANAGED") || !strings.Contains(string(got), ".env") {
		t.Fatalf("merged gitignore=%q", got)
	}
}

func TestInitializeCreatesMinimalReadmeOnlyWhenAbsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sample-project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	runner := &initRunner{}
	result, err := Initialize(context.Background(), runner, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(readme), "# sample-project\n") || !containsString(result.Hygiene, "README.md") {
		t.Fatalf("readme=%q hygiene=%v", readme, result.Hygiene)
	}

	rootWithReadme := t.TempDir()
	original := "# User project\n\nUsage is documented here.\n"
	if err := os.WriteFile(filepath.Join(rootWithReadme, "README.md"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), &initRunner{}, rootWithReadme, "main"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(rootWithReadme, "README.md"))
	if err != nil || string(got) != original {
		t.Fatalf("existing readme changed=%q err=%v", got, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestInitializeDoesNotWriteHygieneWhenGitInitFails(t *testing.T) {
	root := t.TempDir()
	runner := &initRunner{err: errors.New("git failed")}
	if _, err := Initialize(context.Background(), runner, root, "main"); err == nil {
		t.Fatal("failed git init accepted")
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hygiene written after failed init: %v", err)
	}
}

func TestInitializeRejectsInvalidHygieneBeforeGitMutation(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &initRunner{}
	if _, err := Initialize(context.Background(), runner, root, "main"); err == nil {
		t.Fatal("invalid hygiene accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("git mutation occurred before hygiene validation: %#v", runner.calls)
	}
}

func TestApplyHygieneRejectsConcurrentUserChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(path, []byte("# original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# user changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyHygiene(plan); err == nil {
		t.Fatal("concurrent hygiene change accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# user changed\n" {
		t.Fatalf("concurrent user content was overwritten: %q", got)
	}
}

func TestFutureRepositoryIDMatchesDiscoveryAfterGitInit(t *testing.T) {
	root := t.TempDir()
	key := []byte(strings.Repeat("k", 32))
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	want, err := FutureRepositoryID(root, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoID != want {
		t.Fatalf("future repository id=%q discovered=%q", want, got.RepoID)
	}
}
