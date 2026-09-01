package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const matrixRepo = "hmac-sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func matrixOptions() TranslateOptions {
	return TranslateOptions{
		ResolvedScope:  map[string]string{"repo_id": matrixRepo},
		InstallationID: "install-matrix",
		InstanceID:     "instance-matrix",
	}
}

func TestP303ManifestsAreVersionedAndDescribeClientContracts(t *testing.T) {
	for _, name := range SupportedNames() {
		t.Run(name, func(t *testing.T) {
			manifest, err := ManifestFor(name)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Adapter != name || len(manifest.SchemaMajors) != 1 || manifest.SchemaMajors[0] != "autogit.event/1" {
				t.Fatalf("manifest lacks canonical version: %+v", manifest)
			}
			if len(manifest.ClientVersions) == 0 || len(manifest.EventMappings) == 0 {
				t.Fatalf("manifest lacks version or event mapping metadata: %+v", manifest)
			}
			if manifest.InstallSupported && (name == "cursor" || name == "opencode") {
				t.Fatalf("%s must not claim an install contract", name)
			}
		})
	}
}

func TestP303OfficialAndSyntheticFixturesMapPerClient(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"codex", `{"hook_event_name":"SessionEnd","session_id":"codex-session","operation_id":"codex-op"}`, "session.ended"},
		{"claude-code", `{"hook_event_name":"Stop","session_id":"claude-session","operation_id":"claude-op"}`, "model.stopped"},
		{"cursor", `{"observation":"idle","sessionId":"cursor-session","operation_id":"cursor-op"}`, "session.idle"},
		{"gemini-cli", `{"hook_event_name":"SessionEnd","session_id":"gemini-session","operation_id":"gemini-op"}`, "session.ended"},
		{"opencode", `{"observation":"model.stop","session":"opencode-session","operation_id":"opencode-op"}`, "model.stopped"},
		{"commandcode", `{"signal":"session.end","session_id":"command-session","operation_id":"command-op"}`, "session.ended"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := New(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			e, err := a.Translate([]byte(tc.raw), matrixOptions())
			if err != nil {
				t.Fatal(err)
			}
			if e.EventType != tc.want || e.Producer.Version == "1" {
				t.Fatalf("client-specific mapping/version lost: type=%q producer=%+v", e.EventType, e.Producer)
			}
		})
	}
}

func TestP303UnknownCanonicalMajorIsRejected(t *testing.T) {
	a, _ := New("codex")
	_, err := a.Translate([]byte(`{"schema_version":"autogit.event/99","event":"idle","session_id":"s","operation_id":"o"}`), matrixOptions())
	if !errors.Is(err, ErrVersion) {
		t.Fatalf("error=%v, want ErrVersion", err)
	}
}

func TestP303UnknownClientVersionDegradesCapabilitiesSafely(t *testing.T) {
	a, _ := New("codex")
	opts := matrixOptions()
	opts.ClientVersion = "999.0.0"
	e, err := a.Translate([]byte(`{"event":"idle","session_id":"s","operation_id":"o"}`), opts)
	if err != nil {
		t.Fatal(err)
	}
	if e.Producer.Version != "999.0.0" || e.Capabilities == nil || e.Capabilities.TaskBoundaries != "synthetic" || e.Capabilities.QueueState != "unknown" || e.Capabilities.MonotonicSequence {
		t.Fatalf("unknown client version was not safely degraded: producer=%+v capabilities=%+v", e.Producer, e.Capabilities)
	}
}

func TestP303SequenceRepresentationDoesNotInventGaps(t *testing.T) {
	for _, name := range SupportedNames() {
		t.Run(name, func(t *testing.T) {
			a, _ := New(name)
			e, err := a.Translate([]byte(`{"event":"idle","session_id":"s","producer_seq":7,"operation_id":"o"}`), matrixOptions())
			if err != nil {
				t.Fatal(err)
			}
			if e.Ordering.ProducerSeq == nil || *e.Ordering.ProducerSeq != 7 {
				t.Fatalf("sequence was not represented: %+v", e.Ordering)
			}
			noSeq, err := a.Translate([]byte(`{"event":"idle","session_id":"s","operation_id":"o2"}`), matrixOptions())
			if err != nil {
				t.Fatal(err)
			}
			if noSeq.Ordering.ProducerSeq != nil {
				t.Fatal("adapter invented a sequence for a gap/unknown predecessor")
			}
		})
	}
}

