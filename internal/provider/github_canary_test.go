//go:build github_canary

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"autogit/internal/repository"
	"autogit/internal/state"
)

var canaryIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var canaryNamePattern = regexp.MustCompile(`^autogit-v1-test-[0-9]+$`)

func TestGitHubCanary(t *testing.T) {
	if os.Getenv("AUTOGIT_GITHUB_CANARY") != "1" {
		t.Skip("GitHub canary is opt-in")
	}
	owner := os.Getenv("AUTOGIT_CANARY_OWNER")
	name := os.Getenv("AUTOGIT_CANARY_NAME")
	visibility := os.Getenv("AUTOGIT_CANARY_VISIBILITY")
	if visibility == "" {
		visibility = "private"
	}
	if !canaryIdentityPattern.MatchString(owner) || !canaryNamePattern.MatchString(name) {
		t.Fatal("canary owner or repository name is outside the allowlist")
	}
	if visibility != "private" && visibility != "public" {
		t.Fatalf("unsupported canary visibility %q", visibility)
	}
	if visibility == "public" && os.Getenv("AUTOGIT_CANARY_PUBLIC_CONSENT") != "1" {
		t.Fatal("public canary requires explicit public consent")
	}
	token := os.Getenv("AUTOGIT_CANARY_TOKEN")
	if token == "" {
		t.Fatal("AUTOGIT_CANARY_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ghPath := trustedCanaryExecutable(t, "AUTOGIT_GH_PATH", "gh")
	gitPath := trustedCanaryExecutable(t, "AUTOGIT_GIT_PATH", "git")
	root := t.TempDir()
	gh := canaryGHRunner{Executable: ghPath, WorkingDir: root, Token: token}
	git := SystemRunner{Executable: gitPath, WorkingDir: root}

	canaryGit(t, ctx, git, root, "init", "--initial-branch", "main")
	canaryGit(t, ctx, git, root, "config", "user.email", "autogit-canary@example.invalid")
	canaryGit(t, ctx, git, root, "config", "user.name", "AutoGit Canary")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# AutoGit canary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canaryGit(t, ctx, git, root, "add", "--", "README.md")
	canaryGit(t, ctx, git, root, "commit", "-m", "test(canary): verify provider contract")
	sha := strings.TrimSpace(canaryGit(t, ctx, git, root, "rev-parse", "HEAD"))
	if !shaRE.MatchString(sha) {
		t.Fatalf("local commit did not produce a canonical SHA")
	}

	info, err := repository.Discover(root)
	if err != nil {
		t.Fatalf("discover canary repository: %v", err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open canary state: %v", err)
	}
	defer store.Close()
	hosted := GH{Runner: gh, VerifyOwner: true}
	identity, err := (RepositoryTransaction{
		State:  store,
		Hosted: hosted,
		Git:    GitPusher{Runner: git, Dir: root},
		Owner:  "github-canary",
	}).Create(ctx, RemoteCreateRequest{
		ID: "canary-" + name, RepositoryID: info.RepoID, Alias: "origin",
		Owner: owner, Name: name, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("create and attach canary repository: %v", err)
	}
	if identity != owner+"/"+name {
		t.Fatalf("created identity=%q, want %q", identity, owner+"/"+name)
	}
	publication := GH{Runner: gh, Pusher: GitPusher{Runner: git, Dir: root, AllowedRemotes: map[string]string{identity: "origin"}}}
	if err := publication.ConfirmRepository(ctx, RemoteRequest{Owner: owner, Name: name, Visibility: visibility}); err != nil {
		t.Fatalf("confirm exact repository postcondition: %v", err)
	}
	if err := publication.Pusher.Push(ctx, identity, sha, "main"); err != nil {
		t.Fatalf("push exact canary ref: %v", err)
	}
	outcome, err := publication.ConfirmPush(ctx, PushRequest{Owner: owner, Name: name, Ref: "main", SHA: sha})
	if err != nil {
		t.Fatalf("confirm exact ref/SHA postcondition: %v", err)
	}
	if outcome != PushPresent {
		t.Fatalf("canary ref outcome=%q, want %q", outcome, PushPresent)
	}
}

func TestCanaryGHRunnerPassesOnlyExplicitToken(t *testing.T) {
	script := filepath.Join(t.TempDir(), "gh-fixture.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$GH_TOKEN\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := canaryGHRunner{Executable: script, WorkingDir: t.TempDir(), Token: "canary-token"}
	result, err := runner.Run(context.Background(), runner.WorkingDir, "api", "user")
	if err != nil || result.Err != nil {
		t.Fatalf("canary runner err=%v result=%v", err, result.Err)
	}
	if result.Output != "canary-token" {
		t.Fatalf("runner output=%q, want explicit token", result.Output)
	}
}

type canaryGHRunner struct {
	Executable string
	WorkingDir string
	Token      string
}

func (r canaryGHRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if dir == "" {
		dir = r.WorkingDir
	}
	cmd := exec.CommandContext(ctx, r.Executable, args...)
	cmd.Dir = dir
	cmd.Env = []string{"GH_TOKEN=" + r.Token, "GH_PROMPT_DISABLED=1"}
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "SYSTEMROOT", "LANG"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	output, err := cmd.CombinedOutput()
	if len(output) > maxOutput {
		return Result{Output: string(output[:maxOutput]), Err: ErrOutputLimit}, ErrOutputLimit
	}
	return Result{Output: string(output), Err: err}, err
}

func trustedCanaryExecutable(t *testing.T, envName, command string) string {
	t.Helper()
	path := os.Getenv(envName)
	if path == "" {
		var err error
		path, err = exec.LookPath(command)
		if err != nil {
			t.Fatalf("%s is unavailable: %v", command, err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("resolve %s: %v", command, err)
	}
	return filepath.Clean(resolved)
}

func canaryGit(t *testing.T, ctx context.Context, runner SystemRunner, root string, args ...string) string {
	t.Helper()
	result, err := runner.Run(ctx, root, args...)
	if err != nil || result.Err != nil {
		cause := err
		if cause == nil {
			cause = result.Err
		}
		if cause == nil {
			cause = errors.New("git command failed")
		}
		t.Fatalf("git %s: %v", strings.Join(args, " "), fmt.Errorf("%w", cause))
	}
	return result.Output
}
