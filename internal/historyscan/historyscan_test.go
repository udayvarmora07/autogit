package historyscan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/gittransaction"
	"autogit/internal/security"
)

func TestHistoryScanFindsSecretRemovedFromCandidate(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "config.txt", "GH_TOKEN=old-secret-value")
	commit(t, repo, "secret")
	if err := os.Remove(filepath.Join(repo, "config.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "clean.txt", "safe\n")
	commit(t, repo, "remove")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocked || !hasFinding(got, security.ReasonSecretFilename) && !hasFinding(got, security.ReasonSecretPattern) {
		t.Fatalf("history secret was not blocked: %#v", got)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), "old-secret-value") {
		t.Fatal("secret value leaked in evidence")
	}
	if got.Digest == "" || got.CandidateSHA == "" || got.Scanner == "" {
		t.Fatalf("unbound evidence: %#v", got)
	}
}

func TestHistoryScanDoesNotInspectUnrelatedRef(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "safe.txt", "safe\n")
	commit(t, repo, "main")
	mainSHA := head(t, repo)
	runGit(t, repo, "switch", "-c", "secret-branch")
	writeFile(t, repo, "credentials.env", "GH_TOKEN=unrelated-secret-value")
	commit(t, repo, "unrelated")
	runGit(t, repo, "switch", "main")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: mainSHA, CandidateRef: "refs/heads/main", PolicyDigest: "sha256:" + strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocked {
		t.Fatalf("unrelated ref affected scan: %#v", got)
	}
}

func TestHistoryScanAllowsSharedBlobButScansEachPath(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "one.txt", "same benign content\n")
	writeFile(t, repo, "two.txt", "same benign content\n")
	commit(t, repo, "shared")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("2", 64)})
	if err != nil || got.Blocked {
		t.Fatalf("shared benign blob blocked: %#v err=%v", got, err)
	}
	writeFile(t, repo, ".env", "same benign content\n")
	commit(t, repo, "secret-name")
	got, err = New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("3", 64)})
	if err != nil || !got.Blocked || !hasFinding(got, security.ReasonSecretFilename) {
		t.Fatalf("secret path was not scanned: %#v err=%v", got, err)
	}
}

func TestHistoryScanRequiresCanonicalPolicyDigest(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "safe", "safe")
	commit(t, repo, "one")
	for _, digest := range []string{"", "sha1:" + strings.Repeat("a", 40), "sha256:" + strings.Repeat("A", 64), strings.Repeat("b", 64)} {
		got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: digest})
		if err == nil || !got.Blocked || !hasReason(got, ReasonInvalidRequest) {
			t.Fatalf("policy digest %q accepted: %#v err=%v", digest, got, err)
		}
	}
}

func TestHistoryScanRejectsBinaryAndLFSPointer(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), []byte{'x', 0, 'y'}, 0600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "large.dat", "version https://git-lfs.github.com/spec/v1\noid sha256:"+strings.Repeat("c", 64)+"\nsize 3\n")
	commit(t, repo, "binary")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("d", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocked || !hasFinding(got, securityBinaryReason()) || !hasFinding(got, ReasonLFSPointer) {
		t.Fatalf("binary/LFS not blocked: %#v", got)
	}
}

func TestHistoryScanRejectsSymlinkAndGitlinkEntries(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "target", "safe")
	if err := os.Symlink("target", filepath.Join(repo, "link")); err == nil {
		commit(t, repo, "symlink")
		got, scanErr := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("6", 64)})
		if scanErr != nil || !got.Blocked || !hasFinding(got, ReasonSymlink) {
			t.Fatalf("symlink was accepted: %#v err=%v", got, scanErr)
		}
	}

	child := newRepo(t)
	writeFile(t, child, "child", "safe")
	commit(t, child, "child")
	childSHA := head(t, child)
	runGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+childSHA+",nested")
	runGit(t, repo, "-c", "user.name=History Test", "-c", "user.email=history@example.invalid", "commit", "-qm", "gitlink")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("7", 64)})
	if err != nil || !got.Blocked || !hasFinding(got, ReasonSubmodule) {
		t.Fatalf("gitlink was accepted: %#v err=%v", got, err)
	}
}

func TestHistoryScanRejectsMalformedHistoricalPath(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "bad\nname", "safe")
	commit(t, repo, "malformed path")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("8", 64)})
	if err == nil || !got.Blocked || !hasReason(got, ReasonMalformedPath) {
		t.Fatalf("malformed path was accepted: %#v err=%v", got, err)
	}
}

func TestHistoryScanCancellationAndCanonicalRoot(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "a", "safe")
	commit(t, repo, "one")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := New(nil).Scan(ctx, Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("e", 64)})
	if err == nil || !got.Blocked || !hasReason(got, ReasonCancelled) {
		t.Fatalf("cancellation not fail closed: %#v err=%v", got, err)
	}
	alias := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, alias); err == nil {
		if _, err := New(nil).Scan(context.Background(), Request{RepoRoot: alias, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("f", 64)}); err == nil {
			t.Fatal("non-canonical root accepted")
		}
	}
}

