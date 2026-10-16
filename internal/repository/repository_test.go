package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	// Real git records the worktree back-pointer that binds this git
	// directory to the requested worktree root.
	if err := os.WriteFile(filepath.Join(linkedGit, "gitdir"), []byte(filepath.Join(linkedRoot, ".git")+"\n"), 0600); err != nil {
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
	for _, want := range []string{"core.hooksPath=", "core.sshCommand=", "credential.helper=", "rev-parse --path-format=absolute --show-toplevel --git-dir"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("hardened Git args=%q missing %q", args, want)
		}
	}
	escape := t.TempDir()
	if err := os.WriteFile(filepath.Join(linkedGit, "commondir"), []byte(escape+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverWithKey(linkedRoot, []byte("test-identity-key")); err == nil {
		t.Fatal("linked worktree accepted a commondir outside its Git directory")
	}
}

func TestDiscoverWithKeyContextPropagatesCancellationToLinkedWorktreeGit(t *testing.T) {
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
	fakeGitDir := t.TempDir()
	fakeGit := filepath.Join(fakeGitDir, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeGitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := DiscoverWithKeyContext(ctx, linkedRoot, []byte("test-identity-key"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DiscoverWithKeyContext error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

// gitRunIn runs Git inside dir with deterministic identity and fails the test
// on error. Real Git supplies the metadata-binding fidelity that the synthetic
// fixtures above cannot.
func gitRunIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=AutoGit Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=AutoGit Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

// TM-027: a checkout cannot adopt another local repository's identity and
// policy scope by pointing its `.git` file at that repository's git directory.
func TestDiscoverRejectsGitfileAliasingAnotherRepository(t *testing.T) {
	victim := t.TempDir()
	gitRunIn(t, victim, "init", "--quiet", ".")
	gitRunIn(t, victim, "commit", "--quiet", "--allow-empty", "-m", "init")
	if _, err := Discover(victim); err != nil {
		t.Fatalf("victim repository rejected: %v", err)
	}
	alias := t.TempDir()
	if err := os.WriteFile(filepath.Join(alias, ".git"), []byte("gitdir: "+filepath.Join(victim, ".git")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(alias); err == nil || !strings.Contains(err.Error(), "does not declare this directory as its worktree") {
		t.Fatalf("gitfile alias error = %v, want unbound worktree rejection", err)
	}
	if _, err := Discover(victim); err != nil {
		t.Fatalf("victim repository rejected after alias attempt: %v", err)
	}
}

// Git metadata is trusted only when it records the requesting directory as its
// worktree: core.worktree (as submodules and configured separate git
// directories record) or a linked worktree back-pointer. Metadata with
// neither binding is rejected instead of trusted.
func TestDiscoverRequiresRecordedWorktreeBinding(t *testing.T) {
	base := t.TempDir()
	worktree := filepath.Join(base, "wt")
	gitDir := filepath.Join(base, "separate-git-dir")
	gitRunIn(t, base, "init", "--quiet", "--separate-git-dir", gitDir, worktree)
	if _, err := Discover(worktree); err == nil || !strings.Contains(err.Error(), "does not declare this directory as its worktree") {
		t.Fatalf("separate git directory error = %v, want unbound worktree rejection", err)
	}
	gitRunIn(t, base, "config", "--file", filepath.Join(gitDir, "config"), "core.worktree", worktree)
	if _, err := Discover(worktree); err != nil {
		t.Fatalf("recorded worktree path was rejected: %v", err)
	}
}

func TestDiscoverAcceptsSubmoduleWorktreeGitfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local-path submodule setup is Unix-specific in this suite")
	}
	base := t.TempDir()
	upstream := filepath.Join(base, "upstream")
	gitRunIn(t, base, "init", "--quiet", upstream)
	gitRunIn(t, upstream, "commit", "--quiet", "--allow-empty", "-m", "init")
	parent := filepath.Join(base, "parent")
	gitRunIn(t, base, "clone", "--quiet", upstream, parent)
	gitRunIn(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", "--quiet", upstream, "sub")
	if _, err := Discover(filepath.Join(parent, "sub")); err != nil {
		t.Fatalf("submodule worktree gitfile rejected: %v", err)
	}
}

// A linked worktree moved without repair keeps a stale back-pointer, so it is
// rejected until `git worktree repair` re-records the binding. This is the
// same repair workflow Git documents for manually moved worktrees.
func TestDiscoverRejectsMovedWorktreeUntilRepaired(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("worktree move/repair is exercised on Unix hosts in this suite")
	}
	base := t.TempDir()
	main := filepath.Join(base, "main")
	gitRunIn(t, base, "init", "--quiet", main)
	gitRunIn(t, main, "commit", "--quiet", "--allow-empty", "-m", "init")
	original := filepath.Join(base, "linked")
	gitRunIn(t, main, "worktree", "add", "--quiet", "-b", "linked", original)
	moved := filepath.Join(base, "moved")
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(moved); err == nil || !strings.Contains(err.Error(), "git worktree repair") {
		t.Fatalf("moved worktree error = %v, want stale back-pointer rejection", err)
	}
	gitRunIn(t, main, "worktree", "repair", moved)
	if _, err := Discover(moved); err != nil {
		t.Fatalf("repaired worktree rejected: %v", err)
	}
}
