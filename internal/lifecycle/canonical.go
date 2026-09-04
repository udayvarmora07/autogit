package lifecycle

import (
	"autogit/internal/events"
	"strconv"
)

// FromCanonical translates the validated event envelope into the reducer's
// bounded typed facts. It does not retain arbitrary payload fields.
func FromCanonical(in events.Event) Event {
	e := Event{ID: in.EventID, Type: EventType(in.EventType), Class: EventClass(in.EventClass), RepoID: stringValue(in.Scope["repo_id"]), WorktreeID: stringValue(in.Scope["worktree_id"]), SessionID: stringValue(in.Scope["session_id"]), TaskID: stringValue(in.Scope["task_id"]), ChangeID: stringValue(in.Scope["change_id"]), StreamID: stringValue(in.Ordering["stream_id"]), CausationID: stringValue(in.Ordering["causation_id"]), CorrelationID: stringValue(in.Ordering["correlation_id"]), IdempotencyKey: stringValue(in.Idempotency["key"]), Digest: in.Digest}
	if n, ok := in.Ordering["producer_seq"]; ok {
		switch v := n.(type) {
		case int64:
			e.ProducerSeq = &v
		case int:
			n := int64(v)
			e.ProducerSeq = &n
		case float64:
			n := int64(v)
			e.ProducerSeq = &n
		case string:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				e.ProducerSeq = &n
			}
		}
	}
	e.Capabilities = Capabilities{QueueState: stringValue(in.Capabilities["queue_state"]), TaskBoundaries: stringValue(in.Capabilities["task_boundaries"]), ChangedPaths: stringValue(in.Capabilities["changed_paths"]), MonotonicSequence: boolValue(in.Capabilities["monotonic_sequence"])}
	e.Payload = payloadFromMap(in.Payload)
	return e
}

func (r Reducer) ReduceCanonical(state State, in events.Event) (State, Result) {
	return r.Reduce(state, FromCanonical(in))
}

// ApplyIngress is the session-facing name used by the architecture contract;
// the reducer still applies domain facts through the same deterministic path.
func (r Reducer) ApplyIngress(state State, event Event) (State, Result) {
	return r.Reduce(state, event)
}
func (r Reducer) Apply(state State, event Event) (State, Result) { return r.Reduce(state, event) }

func payloadFromMap(m map[string]any) Payload {
	baseDigest := stringValue(m["base_digest"])
	if baseDigest == "" {
		baseDigest = stringValue(m["base_head_digest"])
	}
	p := Payload{Status: stringValue(m["status"]), Outcome: stringValue(m["outcome"]), Reason: stringValue(m["reason"]), PromptID: stringValue(m["prompt_id"]), PromptKind: PromptKind(stringValue(m["prompt_kind"])), Answer: stringValue(m["answer"]), BaselineHead: stringValue(m["baseline_head"]), BaselineIndex: stringValue(m["baseline_index"]), StatusDigest: stringValue(m["status_digest"]), BaselinePathsDigest: stringValue(m["baseline_paths_digest"]), CandidateDigest: stringValue(m["candidate_digest"]), BaseDigest: baseDigest, TreeDigest: stringValue(m["tree_digest"]), IndexDigest: stringValue(m["index_digest"]), PolicyDigest: stringValue(m["policy_digest"]), VerifierDigest: stringValue(m["verifier_digest"]), GuardDigest: stringValue(m["guard_digest"]), MessageDigest: stringValue(m["message_digest"]), EvidenceDigest: stringValue(m["evidence_digest"]), VerificationID: stringValue(m["verification_id"]), CommitJobID: stringValue(m["commit_job_id"]), CommitSHA: stringValue(m["commit_sha"]), PushJobID: stringValue(m["push_job_id"]), RemoteDigest: stringValue(m["remote_digest"]), Ref: stringValue(m["ref"]), ErrorCode: stringValue(m["error_code"]), Visibility: stringValue(m["visibility"]), QueueState: stringValue(m["queue_state"])}
	p.Blocking = boolValue(m["blocking"])
	p.ExplicitComplete = boolValue(m["explicit_complete"])
	p.PublicConsent = boolValue(m["public_consent"])
	p.CompletionEligible = boolValue(m["completion_eligible"])
	p.ActiveTool = boolValue(m["active_tool"])
	p.Ambiguous = boolValue(m["ambiguous"])
	p.LocalOnly = boolValue(m["local_only"])
	return p
}
func stringValue(v any) string { s, _ := v.(string); return s }
func boolValue(v any) bool     { b, _ := v.(bool); return b }