func TestHistoryScanUsesControlledReadOnlyGitCommands(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "safe", "safe")
	commit(t, repo, "one")
	r := &recordRunner{base: gittransaction.SystemRunner{MaxOutput: 1 << 20}}
	_, err := New(r).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("1", 64)})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range r.args {
		for _, arg := range args {
			if strings.Contains(arg, "--all") || strings.Contains(arg, "--mirror") || strings.Contains(arg, "push") {
				t.Fatalf("unsafe command: %#v", args)
			}
		}
	}
	if len(r.env) == 0 || r.env["GIT_CONFIG_NOSYSTEM"] != "1" || r.env["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("uncontrolled Git environment: %#v", r.env)
	}
}

func TestHistoryScanFailsClosedOnLimitsOutputAndObjectSubstitution(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "safe", "0123456789")
	commit(t, repo, "one")
	request := Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("4", 64)}
	limited, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: request.CandidateSHA, PolicyDigest: request.PolicyDigest, Limits: Limits{MaxBlobBytes: 1}})
	if err != nil || !limited.Blocked || !hasReason(limited, ReasonBlobSize) {
		t.Fatalf("blob limit not enforced: %#v err=%v", limited, err)
	}
	counted, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: request.CandidateSHA, PolicyDigest: request.PolicyDigest, Limits: Limits{MaxObjects: 1}})
	if err == nil || !counted.Blocked || !hasReason(counted, ReasonObjectCount) {
		t.Fatalf("object limit not enforced: %#v err=%v", counted, err)
	}
	truncated, err := New(&truncateRunner{base: gittransaction.SystemRunner{MaxOutput: 1 << 20}}).Scan(context.Background(), request)
	if err == nil || !truncated.Blocked || !hasReason(truncated, ReasonOutputTruncated) {
		t.Fatalf("output truncation not enforced: %#v err=%v", truncated, err)
	}
	substituted, err := New(&substituteRunner{base: gittransaction.SystemRunner{MaxOutput: 1 << 20}}).Scan(context.Background(), request)
	if err == nil || !substituted.Blocked || !hasReason(substituted, ReasonObjectSubstitute) {
		t.Fatalf("object substitution not enforced: %#v err=%v", substituted, err)
	}
}

func TestHistoryScanCapsAggregatedFindings(t *testing.T) {
	repo := newRepo(t)
	for _, name := range []string{".env", "credentials", "private-key"} {
		writeFile(t, repo, name, "safe\n")
	}
	commit(t, repo, "many findings")
	got, err := New(nil).Scan(context.Background(), Request{RepoRoot: repo, CandidateSHA: head(t, repo), PolicyDigest: "sha256:" + strings.Repeat("5", 64), Limits: Limits{MaxFindings: 2}})
	if err != nil || !got.Blocked || len(got.Findings) != 2 || !hasReason(got, ReasonFindingLimit) {
		t.Fatalf("finding limit was not enforced: %#v err=%v", got, err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	return dir
}
func writeFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func commit(t *testing.T, repo, msg string) {
	t.Helper()
	runGit(t, repo, "add", "--", ".")
	runGit(t, repo, "-c", "user.name=History Test", "-c", "user.email=history@example.invalid", "commit", "-qm", msg)
}
func head(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
}
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func hasFinding(e Evidence, reason string) bool {
	for _, f := range e.Findings {
		if f.Reason == reason {
			return true
		}
	}
	return false
}
func hasReason(e Evidence, reason string) bool {
	for _, r := range e.ReasonCodes {
		if r == reason {
			return true
		}
	}
	return false
}
func securityBinaryReason() string { return "SEC_BINARY_CONTENT" }

type recordRunner struct {
	base gittransaction.SystemRunner
	args [][]string
	env  map[string]string
}

func (r *recordRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (gittransaction.Result, error) {
	r.args = append(r.args, append([]string(nil), args...))
	r.env = env
	return r.base.Run(ctx, dir, env, args...)
}

type truncateRunner struct{ base gittransaction.SystemRunner }

func (r *truncateRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (gittransaction.Result, error) {
	result, err := r.base.Run(ctx, dir, env, args...)
	if len(args) > 0 && args[0] == "rev-list" {
		result.Truncated = true
	}
	return result, err
}

type substituteRunner struct{ base gittransaction.SystemRunner }

func (r *substituteRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (gittransaction.Result, error) {
	result, err := r.base.Run(ctx, dir, env, args...)
	if len(args) == 3 && args[0] == "cat-file" && args[1] == "blob" {
		result.Output = "substituted"
		result.Err = nil
		return result, nil
	}
	return result, err
}
