package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"autogit/internal/policy"
	"autogit/internal/repository"
	"autogit/internal/staging"
	"autogit/internal/verification"
	"autogit/internal/workflow"
)

type fakeRunner struct{ baseline repository.Baseline }

func (r fakeRunner) Run(context.Context, string, map[string]string, ...string) (repository.CommandResult, error) {
	return repository.CommandResult{}, errors.New("unexpected command")
}

type fakeStore struct {
	sessionID, repositoryID, clientID string
	baseline                          repository.Baseline
}

func (s *fakeStore) RecordSessionBaseline(_ context.Context, sessionID, repositoryID, clientID string, baseline repository.Baseline) error {
	s.sessionID, s.repositoryID, s.clientID, s.baseline = sessionID, repositoryID, clientID, baseline.Clone()
	return nil
}

func TestCaptureAndRecordRequiresCompleteRequest(t *testing.T) {
	service := Service{Runner: fakeRunner{}, Store: &fakeStore{}}
	for name, request := range map[string]Request{
		"session":    {RepositoryID: "repo", ClientID: "client", Root: t.TempDir()},
		"repository": {SessionID: "session", ClientID: "client", Root: t.TempDir()},
		"client":     {SessionID: "session", RepositoryID: "repo", Root: t.TempDir()},
		"root":       {SessionID: "session", RepositoryID: "repo", ClientID: "client"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CaptureAndRecord(context.Background(), request); err == nil {
				t.Fatal("incomplete request accepted")
			}
		})
	}
}

func TestCaptureAndRecordPersistsOnlyCapturedBaseline(t *testing.T) {
	root := t.TempDir()
	store := &fakeStore{}
	want := repository.Baseline{Head: "0123456789012345678901234567890123456789"}
	service := Service{Capture: func(context.Context, repository.Runner, string) (repository.Baseline, error) { return want, nil }, Runner: fakeRunner{}, Store: store}

	got, err := service.CaptureAndRecord(context.Background(), Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: filepath.Join(root, ".")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || store.sessionID != "session" || store.repositoryID != "repo" || store.clientID != "codex" || store.baseline.Head != want.Head {
		t.Fatalf("got=%+v store=%+v", got, store)
	}
}

func TestCaptureAndRecordDoesNotPersistWhenCaptureFails(t *testing.T) {
	store := &fakeStore{}
	service := Service{Capture: func(context.Context, repository.Runner, string) (repository.Baseline, error) {
		return repository.Baseline{}, errors.New("capture failed")
	}, Runner: fakeRunner{}, Store: store}
	if _, err := service.CaptureAndRecord(context.Background(), Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: t.TempDir()}); err == nil {
		t.Fatal("capture failure hidden")
	}
	if store.sessionID != "" {
		t.Fatal("baseline persisted after capture failure")
	}
}

func TestNewServiceUsesProductionObservationRunner(t *testing.T) {
	service := New(&fakeStore{})
	if _, ok := service.Runner.(repository.SystemRunner); !ok {
		t.Fatalf("runner=%T, want repository.SystemRunner", service.Runner)
	}
}

