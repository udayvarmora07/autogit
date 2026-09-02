package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/events"
	"autogit/internal/policy"
	"autogit/internal/repository"
	"autogit/internal/session"
	"autogit/internal/staging"
	"autogit/internal/verification"
	"autogit/internal/workflow"
)

type countingProvider struct{ calls int }

type appBaselineStore struct{ baseline repository.Baseline }

func (s *appBaselineStore) RecordSessionBaseline(_ context.Context, _, _, _ string, baseline repository.Baseline) error {
	s.baseline = baseline
	return nil
}

type appBaselineRunner struct{}

type appSessionWorkflow struct {
	called  bool
	request workflow.Request
	plan    staging.Plan
}

func (w *appSessionWorkflow) RunPlan(_ context.Context, request workflow.Request, plan staging.Plan) (workflow.Result, error) {
	w.called = true
	w.request = request
	w.plan = plan
	return workflow.Result{OwnershipDigest: plan.OwnershipDigest()}, nil
}

func (appBaselineRunner) Run(context.Context, string, map[string]string, ...string) (repository.CommandResult, error) {
	return repository.CommandResult{}, nil
}

func (p *countingProvider) Touch() { p.calls++ }
func (p *countingProvider) EnsureRemote(context.Context, string, string, string) (string, error) {
	p.calls++
	return "", fmt.Errorf("must not be called")
}
func (p *countingProvider) Push(context.Context, string, string, string) error {
	p.calls++
	return fmt.Errorf("must not be called")
}

