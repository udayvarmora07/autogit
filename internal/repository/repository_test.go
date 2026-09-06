package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
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

func TestDiscoverUsesHardenedGitRunnerForLinkedWorktreeValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-specific")
	}
	mainRoot := t.TempDir()
	mainGit := filepath.Join(mainRoot, ".git")
	linkedRoot := t.TempDir()
	linkedGit := filepath.Join(mainGit, "worktrees", "linked")
	for _, directory := range []string{mainGit, filepath.Dir(linkedGit), linkedGit} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(linkedGit, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedRoot, ".git"), []byte("gitdir: "+linkedGit+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "git.log")
	fakeGitDir := t.TempDir()
	fakeGit := filepath.Join(fakeGitDir, "git")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n%%s\\n' %q %q\nprintf '%%s\\n' \"$*\" > %q\n", linkedRoot, linkedGit, logPath)
	if err := os.WriteFile(fakeGit, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeGitDir)
	if _, err := DiscoverWithKey(linkedRoot, []byte("test-identity-key")); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"core.hooksPath=/dev/null", "core.sshCommand=", "credential.helper=", "rev-parse --path-format=absolute --show-toplevel --git-dir"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("hardened Git args=%q missing %q", args, want)
		}
	}
}
