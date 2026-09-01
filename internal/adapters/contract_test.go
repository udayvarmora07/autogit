package adapters

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"autogit/internal/events"
)

func TestAllAdaptersTranslateACompletedClientClaim(t *testing.T) {
	for _, name := range SupportedNames() {
		t.Run(name, func(t *testing.T) {
			a, err := New(name)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			raw, _ := json.Marshal(map[string]any{"event": "complete", "session_id": "s-1", "task_id": "t-1", "cwd": root, "status": "success", "changed_files": []string{"internal/app.go"}, "operation_id": "op-1"})
			e, err := a.Translate(raw, TranslateOptions{ApprovedRoots: []string{root}, ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, InstallationID: "install-1", InstanceID: "instance-1"})
			if err != nil {
				t.Fatal(err)
			}
			if e.EventType != "task.completed" || e.EventClass != "ingress" || e.Producer.Adapter != name {
				t.Fatalf("unexpected canonical event: %+v", e)
			}
			if e.Scope["repo_id"] == "" || e.Idempotency.Key == "" || e.EventID == "" {
				t.Fatalf("missing stable envelope identity: %+v", e)
			}
			if got, ok := e.Payload["changes"].([]map[string]any); !ok || len(got) != 1 || got[0]["path"] != "internal/app.go" {
				t.Fatalf("changed paths not normalized: %#v", e.Payload["changes"])
			}
		})
	}
}

func TestClientSpecificEventKeysAreNormalized(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"codex", `{"hook":"complete","session_id":"s","task_id":"t","operation_id":"codex-1"}`},
		{"claude-code", `{"eventName":"task.complete","session":"s","task":"t","operation_id":"claude-1"}`},
		{"cursor", `{"type":"idle","sessionId":"s","operation_id":"cursor-1"}`},
		{"gemini-cli", `{"event_type":"task.failed","session_id":"s","task_id":"t","operation_id":"gemini-1"}`},
		{"opencode", `{"event":"model.stop","session":"s","operation_id":"open-1"}`},
		{"commandcode", `{"event":"session.end","session_id":"s","operation_id":"command-1"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := New(tc.name)
			e, err := a.Translate([]byte(tc.raw), TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
			if err != nil {
				t.Fatal(err)
			}
			if e.EventType == "" || e.Producer.Adapter != tc.name {
				t.Fatalf("bad mapping: %+v", e)
			}
		})
	}
}

func TestTranslationRejectsMalformedDuplicateAndTrailingInput(t *testing.T) {
	a, _ := New("codex")
	for name, raw := range map[string][]byte{
		"malformed": []byte(`{"event":`),
		"duplicate": []byte(`{"event":"idle","event":"complete"}`),
		"trailing":  []byte(`{"event":"idle"} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := a.Translate(raw, TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, InstanceID: "instance-1"})
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("error=%v, want ErrSchema", err)
			}
		})
	}
}

