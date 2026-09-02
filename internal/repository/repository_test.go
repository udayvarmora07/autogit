package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRejectsHomeAndFilesystemRoot(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, root := range []string{home, string(filepath.Separator)} {
		if _, err := Discover(root); err == nil {
			t.Fatalf("Discover(%q) accepted protected root", root)
		}
	}
}

func TestDiscoverRejectsRepositoryFoundAtHomeAfterWalkingUp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root); err == nil {
		t.Fatal("repository rooted at HOME was accepted")
	}
}

func TestDiscoverCanonicalizesRepositoryAndProvidesStableIdentities(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := Discover(filepath.Join(root, "."))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if a.Root != root || a.RepoID != b.RepoID || a.WorktreeID != b.WorktreeID {
		t.Fatalf("identities not canonical: a=%+v b=%+v", a, b)
	}
}