func TestHookRequiresConsentBeforeSchedulingAction(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{}, nil)
	r, err := a.Hook(context.Background(), []byte(`{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"01J7N6X8P5K2V4W6FQ8M9ABCDF","event_type":"session.idle","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session"},"ordering":{"stream_id":"stream"},"idempotency":{"key":"k"},"payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Action != "ask_consent" || r.ReasonCode != "CONSENT_REQUIRED" {
		t.Fatalf("result=%+v", r)
	}
}

func TestApplicationExposesSessionBaselineCaptureBoundary(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store := &appBaselineStore{}
	want := repository.Baseline{Head: "0123456789012345678901234567890123456789"}
	service := &session.Service{Runner: appBaselineRunner{}, Store: store, Capture: func(context.Context, repository.Runner, string) (repository.Baseline, error) { return want, nil }}
	a := New(s, policy.Policy{}, nil)
	a.Baselines = service
	got, err := a.CaptureSessionBaseline(context.Background(), session.Request{SessionID: "s", RepositoryID: "r", ClientID: "codex", Root: t.TempDir()})
	if err != nil || got.Head != want.Head || store.baseline.Head != want.Head {
		t.Fatalf("got=%+v stored=%+v err=%v", got, store.baseline, err)
	}
}

func TestSessionStartedIngressCapturesBaselineBeforeReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	info, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	baselineStore := &appBaselineStore{}
	baseline := repository.Baseline{Head: "0123456789012345678901234567890123456789", IndexDigest: "sha256:" + strings.Repeat("1", 64), StatusDigest: "sha256:" + strings.Repeat("2", 64), PathsDigest: "sha256:" + strings.Repeat("3", 64)}
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	a.Resolver = func(string) (repository.Info, error) { return repository.Discover(root) }
	a.Baselines = &session.Service{Runner: appBaselineRunner{}, Store: baselineStore, Capture: func(context.Context, repository.Runner, string) (repository.Baseline, error) { return baseline, nil }}
	input := hookEvent("01J7N6X8P5K2V4W6NQ8M9ABCDF", "session.started", "started", `,"session_id":"session"`, "", "")
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		t.Fatal(err)
	}
	raw["scope"].(map[string]any)["repo_id"] = info.RepoID
	raw["scope"].(map[string]any)["worktree_id"] = info.WorktreeID
	raw["project"] = map[string]any{"candidate_root": root}
	inputBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Hook(context.Background(), inputBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != "accepted" || baselineStore.baseline.StatusDigest != baseline.StatusDigest {
		t.Fatalf("result=%+v baseline-store=%+v", got, baselineStore)
	}
	if _, _, err := s.LifecycleProjection(info.RepoID); err != nil {
		t.Fatalf("session.started receipt was not accepted: %v", err)
	}
}

func TestSessionStartedIngressDoesNotAcceptWhenBaselineCaptureFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	info, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	a.Resolver = func(string) (repository.Info, error) { return repository.Discover(root) }
	a.Baselines = &session.Service{Runner: appBaselineRunner{}, Store: &appBaselineStore{}, Capture: func(context.Context, repository.Runner, string) (repository.Baseline, error) {
		return repository.Baseline{}, errors.New("baseline unavailable")
	}}
	input := hookEvent("01J7N6X8P5K2V4W6NQ8M9ABCDF", "session.started", "started", `,"session_id":"session"`, "", "")
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		t.Fatal(err)
	}
	raw["scope"].(map[string]any)["repo_id"] = info.RepoID
	raw["scope"].(map[string]any)["worktree_id"] = info.WorktreeID
	raw["project"] = map[string]any{"candidate_root": root}
	inputBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Hook(context.Background(), inputBytes); err == nil {
		t.Fatal("baseline failure was accepted")
	}
	if _, _, err := s.LifecycleProjection(info.RepoID); err == nil {
		t.Fatal("baseline failure created a lifecycle receipt")
	}
}

func TestApplicationExposesSessionCompletionThroughVerifiedWorkflowBoundary(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	baselineStore := &appBaselineStore{}
	service := &session.Service{Runner: repository.SystemRunner{}, Store: baselineStore}
	a := New(nil, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	a.Baselines = service
	w := &appSessionWorkflow{}
	a.SessionWorkflow = w
	started, err := service.Start(context.Background(), session.Request{SessionID: "s", RepositoryID: "repo", ClientID: "codex", Root: root, Paths: []string{"new.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := a.CompleteSession(context.Background(), started, "commit-1", "feat: complete session", policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}, &verification.VerifierRegistry{})
	if err != nil {
		t.Fatal(err)
	}
	if !w.called || got.OwnershipDigest == "" || len(w.plan.CandidateSnapshot()) != 1 || w.request.ID != "commit-1" {
		t.Fatalf("called=%v result=%+v plan=%+v request=%+v", w.called, got, w.plan.CandidateSnapshot(), w.request)
	}
}

func TestHookRejectsForgedDomainFactBeforeReceiptOrProjection(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local"}, nil)
	input := `{"schema_version":"autogit.event/1","event_class":"domain","event_id":"01J7N6X8P5K2V4W6FQ8M9ABCDG","event_type":"commit.requested","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"core","instance_id":"core"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","worktree_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session","task_id":"task","change_id":"change"},"ordering":{"stream_id":"stream","correlation_id":"correlation"},"idempotency":{"key":"commit"},"payload":{"commit_job_id":"job","candidate_digest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}`
	if _, err := a.Hook(context.Background(), []byte(input)); err == nil || events.CodeOf(err) != "E_EVENT_CLASS" {
		t.Fatalf("forged domain error=%v", err)
	}
	if _, _, err := s.LifecycleProjection("sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("forged domain event created projection")
	}
}

func TestHookLocalOnlyNeverTouchesProvider(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := &countingProvider{}
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, p)
	r, err := a.Hook(context.Background(), []byte(`{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"01J7N6X8P5K2V4W6FQ8M9ABCDF","event_type":"session.idle","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session"},"ordering":{"stream_id":"stream"},"idempotency":{"key":"k"},"payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Action != "none" || r.ReasonCode != "LOCAL_ONLY" || p.calls != 0 {
		t.Fatalf("result=%+v provider calls=%d", r, p.calls)
	}
}

func TestResultIsStableJSONContract(t *testing.T) {
	b, err := json.Marshal(Result{Disposition: "accepted", Action: "none", ReasonCode: "LOCAL_ONLY"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{\"schema_version\":\"autogit.result/1\",\"disposition\":\"accepted\",\"action\":\"none\",\"reason_code\":\"LOCAL_ONLY\"}" {
		t.Fatalf("json=%s", b)
	}
}

func TestHookRejectsProjectIdentityMismatchBeforeReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	info, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	base := `{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"01J7N6X8P5K2V4W6FQ8M9ABCDF","event_type":"session.idle","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session"},"ordering":{"stream_id":"stream"},"idempotency":{"key":"k"},"payload":{}}`
	var evt map[string]any
	if err := json.Unmarshal([]byte(base), &evt); err != nil {
		t.Fatal(err)
	}
	scope := evt["scope"].(map[string]any)
	scope["repo_id"] = info.RepoID
	evt["project"] = map[string]any{"candidate_root": root}
	input, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	// A resolver deliberately returns a different identity, proving the event
	// is rejected before its receipt can be accepted.
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{}, nil)
	a.Resolver = func(string) (repository.Info, error) { return repository.Info{RepoID: "other"}, nil }
	if _, decodeErr := events.Decode(input, 64<<10); decodeErr != nil {
		t.Fatalf("input=%s decode=%v", input, decodeErr)
	}
	if _, err = a.Hook(context.Background(), input); err == nil || events.CodeOf(err) != "E_SCOPE" {
		t.Fatalf("error=%v", err)
	}
}

func TestHookRejectsProjectWorktreeIdentityMismatchBeforeReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	info, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{}, nil)
	a.Resolver = func(string) (repository.Info, error) { return repository.Discover(root) }
	input := hookEvent("01J7N6X8P5K2V4W6NQ8M9ABCDH", "session.idle", "idle", `,"session_id":"session"`, "", "")
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		t.Fatal(err)
	}
	raw["scope"].(map[string]any)["repo_id"] = info.RepoID
	raw["scope"].(map[string]any)["worktree_id"] = "sha256:" + strings.Repeat("f", 64)
	raw["project"] = map[string]any{"candidate_root": root}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Hook(context.Background(), b); err == nil || events.CodeOf(err) != "E_SCOPE" {
		t.Fatalf("worktree mismatch error=%v", err)
	}
}

func TestHookPersistsLifecycleProjectionAcrossRestartAndUsesReducerResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := events.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	started := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDG", "session.started", "started", `,"session_id":"session"`, `,"task_id":"task"`, "")
	got, err := a.Hook(context.Background(), []byte(started))
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != "accepted" || got.StateRevision != 1 || got.Action != "none" {
		t.Fatalf("started result=%+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = events.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a = New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	idle := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDH", "session.idle", "idle", `,"session_id":"session"`, "", "")
	got, err = a.Hook(context.Background(), []byte(idle))
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != "accepted" || got.StateRevision != 2 || got.Action != "none" || got.ReasonCode != "LOCAL_ONLY" {
		t.Fatalf("idle result=%+v", got)
	}
}

func TestHookDuplicateDoesNotAdvancePersistedLifecycleRevision(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	input := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDG", "session.started", "started", `,"session_id":"session"`, "", "")
	first, err := a.Hook(context.Background(), []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	dup, err := a.Hook(context.Background(), []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if first.StateRevision != 1 || dup.Disposition != "duplicate" || dup.StateRevision != 1 || dup.Action != "none" {
		t.Fatalf("first=%+v duplicate=%+v", first, dup)
	}
}

func TestHookCausalPendingReplaysIntoProjectionOnce(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	pending := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDH", "session.idle", "idle", `,"session_id":"session"`, "", `,"causation_id":"01J7N6X8P5K2V4W6FQ8M9ABCDG"`)
	got, err := a.Hook(context.Background(), []byte(pending))
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != "pending" || got.StateRevision != 0 {
		t.Fatalf("pending result=%+v", got)
	}
	predecessor := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDG", "session.started", "started", `,"session_id":"session"`, "", "")
	got, err = a.Hook(context.Background(), []byte(predecessor))
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != "accepted" || got.StateRevision != 2 {
		t.Fatalf("replay result=%+v", got)
	}
	dup, err := a.Hook(context.Background(), []byte(pending))
	if err != nil {
		t.Fatal(err)
	}
	if dup.Disposition != "duplicate" || dup.StateRevision != 2 {
		t.Fatalf("replayed duplicate=%+v", dup)
	}
}

func TestHookNeverTurnsWeakModelStopIntoCommitOrPush(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local"}, nil)
	input := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDG", "model.stopped", "stop", `,"session_id":"session"`, "", "")
	got, err := a.Hook(context.Background(), []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got.Action == "commit" || got.Action == "push" {
		t.Fatalf("weak stop scheduled side effect: %+v", got)
	}
}

func TestHookProjectionRedactsPromptAnswerAndDoesNotStoreRawPayload(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	input := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDG", "prompt.submitted", "prompt", `,"session_id":"session","task_id":"task"`, "", "")
	input = strings.Replace(input, `"payload":{}`, `"payload":{"prompt_id":"prompt-id","prompt_kind":"unknown","answer":"yes"}`, 1)
	if _, err := a.Hook(context.Background(), []byte(input)); err != nil {
		t.Fatal(err)
	}
	data, _, err := s.LifecycleProjection("sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"Answer":"yes"`) {
		t.Fatalf("projection retained raw prompt answer: %s", data)
	}
}

func TestHookProjectionRedactsQuarantinedIngressPayload(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	input := hookEvent("01J7N6X8P5K2V4W6FQ8M9ABCDH", "task.updated", "answer", `,"session_id":"session","task_id":"task"`, "", "")
	input = strings.Replace(input, `"payload":{}`, `"payload":{"prompt_id":"missing-prompt","prompt_kind":"unknown","answer":"yes","reason":"source-path-secret-sentinel","extensions":{"raw":"extension-secret-sentinel"}}`, 1)
	if _, err := a.Hook(context.Background(), []byte(input)); err != nil {
		t.Fatal(err)
	}
	data, _, err := s.LifecycleProjection("sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{"source-path-secret-sentinel", "extension-secret-sentinel", `"Answer":"yes"`} {
		if strings.Contains(string(data), sentinel) {
			t.Fatalf("projection retained %q: %s", sentinel, data)
		}
	}
}

func TestHookConcurrentCallsSerializeLifecycleRevision(t *testing.T) {
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{Tracking: "local", LocalOnly: true}, nil)
	const calls = 8
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		id := fmt.Sprintf("01J7N6X8P5K2V4W6FQ8M9ABC%02d", i)
		key := fmt.Sprintf("idle-%d", i)
		go func(input string) {
			_, callErr := a.Hook(context.Background(), []byte(input))
			errs <- callErr
		}(hookEvent(id, "session.idle", key, `,"session_id":"session"`, "", ""))
	}
	for i := 0; i < calls; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	data, revision, err := s.LifecycleProjection("sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if revision != calls || !strings.Contains(string(data), `"Revision":8`) {
		t.Fatalf("projection revision=%d data=%s", revision, data)
	}
}

func hookEvent(id, typ, key, scopeExtra, taskExtra, orderingExtra string) string {
	return `{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"` + id + `","event_type":"` + typ + `","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"` + scopeExtra + taskExtra + `},"ordering":{"stream_id":"stream"` + orderingExtra + `},"idempotency":{"key":"` + key + `"},"payload":{}}`
}
