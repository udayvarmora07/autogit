package gittransaction

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSnapshotAtCommitReadsExactTreeFilesAndModes(t *testing.T) {
	repo := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gitPath, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Snapshot\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "run.sh"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gitPath, "-C", repo, "add", "--", "README.md", "run.sh").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command(gitPath, "-C", repo, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: snapshot").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	sha := strings.TrimSpace(string(mustGitCommand(t, gitPath, repo, "rev-parse", "HEAD")))
	files, err := SnapshotAtCommit(context.Background(), SystemRunner{}, repo, sha, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	wantExecutableMode := os.FileMode(0755)
	if runtime.GOOS == "windows" {
		wantExecutableMode = 0644
	}
	if len(files) != 2 || files[0].Path != "README.md" || string(files[0].Content) != "# Snapshot\n" || files[1].Path != "run.sh" || files[1].Mode.Perm() != wantExecutableMode.Perm() {
		t.Fatalf("snapshot=%+v", files)
	}
}

func mustGitCommand(t *testing.T, executable, repo string, args ...string) []byte {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command(executable, commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
	return output
}