func matrixFixture(name, event string) []byte {
	fields := map[string]any{"operation_id": name + "-operation", "session_id": name + "-session", "task_id": name + "-task"}
	switch name {
	case "codex", "claude-code", "gemini-cli":
		fields["hook_event_name"] = event
	case "cursor", "opencode":
		fields["observation"] = event
	case "commandcode":
		fields["signal"] = event
	}
	return mustJSON(fields)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("fixture marshal: %v", err))
	}
	return b
}

// TestP303CommonEightCaseSuite expands to 48 adapter cases (six clients x the
// eight contract behaviors required by P3-03).
func TestP303CommonEightCaseSuite(t *testing.T) {
	for _, name := range SupportedNames() {
		t.Run(name, func(t *testing.T) {
			a, err := New(name)
			if err != nil {
				t.Fatal(err)
			}
			t.Run("valid-official-or-observation", func(t *testing.T) {
				e, err := a.Translate(matrixFixture(name, "session.started"), matrixOptions())
				if err != nil || e.EventType != "session.started" || e.Producer.Adapter != name {
					t.Fatalf("event=%+v err=%v", e, err)
				}
			})
			t.Run("malformed-and-duplicate-json", func(t *testing.T) {
				for _, raw := range []string{`{"event":`, `{"event":"idle","event":"end"}`} {
					if _, err := a.Translate([]byte(raw), matrixOptions()); !errors.Is(err, ErrSchema) {
						t.Fatalf("raw=%q err=%v", raw, err)
					}
				}
			})
			t.Run("unknown-major-or-event", func(t *testing.T) {
				var obj map[string]any
				_ = json.Unmarshal(matrixFixture(name, "idle"), &obj)
				obj["schema_version"] = "autogit.event/77"
				if _, err := a.Translate(mustJSON(obj), matrixOptions()); !errors.Is(err, ErrVersion) {
					t.Fatalf("major error=%v", err)
				}
				delete(obj, "schema_version")
				switch name {
				case "codex", "claude-code", "gemini-cli":
					obj["hook_event_name"] = "future.signal"
				case "cursor", "opencode":
					obj["observation"] = "future.signal"
				case "commandcode":
					obj["signal"] = "future.signal"
				}
				if _, err := a.Translate(mustJSON(obj), matrixOptions()); !errors.Is(err, ErrEventType) {
					t.Fatalf("event error=%v", err)
				}
			})
			t.Run("duplicate-identity-is-deterministic", func(t *testing.T) {
				raw := matrixFixture(name, "idle")
				one, e1 := a.Translate(raw, matrixOptions())
				two, e2 := a.Translate(raw, matrixOptions())
				if e1 != nil || e2 != nil || one.EventID != two.EventID || one.Idempotency.Key != two.Idempotency.Key {
					t.Fatalf("one=%+v/%v two=%+v/%v", one, e1, two, e2)
				}
			})
			t.Run("missing-capabilities-safe-degradation", func(t *testing.T) {
				e, err := a.Translate(matrixFixture(name, "idle"), matrixOptions())
				if err != nil || e.Capabilities == nil || e.Capabilities.QueueState == "" || e.Capabilities.TaskBoundaries == "" || e.Capabilities.ChangedPaths == "" {
					t.Fatalf("capabilities=%+v err=%v", e.Capabilities, err)
				}
				var partial map[string]any
				_ = json.Unmarshal(matrixFixture(name, "idle"), &partial)
				partial["capabilities"] = map[string]any{}
				e, err = a.Translate(mustJSON(partial), matrixOptions())
				if err != nil || e.Capabilities.QueueState != "unknown" || e.Capabilities.TaskBoundaries != "synthetic" || e.Capabilities.ChangedPaths != "derived" || e.Capabilities.MonotonicSequence {
					t.Fatalf("partial capabilities were inferred unsafely: %+v err=%v", e.Capabilities, err)
				}
			})
			t.Run("unsafe-root-or-path", func(t *testing.T) {
				outside := filepath.Join(t.TempDir(), "outside")
				var obj map[string]any
				_ = json.Unmarshal(matrixFixture(name, "idle"), &obj)
				obj["cwd"] = outside
				allowed := filepath.Join(t.TempDir(), "allowed")
				rootOpts := matrixOptions()
				rootOpts.ApprovedRoots = []string{allowed}
				if _, err := a.Translate(mustJSON(obj), rootOpts); !errors.Is(err, ErrScope) {
					t.Fatalf("root=%q err=%v", outside, err)
				}
				pathObj := map[string]any{"event": "files.changed", "session_id": "s", "task_id": "t", "operation_id": "path-op", "files": []string{"../escape"}}
				if _, err := a.Translate(mustJSON(pathObj), matrixOptions()); !errors.Is(err, ErrScope) {
					t.Fatalf("path err=%v", err)
				}
			})
			t.Run("sequence-gap-is-represented", func(t *testing.T) {
				obj := map[string]any{"event": "idle", "session_id": "s", "operation_id": "sequence-op", "producer_seq": 9}
				e, err := a.Translate(mustJSON(obj), matrixOptions())
				if err != nil || e.Ordering.ProducerSeq == nil || *e.Ordering.ProducerSeq != 9 {
					t.Fatalf("sequence=%+v err=%v", e.Ordering, err)
				}
				obj["operation_id"] = "sequence-gap"
				delete(obj, "producer_seq")
				gap, err := a.Translate(mustJSON(obj), matrixOptions())
				if err != nil || gap.Ordering.ProducerSeq != nil {
					t.Fatalf("gap sequence=%+v err=%v", gap.Ordering, err)
				}
			})
			t.Run("result-exit-mapping", func(t *testing.T) {
				if got := a.MapResult(Result{Disposition: "accepted"}); got.ExitCode != 0 || got.Retryable {
					t.Fatalf("accepted=%+v", got)
				}
				if got := a.MapResult(Result{Disposition: "rejected", ReasonCode: "E_SCOPE"}); got.ExitCode == 0 || got.Retryable {
					t.Fatalf("rejected=%+v", got)
				}
			})
		})
	}
}

