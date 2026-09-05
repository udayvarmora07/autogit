package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"autogit/internal/events"
	"autogit/internal/policy"
)

func BenchmarkHookNoCandidate(b *testing.B) {
	store, err := events.OpenStore(filepath.Join(b.TempDir(), "state.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	application := New(store, policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe", Version: 1}, nil)
	const repoID = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	rawEvents := make([][]byte, 1000)
	for i := range rawEvents {
		eventID := fmt.Sprintf("%026d", i+1)
		rawEvents[i] = []byte(fmt.Sprintf(`{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"%s","event_type":"session.idle","occurred_at":"2026-09-05T00:00:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"benchmark","instance_id":"benchmark"},"scope":{"repo_id":"%s","session_id":"benchmark-session"},"ordering":{"stream_id":"benchmark-stream"},"idempotency":{"key":"idle-%d"},"payload":{}}`, eventID, repoID, i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, hookErr := application.Hook(context.Background(), rawEvents[i%len(rawEvents)])
		if hookErr != nil || result.Action != "none" && result.Action != "notify" {
			b.Fatalf("no-candidate hook result=%+v err=%v", result, hookErr)
		}
	}
}
