package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// DomainEventRequest contains only core-owned facts. The builder supplies the
// domain producer envelope and validates the result through the same canonical
// decoder used by ingress events.
type DomainEventRequest struct {
	EventID, EventType, OccurredAt                       string
	RepoID, WorktreeID, SessionID, TaskID, ChangeID      string
	StreamID, CorrelationID, CausationID, IdempotencyKey string
	ProducerInstanceID                                   string
	Payload                                              map[string]any
}

// NewDomainEvent returns canonical JSON for one durable core fact. Event IDs
// are deterministic when omitted, allowing a retried operation to submit the
// same receipt identity; callers must provide the original OccurredAt value
// when reconstructing a retry.
func NewDomainEvent(req DomainEventRequest) ([]byte, error) {
	if req.EventType == "" || req.OccurredAt == "" || req.RepoID == "" || req.CorrelationID == "" || req.IdempotencyKey == "" {
		return nil, errors.New("domain event identity is incomplete")
	}
	if req.ProducerInstanceID == "" {
		req.ProducerInstanceID = "autogit-core"
	}
	if req.StreamID == "" {
		req.StreamID = "repo/" + req.RepoID
	}
	if req.EventID == "" {
		req.EventID = deterministicDomainID(req.RepoID + "\x00" + req.EventType + "\x00" + req.IdempotencyKey)
	}
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	scope := map[string]any{"repo_id": req.RepoID}
	for key, value := range map[string]string{
		"worktree_id": req.WorktreeID,
		"session_id":  req.SessionID,
		"task_id":     req.TaskID,
		"change_id":   req.ChangeID,
	} {
		if value != "" {
			scope[key] = value
		}
	}
	ordering := map[string]any{"stream_id": req.StreamID, "correlation_id": req.CorrelationID}
	if req.CausationID != "" {
		ordering["causation_id"] = req.CausationID
	}
	raw := map[string]any{
		"schema_version": "autogit.event/1",
		"event_class":    "domain",
		"event_id":       req.EventID,
		"event_type":     req.EventType,
		"occurred_at":    req.OccurredAt,
		"producer":       map[string]any{"kind": "core", "instance_id": req.ProducerInstanceID},
		"scope":          scope,
		"ordering":       ordering,
		"idempotency":    map[string]any{"key": req.IdempotencyKey},
		"payload":        req.Payload,
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if _, err := Decode(b, 64<<10); err != nil {
		return nil, err
	}
	return b, nil
}

func deterministicDomainID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return strings.ToUpper(hex.EncodeToString(sum[:])[:26])
}
