package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"autogit/internal/repository"
	"autogit/internal/staging"
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