func TestCaptureAndRecordCapturesExplicitOwnedPathsWithDefaultCapture(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	service := New(store)
	got, err := service.CaptureAndRecord(context.Background(), Request{SessionID: "s", RepositoryID: "r", ClientID: "codex", Root: root, Paths: []string{"owned.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if file, ok := got.Files["owned.txt"]; !ok || string(file.Content) != "baseline\n" {
		t.Fatalf("baseline=%+v", got)
	}
}

func TestBuildOwnedPlanBridgesDurableBaselineToCurrentOwnedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(&fakeStore{})
	plan, err := service.BuildOwnedPlan(Request{SessionID: "s", RepositoryID: "r", ClientID: "codex", Root: root, Paths: []string{"new.txt"}}, repository.Baseline{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CandidateSnapshot()) != 1 || plan.CandidateSnapshot()[0].Path != "new.txt" || string(plan.CandidateSnapshot()[0].Content) != "candidate\n" {
		t.Fatalf("plan=%+v candidate=%+v", plan, plan.CandidateSnapshot())
	}
	var _ staging.Plan = plan
}

type fakeWorkflow struct {
	called  bool
	request workflow.Request
	plan    staging.Plan
}

func (w *fakeWorkflow) RunPlan(_ context.Context, request workflow.Request, plan staging.Plan) (workflow.Result, error) {
	w.called = true
	w.request = request
	w.plan = plan
	return workflow.Result{OwnershipDigest: plan.OwnershipDigest()}, nil
}

func TestCoordinatorCompletesOnlyChangesMadeAfterSessionBaseline(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(&fakeStore{})
	request := Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: root, Paths: []string{"existing.txt", "new.txt"}}
	started, err := service.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := &fakeWorkflow{}
	got, err := service.Complete(context.Background(), started, w, "commit-1", "feat: capture new file", policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, &verification.VerifierRegistry{})
	if err != nil {
		t.Fatal(err)
	}
	entries := w.plan.CandidateSnapshot()
	if !w.called || len(entries) != 1 || entries[0].Path != "new.txt" || string(entries[0].Content) != "after\n" || got.OwnershipDigest == "" {
		t.Fatalf("called=%v entries=%+v result=%+v", w.called, entries, got)
	}
	if w.request.ID != "commit-1" || w.request.RepositoryDir != root || w.request.Message != "feat: capture new file" || w.request.Verifiers == nil {
		t.Fatalf("workflow request=%+v", w.request)
	}
}

func TestCoordinatorBlocksBaselineOwnershipChangesBeforeWorkflow(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(&fakeStore{})
	started, err := service.Start(context.Background(), Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: root, Paths: []string{"existing.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := &fakeWorkflow{}
	if _, err := service.Complete(context.Background(), started, w, "commit-1", "feat: changed file", policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, &verification.VerifierRegistry{}); err == nil {
		t.Fatal("baseline ownership change was accepted")
	}
	if w.called {
		t.Fatal("workflow ran for ambiguous ownership")
	}
}

func TestCoordinatorDoesNotInvokeWorkflowForAnEmptyOwnedPlan(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	service := New(&fakeStore{})
	started, err := service.Start(context.Background(), Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: root, Paths: []string{"new.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	w := &fakeWorkflow{}
	if _, err := service.Complete(context.Background(), started, w, "commit-1", "feat: no changes", policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, &verification.VerifierRegistry{}); err == nil {
		t.Fatal("empty owned plan was accepted")
	}
	if w.called {
		t.Fatal("workflow ran for an empty owned plan")
	}
}

func TestCoordinatorRejectsSharedIndexChangesAfterBaseline(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	service := New(&fakeStore{})
	request := Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: root, Paths: []string{"new.txt"}}
	started, err := service.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", root, "add", "--", "new.txt").Run(); err != nil {
		t.Fatal(err)
	}
	w := &fakeWorkflow{}
	if _, err := service.Complete(context.Background(), started, w, "commit-1", "feat: add new file", policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, &verification.VerifierRegistry{}); err == nil {
		t.Fatal("shared index change was accepted")
	}
	if w.called {
		t.Fatal("workflow ran after shared index changed")
	}
}

func TestCoordinatorRejectsHEADChangesAfterBaseline(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	service := New(&fakeStore{})
	request := Request{SessionID: "session", RepositoryID: "repo", ClientID: "codex", Root: root, Paths: []string{"new.txt"}}
	started, err := service.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "baseline.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "baseline.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "chore: concurrent head")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := &fakeWorkflow{}
	if _, err := service.Complete(context.Background(), started, w, "commit-1", "feat: add new file", policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, &verification.VerifierRegistry{}); err == nil {
		t.Fatal("HEAD change was accepted")
	}
	if w.called {
		t.Fatal("workflow ran after HEAD changed")
	}
}
