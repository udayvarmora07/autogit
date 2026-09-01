package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/events"
	"autogit/internal/policy"
	"autogit/internal/repository"
)

type countingProvider struct{ calls int }

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
	input := strings.Replace(base, `"payload":{}`, `"payload":{},"project":{"candidate_root":"`+root+`"}`, 1)
	input = strings.Replace(input, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", info.RepoID, 1)
	// A resolver deliberately returns a different identity, proving the event
	// is rejected before its receipt can be accepted.
	s, err := events.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, policy.Policy{}, nil)
	a.Resolver = func(string) (repository.Info, error) { return repository.Info{RepoID: "other"}, nil }
	if _, decodeErr := events.Decode([]byte(input), 64<<10); decodeErr != nil {
		t.Fatalf("input=%s decode=%v", input, decodeErr)
	}
	if _, err = a.Hook(context.Background(), []byte(input)); err == nil || events.CodeOf(err) != "E_SCOPE" {
		t.Fatalf("error=%v", err)
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
