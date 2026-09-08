package gittransaction

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectObjectFormatSupportsSHA1AndSHA256Repositories(t *testing.T) {
	tests := []struct {
		name   string
		format ObjectFormat
		length int
	}{
		{name: "sha1", format: ObjectFormatSHA1, length: 40},
		{name: "sha256", format: ObjectFormatSHA256, length: 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			args := []string{"init", "-q"}
			if tt.format == ObjectFormatSHA256 {
				args = append(args, "--object-format=sha256")
			}
			args = append(args, repo)
			if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Skipf("Git does not support %s repositories: %v: %s", tt.format, err, output)
			}
			got, err := DetectObjectFormat(context.Background(), SystemRunner{}, repo)
			if err != nil || got != tt.format || got.ObjectIDLength() != tt.length {
				t.Fatalf("format=%q length=%d err=%v, want %q/%d", got, got.ObjectIDLength(), err, tt.format, tt.length)
			}
			id := strings.Repeat("a", tt.length)
			if !got.ValidObjectID(id) || got.ValidObjectID(strings.Repeat("a", 40+64-tt.length)) {
				t.Fatalf("object-id validation for %s is incorrect", tt.format)
			}
		})
	}
}

func TestCompareTreeToSnapshotRejectsPathModeOrContentDrift(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	path := filepath.Join(repo, "script.sh")
	if err := writeTestFile(path, []byte("echo stable\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repo, "add", "--", "script.sh").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", repo, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "tree comparison").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	sha := strings.TrimSpace(string(mustGitCommand(t, "git", repo, "rev-parse", "HEAD")))
	entries, err := SnapshotAtCommit(context.Background(), SystemRunner{}, repo, sha, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := CompareTreeToSnapshot(context.Background(), SystemRunner{}, repo, sha, entries, 1<<20); err != nil {
		t.Fatal(err)
	}
	entries[0].Content = []byte("changed\n")
	if err := CompareTreeToSnapshot(context.Background(), SystemRunner{}, repo, sha, entries, 1<<20); err == nil {
		t.Fatal("content drift was accepted")
	}
}

func TestSHA256TransactionPreservesSpecialPathsModesAndBytes(t *testing.T) {
	repo := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", "--initial-branch=main", "--object-format=sha256", repo).CombinedOutput(); err != nil {
		t.Skipf("Git does not support SHA-256 repositories: %v: %s", err, output)
	}
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	if err := os.MkdirAll(filepath.Join(repo, "目录"), 0700); err != nil {
		t.Fatal(err)
	}
	writeTestFile := func(name string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile("--option.txt", []byte("option\n"), 0600)
	writeTestFile("目录/é.txt", []byte("unicode\r\n"), 0600)
	writeTestFile("run.sh", []byte("#!/bin/sh\nexit 0\n"), 0700)
	git(t, repo, "add", "--", "--option.txt", "目录/é.txt", "run.sh")
	git(t, repo, "commit", "-m", "baseline")

	format, err := DetectObjectFormat(context.Background(), SystemRunner{}, repo)
	if err != nil || format != ObjectFormatSHA256 {
		t.Fatalf("object format=%q err=%v", format, err)
	}
	entries := []SnapshotEntry{
		{Path: "--option.txt", Content: []byte("candidate\n"), Mode: 0644},
		{Path: "目录/é.txt", Content: []byte("exact\r\nbytes\n"), Mode: 0644},
		{Path: "run.sh", Content: []byte("#!/bin/sh\nexit 7\n"), Mode: 0755},
	}
	runner := testLoggingRunner{t: t}
	prepared, err := New(runner, &memoryIntentStore{}).Prepare(context.Background(), Request{
		ID: "sha256-special", RepoDir: repo, Snapshot: entries, Message: "feat: candidate",
		PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.TreeOID()) != 64 || len(prepared.ParentSHA()) != 64 {
		t.Fatalf("prepared=%+v, want SHA-256 tree and parent", prepared)
	}
	if err := CompareTreeToSnapshot(context.Background(), runner, repo, prepared.TreeOID(), entries, 1<<20); err != nil {
		t.Fatalf("candidate tree differed from immutable snapshot: %v", err)
	}
	created, err := New(runner, &memoryIntentStore{}).Create(context.Background(), Request{
		ID: "sha256-commit", RepoDir: repo, Snapshot: entries, Message: "feat: candidate",
		PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.SHA) != 64 || len(created.TreeOID) != 64 {
		t.Fatalf("created=%+v, want SHA-256 commit and tree", created)
	}
}

type testLoggingRunner struct {
	t *testing.T
}

func (r testLoggingRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	result, err := (SystemRunner{}).Run(ctx, dir, env, args...)
	if err != nil {
		r.t.Logf("git %v output=%q err=%v", args, result.Output, err)
	}
	return result, err
}

func TestSnapshotAndTransactionRejectControlPathsButAcceptOptionLikePaths(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "base.txt"), "base\n")
	git(t, repo, "add", "--", "base.txt")
	git(t, repo, "commit", "-m", "baseline")

	tx := New(SystemRunner{}, &memoryIntentStore{})
	for _, path := range []string{"bad\nname", "control\x01name", "../escape", "nested//empty"} {
		if _, err := tx.Prepare(context.Background(), Request{
			ID: "reject-" + strings.ReplaceAll(path, "/", "-"), RepoDir: repo,
			Snapshot: []SnapshotEntry{{Path: path, Content: []byte("x\n"), Mode: 0644}}, Message: "feat: reject",
			PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
		}); err == nil {
			t.Fatalf("unsafe path %q was accepted", path)
		}
	}
	if _, err := tx.Prepare(context.Background(), Request{
		ID: "accept-option", RepoDir: repo,
		Snapshot: []SnapshotEntry{{Path: "--option.txt", Content: []byte("x\n"), Mode: 0644}}, Message: "feat: option",
		PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	}); err != nil {
		t.Fatalf("option-like path was rejected: %v", err)
	}
}

func writeTestFile(path string, data []byte, mode uint32) error {
	if err := os.WriteFile(path, data, os.FileMode(mode)); err != nil {
		return err
	}
	return nil
}