func TestTranslationRejectsUnknownEventAndUnsafeCandidate(t *testing.T) {
	a, _ := New("cursor")
	_, err := a.Translate([]byte(`{"event":"surprise"}`), TranslateOptions{})
	if !errors.Is(err, ErrEventType) {
		t.Fatalf("unknown event error=%v", err)
	}
	for _, cwd := range []string{filepath.Join(t.TempDir(), "outside"), filepath.Join(t.TempDir(), "..", "escape")} {
		raw, _ := json.Marshal(map[string]any{"event": "idle", "cwd": cwd, "operation_id": "op-2"})
		_, err = a.Translate(raw, TranslateOptions{ApprovedRoots: []string{filepath.Join(t.TempDir(), "allowed")}, ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
		if !errors.Is(err, ErrScope) {
			t.Fatalf("cwd %q error=%v, want ErrScope", cwd, err)
		}
	}
}

func TestCodexSessionEndHookNameMapsToCanonicalSessionEnded(t *testing.T) {
	a, err := New("codex")
	if err != nil {
		t.Fatal(err)
	}
	e, err := a.Translate([]byte(`{"hook_event_name":"SessionEnd","session_id":"s-1","operation_id":"op-session-end"}`), TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.EventType != "session.ended" {
		t.Fatalf("event type=%q", e.EventType)
	}
}

func TestTranslationRejectsUnsafeChangedPath(t *testing.T) {
	a, _ := New("gemini-cli")
	raw, _ := json.Marshal(map[string]any{"event": "files.changed", "session_id": "s", "task_id": "t", "files": []string{"../secret"}, "operation_id": "op-3"})
	_, err := a.Translate(raw, TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if !errors.Is(err, ErrScope) {
		t.Fatalf("error=%v, want ErrScope", err)
	}
}

func TestTranslationStableIdentityAndSafeCapabilityDegradation(t *testing.T) {
	a, _ := New("opencode")
	raw := []byte(`{"event":"model.stop","session":"s","operation_id":"op-4"}`)
	o := TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, InstallationID: "i", InstanceID: "x"}
	one, err := a.Translate(raw, o)
	if err != nil {
		t.Fatal(err)
	}
	two, err := a.Translate(raw, o)
	if err != nil {
		t.Fatal(err)
	}
	if one.EventID != two.EventID || one.Idempotency.Key != two.Idempotency.Key {
		t.Fatalf("unstable deterministic identity: %q/%q vs %q/%q", one.EventID, one.Idempotency.Key, two.EventID, two.Idempotency.Key)
	}
	if one.EventType != "model.stopped" || one.Capabilities.TaskBoundaries != "synthetic" || one.Capabilities.QueueState != "none" {
		t.Fatalf("unsafe degradation: %+v", one.Capabilities)
	}
}

func TestClientResultMappingIsStableAndNonSuccessIsVisible(t *testing.T) {
	for _, name := range SupportedNames() {
		a, _ := New(name)
		if got := a.MapResult(Result{Disposition: "accepted"}); got.ExitCode != 0 {
			t.Fatalf("%s accepted exit=%d", name, got.ExitCode)
		}
		if got := a.MapResult(Result{Disposition: "rejected", ReasonCode: "E_SCOPE"}); got.ExitCode == 0 || got.Retryable {
			t.Fatalf("%s rejected mapping=%+v", name, got)
		}
		if got := a.MapResult(Result{Disposition: "pending", Retryable: true}); got.ExitCode == 0 || !got.Retryable {
			t.Fatalf("%s pending mapping=%+v", name, got)
		}
	}
}

func TestCanonicalEventJSONIsAnEnvelope(t *testing.T) {
	a, _ := New("commandcode")
	e, err := a.Translate([]byte(`{"event":"session.started","session_id":"s-1","stream_id":"stream-1","operation_id":"op-5"}`), TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil || got["schema_version"] != "autogit.event/1" || got["event_class"] != "ingress" {
		t.Fatalf("not canonical envelope: %s", b)
	}
}

func TestCandidateRootMustBeWithinAnExplicitRoot(t *testing.T) {
	a, _ := New("claude-code")
	root := filepath.Join(t.TempDir(), "project")
	raw, _ := json.Marshal(map[string]any{"event": "idle", "candidate_root": root, "operation_id": "op-6"})
	_, err := a.Translate(raw, TranslateOptions{ApprovedRoots: []string{filepath.Join(t.TempDir(), "other")}, ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if !errors.Is(err, ErrScope) {
		t.Fatalf("error=%v, want ErrScope", err)
	}
}

func TestCanonicalOutputPassesCoreEnvelopeValidation(t *testing.T) {
	a, _ := New("codex")
	e, err := a.Translate([]byte(`{"event":"complete","session_id":"s","task_id":"t","operation_id":"op-schema"}`), TranslateOptions{ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(e)
	if _, err := events.Decode(b, 64<<10); err != nil {
		t.Fatalf("canonical envelope rejected by core: %v; json=%s", err, b)
	}
}
