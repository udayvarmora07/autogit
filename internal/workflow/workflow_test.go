package workflow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/gittransaction"
	"autogit/internal/policy"
	"autogit/internal/security"
	"autogit/internal/staging"
	"autogit/internal/state"
	"autogit/internal/verification"
)

func TestRunCreatesVerifiedOwnedCommitWithoutChangingSharedState(t *testing.T) {
	repo := newRepository(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	write(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	write(t, filepath.Join(repo, "unrelated.txt"), "user work\n")
	git(t, repo, "add", "--", "unrelated.txt")

	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := read(t, filepath.Join(repo, ".git", "index"))
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lease := &recordingWorkflowLease{}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "test", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := (Service{
		Git:            gittransaction.SystemRunner{},
		Intents:        gittransaction.NewStateIntentPort(db),
		VerifierRunner: verifierRunner{},
		Lease:          lease,
	}).Run(context.Background(), Request{
		ID:            "owned-commit",
		RepositoryDir: repo,
		Snapshot: []gittransaction.SnapshotEntry{{
			Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644,
		}},
		Message:   "feat: commit the verified candidate",
		Policy:    policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"},
		Verifiers: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit.SHA == "" || !got.Verification.Passed {
		t.Fatalf("result=%+v", got)
	}
	if content := git(t, repo, "show", got.Commit.SHA+":owned.txt"); content != "candidate" {
		t.Fatalf("owned content=%q", content)
	}
	if head := git(t, repo, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("HEAD changed from %s to %s", headBefore, head)
	}
	if index := read(t, filepath.Join(repo, ".git", "index")); string(index) != string(indexBefore) {
		t.Fatal("shared index changed")
	}
	if lease.acquired == "" || lease.released != lease.acquired || lease.owner != "owned-commit" {
		t.Fatalf("lease=%+v", lease)
	}
}

type recordingWorkflowLease struct {
	acquired, released, owner string
}

func (l *recordingWorkflowLease) Acquire(_ context.Context, key, owner string) error {
	l.acquired, l.owner = key, owner
	return nil
}

func (l *recordingWorkflowLease) Release(_ context.Context, key, owner string) error {
	if owner != l.owner {
		return errors.New("workflow lease owner changed")
	}
	l.released = key
	return nil
}

func TestRunBlocksSecretBeforePreparingGitCandidate(t *testing.T) {
	repo := newRepository(t)
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = (Service{
		Git:     gittransaction.SystemRunner{},
		Intents: gittransaction.NewStateIntentPort(db),
	}).Run(context.Background(), Request{
		ID:            "secret-candidate",
		RepositoryDir: repo,
		Snapshot:      []gittransaction.SnapshotEntry{{Path: ".env", Content: []byte("API_KEY=not-a-real-token"), Mode: 0644}},
		Message:       "feat: add configuration",
		Policy:        policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"},
	})
	if err == nil || !strings.Contains(err.Error(), "security scan blocked") {
		t.Fatalf("error=%v", err)
	}
	if _, err := db.GitCommitIntent(context.Background(), "secret-candidate"); !os.IsNotExist(err) {
		t.Fatalf("blocked candidate persisted an intent: %v", err)
	}
	if ref := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/autogit/commits/secret-candidate").Run(); ref == nil {
		t.Fatal("blocked candidate created an AutoGit ref")
	}
}

func TestRunWithVerifierConfigRejectsMissingTrustedConfigurationBeforeGit(t *testing.T) {
	repo := newRepository(t)
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = (Service{
		Git:                gittransaction.SystemRunner{},
		Intents:            gittransaction.NewStateIntentPort(db),
		VerifierRunner:     verification.ExecRunner{},
		TrustedVerifierDir: t.TempDir(),
	}).RunWithVerifierConfig(context.Background(), Request{
		ID:            "missing-verifier-config",
		RepositoryDir: repo,
		Snapshot:      []gittransaction.SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
		Message:       "feat: verify configured candidate",
		Policy:        policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"},
	}, filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "verifier configuration") {
		t.Fatalf("missing trusted config error=%v", err)
	}
	if _, err := db.GitCommitIntent(context.Background(), "missing-verifier-config"); !os.IsNotExist(err) {
		t.Fatalf("missing config persisted a Git intent: %v", err)
	}
}

func TestRunUsesSnapshotCapturedBeforeScannerMutation(t *testing.T) {
	repo := newRepository(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	write(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "test", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		ID:            "captured-before-scan",
		RepositoryDir: repo,
		Snapshot:      []gittransaction.SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
		Message:       "feat: preserve captured candidate",
		Policy:        policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"},
		Verifiers:     registry,
	}
	service := Service{
		Git:            gittransaction.SystemRunner{},
		Intents:        gittransaction.NewStateIntentPort(db),
		VerifierRunner: verifierRunner{},
		Scanner: mutatingScanner{mutate: func() {
			req.Snapshot[0].Content = []byte("changed-after-scan\n")
		}},
	}
	got, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if content := git(t, repo, "show", got.Commit.SHA+":owned.txt"); content != "candidate" {
		t.Fatalf("committed content=%q", content)
	}
}

func TestRunPlanCommitsOnlyTheOwnedPlanSnapshot(t *testing.T) {
	repo := newRepository(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	write(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	plan, err := staging.BuildObservedPlan(nil, staging.ObservedSnapshot{
		"owned.txt": {Content: []byte("owned candidate\n"), Mode: 0644},
	}, []string{"owned.txt"})
	if err != nil {
		t.Fatal(err)
	}
	wantOwnership := plan.OwnershipDigest()
	plan.Digest = "tampered public display digest"
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "test", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Service{
		Git:            gittransaction.SystemRunner{},
		Intents:        gittransaction.NewStateIntentPort(db),
		VerifierRunner: verifierRunner{},
	}).RunPlan(context.Background(), Request{
		ID:            "owned-plan",
		RepositoryDir: repo,
		Snapshot:      []gittransaction.SnapshotEntry{{Path: "owned.txt", Content: []byte("unowned input\n"), Mode: 0644}},
		Message:       "feat: commit owned plan",
		Policy:        policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"},
		Verifiers:     registry,
	}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if content := git(t, repo, "show", got.Commit.SHA+":owned.txt"); content != "owned candidate" {
		t.Fatalf("committed content=%q", content)
	}
	if got.OwnershipDigest != wantOwnership {
		t.Fatalf("ownership digest=%q, want %q", got.OwnershipDigest, wantOwnership)
	}
	scanOnly, err := digest(got.Scan)
	if err != nil {
		t.Fatal(err)
	}
	if got.GuardDigest == scanOnly {
		t.Fatal("guard evidence did not bind the ownership digest")
	}
}

func TestVerifyPlanReturnsEvidenceWithoutCreatingCommitIntent(t *testing.T) {
	repo := newRepository(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	write(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	plan, err := staging.BuildObservedPlan(nil, staging.ObservedSnapshot{"owned.txt": {Content: []byte("candidate\n"), Mode: 0644}}, []string{"owned.txt"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "test", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Service{Git: gittransaction.SystemRunner{}, Intents: gittransaction.NewStateIntentPort(db), VerifierRunner: verifierRunner{}}).VerifyPlan(context.Background(), Request{
		ID: "verify-only", RepositoryDir: repo, Message: "feat: verify candidate", Policy: policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, Verifiers: registry,
	}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verification.Passed || result.Commit.SHA != "" || result.OwnershipDigest != plan.OwnershipDigest() {
		t.Fatalf("verify result=%+v", result)
	}
	if _, err := db.GitCommitIntent(context.Background(), "verify-only"); !os.IsNotExist(err) {
		t.Fatalf("verify-only operation persisted Git intent: %v", err)
	}
}

func TestRunPlanRejectsEmptyPlanBeforeScanning(t *testing.T) {
	repo := newRepository(t)
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	scanned := false
	_, err = (Service{
		Git:     gittransaction.SystemRunner{},
		Intents: gittransaction.NewStateIntentPort(db),
		Scanner: mutatingScanner{mutate: func() { scanned = true }},
	}).RunPlan(context.Background(), Request{
		ID:            "empty-plan",
		RepositoryDir: repo,
		Message:       "feat: reject empty plan",
		Policy:        policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"},
	}, staging.Plan{})
	if err == nil || !strings.Contains(err.Error(), "owned plan is empty") {
		t.Fatalf("error=%v", err)
	}
	if scanned {
		t.Fatal("empty plan reached scanner")
	}
}

type mutatingScanner struct{ mutate func() }

func (s mutatingScanner) Scan(_ context.Context, _ security.CandidateSnapshot) security.ScanResult {
	s.mutate()
	return security.ScanResult{}
}

type verifierRunner struct{}

func (verifierRunner) Run(context.Context, string, map[string]string, ...string) (verification.Result, error) {
	return verification.Result{ExitCode: 0}, nil
}

func newRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init")
	return repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func write(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
