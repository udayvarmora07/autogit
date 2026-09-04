package events

import (
	"bytes"
	"testing"
)

func TestNewDomainEventBuildsDeterministicReplaySafeEnvelope(t *testing.T) {
	req := DomainEventRequest{
		EventType: "commit.created", OccurredAt: "2026-09-04T00:00:00Z",
		RepoID: "sha256:" + repeatDomain('a'), WorktreeID: "sha256:" + repeatDomain('b'),
		SessionID: "session-1", TaskID: "task-1", ChangeID: "change-1",
		CorrelationID: "correlation-1", IdempotencyKey: "commit/commit-1",
		Payload: map[string]any{
			"commit_job_id": "commit-1", "candidate_digest": "sha256:" + repeatDomain('c'),
			"base_head_digest": "sha256:" + repeatDomain('e'), "policy_digest": "sha256:" + repeatDomain('f'),
			"verifier_digest": "sha256:" + repeatDomain('a'), "guard_digest": "sha256:" + repeatDomain('b'),
			"message_digest": "sha256:" + repeatDomain('c'), "commit_sha": repeatDomain('d'),
		},
	}
	first, err := NewDomainEvent(req)
	if err != nil {
		t.Fatalf("build domain event: %v", err)
	}
	second, err := NewDomainEvent(req)
	if err != nil {
		t.Fatalf("rebuild domain event: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("domain event is not deterministic:\n%s\n%s", first, second)
	}
	decoded, err := Decode(first, 64<<10)
	if err != nil {
		t.Fatalf("decode domain event: %v", err)
	}
	if decoded.EventClass != "domain" || decoded.Producer["kind"] != "core" || decoded.Ordering["correlation_id"] != req.CorrelationID {
		t.Fatalf("decoded envelope=%+v", decoded)
	}
}

func TestNewDomainEventRequiresStableCorrelationAndIdempotency(t *testing.T) {
	base := DomainEventRequest{EventType: "push.succeeded", OccurredAt: "2026-09-04T00:00:00Z", RepoID: "sha256:" + repeatDomain('a'), WorktreeID: "sha256:" + repeatDomain('b'), SessionID: "s", TaskID: "t", ChangeID: "c", Payload: map[string]any{"push_job_id": "push-1", "commit_sha": repeatDomain('d')}}
	for name, mutate := range map[string]func(*DomainEventRequest){
		"missing correlation": func(r *DomainEventRequest) { r.CorrelationID = "" },
		"missing idempotency": func(r *DomainEventRequest) { r.IdempotencyKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			req := base
			mutate(&req)
			if _, err := NewDomainEvent(req); err == nil {
				t.Fatal("invalid domain event accepted")
			}
		})
	}
}

func repeatDomain(ch byte) string {
	result := make([]byte, 64)
	for i := range result {
		result[i] = ch
	}
	return string(result)
}