func TestP303UnknownVersionDoesNotChangeTrustedScope(t *testing.T) {
	a, _ := New("gemini-cli")
	opts := matrixOptions()
	opts.ClientVersion = "999.0.0"
	obj := map[string]any{"event": "idle", "session_id": "s", "operation_id": "unknown-version", "repo_id": "sha256:" + strings.Repeat("f", 64)}
	e, err := a.Translate(mustJSON(obj), opts)
	if err != nil || e.Scope["repo_id"] != matrixRepo {
		t.Fatalf("trusted scope changed: event=%+v err=%v", e, err)
	}
}

func TestP303CanonicalDigestIsDeterministic(t *testing.T) {
	a, _ := New("codex")
	raw := []byte(`{"event":"idle","session_id":"s","operation_id":"digest-op","occurred_at":"2026-09-01T00:00:00Z"}`)
	one, err := a.Translate(raw, matrixOptions())
	if err != nil {
		t.Fatal(err)
	}
	two, err := a.Translate(raw, matrixOptions())
	if err != nil {
		t.Fatal(err)
	}
	d1, err := CanonicalDigest(one)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := CanonicalDigest(two)
	if err != nil || d1 != d2 || !validDigest(d1) {
		t.Fatalf("digest mismatch: %q %q err=%v", d1, d2, err)
	}
}

func TestP303MissingTimestampReplayIsCanonicalAcrossReceiptTime(t *testing.T) {
	a, _ := New("claude-code")
	raw := []byte(`{"hook_event_name":"Stop","session_id":"replay-session","operation_id":"replay-op"}`)
	one, err := a.Translate(raw, matrixOptions())
	if err != nil {
		t.Fatal(err)
	}
	first, err := CanonicalDigest(one)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	two, err := a.Translate(raw, matrixOptions())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalDigest(two)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || one.OccurredAt != two.OccurredAt {
		t.Fatalf("missing timestamp was receipt-time dependent: first=%s/%s second=%s/%s", one.OccurredAt, first, two.OccurredAt, second)
	}
}
