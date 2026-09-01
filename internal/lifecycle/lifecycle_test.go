package lifecycle

import (
	"strings"
	"testing"
	"time"
)

func TestReducerLifecycleTransitionsAndWeakCompletion(t *testing.T) {
	r := NewReducer(Config{Now: func() time.Time { return time.Unix(100, 0).UTC() }, NewID: func(kind string) string { return "synthetic-" + kind }})
	s := NewState("repo-1")
	cases := []struct {
		name       string
		e          Event
		session    SessionStatus
		task       TaskStatus
		reason     ReasonCode
		wantPrompt bool
	}{
		{"start", Event{ID: "e1", Type: SessionStarted, SessionID: "s1", TaskID: "t1", Payload: Payload{BaselineHead: "h1", BaselineIndex: "i1"}}, SessionActive, TaskActive, ReasonAccepted, false},
		{"stop is settling", Event{ID: "e2", Type: ModelStopped, SessionID: "s1", TaskID: "t1"}, SessionSettling, TaskActive, ReasonWeakStop, false},
		{"completion claim is not completion", Event{ID: "e3", Type: TaskCompleted, Class: Ingress, SessionID: "s1", TaskID: "t1"}, SessionSettling, TaskActive, ReasonWeakCompletion, false},
		{"blocking prompt waits", Event{ID: "e4", Type: PromptRequested, SessionID: "s1", TaskID: "t1", Payload: Payload{PromptID: "p1", PromptKind: PromptVerification, Blocking: true}}, SessionWaitingPrompt, TaskWaitingPrompt, ReasonAccepted, false},
		{"answer resumes work", Event{ID: "e5", Type: PromptAnswered, SessionID: "s1", TaskID: "t1", Payload: Payload{PromptID: "p1"}}, SessionActive, TaskActive, ReasonAccepted, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, result := r.Reduce(s, tc.e)
			if result.ReasonCode != tc.reason {
				t.Fatalf("reason=%q want %q", result.ReasonCode, tc.reason)
			}
			if next.Session.State != tc.session || next.Tasks["t1"].State != tc.task {
				t.Fatalf("state session=%q task=%q", next.Session.State, next.Tasks["t1"].State)
			}
			if tc.wantPrompt && next.Prompts["p1"].State != PromptPresentedStatus {
				t.Fatalf("prompt=%q want %q", next.Prompts["p1"].State, PromptPresentedStatus)
			}
		})
		// Each case intentionally builds on the preceding immutable snapshot.
		s, _ = r.Reduce(s, tc.e)
	}
	if s.Tasks["t1"].State == TaskCompletedStatus {
		t.Fatal("weak stop/ingress completion claim completed task")
	}
}

func TestReducerQueuesUnknownAndSynthesizesTaskWithoutBoundaries(t *testing.T) {
	r := NewReducer(Config{NewID: func(kind string) string { return "synthetic-" + kind }})
	s := NewState("repo-1")
	e := Event{ID: "e1", Type: PromptSubmitted, SessionID: "s1", Payload: Payload{PromptID: "p1", PromptKind: PromptUnknown, Blocking: true}, Capabilities: Capabilities{QueueState: QueueUnknown, TaskBoundaries: TaskSynthetic}}
	next, result := r.Reduce(s, e)
	if result.ReasonCode != ReasonAccepted || next.Tasks["synthetic-task"].State != TaskWaitingPrompt {
		t.Fatalf("result=%+v tasks=%+v", result, next.Tasks)
	}
	if next.Prompts["p1"].State != PromptQueuedStatus || !next.Prompts["p1"].Blocking {
		t.Fatalf("prompt=%+v; unknown queue must remain queued", next.Prompts["p1"])
	}
}

func TestReducerDuplicateConflictAndCausalReplay(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	first := Event{ID: "e1", Type: SessionStarted, SessionID: "s1", IdempotencyKey: "k1"}
	s, got := r.Reduce(s, first)
	if got.Disposition != Accepted {
		t.Fatalf("first=%+v", got)
	}
	same := first
	s, got = r.Reduce(s, same)
	if got.Disposition != Duplicate || s.Revision != 1 {
		t.Fatalf("duplicate result=%+v revision=%d", got, s.Revision)
	}
	conflict := first
	conflict.ID, conflict.Payload = "e2", Payload{Outcome: "different"}
	s, got = r.Reduce(s, conflict)
	if got.ReasonCode != ReasonIdempotencyConflict || len(s.Quarantine) != 1 {
		t.Fatalf("conflict result=%+v quarantine=%+v", got, s.Quarantine)
	}
	pending := Event{ID: "e3", Type: TaskStarted, SessionID: "s1", TaskID: "t1", CausationID: "missing"}
	s, got = r.Reduce(s, pending)
	if got.Disposition != Pending || len(s.Pending) != 1 {
		t.Fatalf("pending result=%+v pending=%+v", got, s.Pending)
	}
	s, got = r.Reduce(s, Event{ID: "missing", Type: SessionStarted, SessionID: "s1"})
	if got.Disposition != Accepted || len(s.Pending) != 0 || s.Tasks["t1"].State != TaskActive {
		t.Fatalf("replay result=%+v pending=%+v task=%+v", got, s.Pending, s.Tasks["t1"])
	}
}

func TestReducerInvalidatesImmutableEvidenceOnCandidateAndPolicyChanges(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	for _, e := range []Event{
		{ID: "s", Type: SessionStarted, SessionID: "s1", TaskID: "t1"},
		{ID: "c", Type: ChangeStaged, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{BaseDigest: "base-1", CandidateDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TreeDigest: "tree-1", IndexDigest: "index-1", PolicyDigest: "policy-1", VerifierDigest: "verifier-1", GuardDigest: "guard-1", MessageDigest: "message-1"}},
		{ID: "v1", Type: VerificationPassed, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{VerificationID: "v1", CandidateDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BaseDigest: "base-1", PolicyDigest: "policy-1", VerifierDigest: "verifier-1", GuardDigest: "guard-1", EvidenceDigest: "ev-1"}},
	} {
		s, _ = r.Reduce(s, e)
	}
	if !s.Verifications["v1"].Reusable {
		t.Fatal("matching verification should be reusable")
	}
	next, result := r.Reduce(s, Event{ID: "c2", Type: ChangeDetected, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CandidateDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", BaseDigest: "base-2", TreeDigest: "tree-2", IndexDigest: "index-2", PolicyDigest: "policy-2"}})
	if result.ReasonCode != ReasonEvidenceInvalidated || next.Verifications["v1"].Reusable {
		t.Fatalf("result=%+v verification=%+v", result, next.Verifications["v1"])
	}
}

func TestReducerCommitPushFactsRequireExactEvidenceAndAreDistinct(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	base := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, e := range []Event{
		{ID: "s", Type: SessionStarted, SessionID: "s1", TaskID: "t1"},
		{ID: "c", Type: ChangeStaged, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CandidateDigest: base, BaseDigest: "base", TreeDigest: "tree", IndexDigest: "idx", PolicyDigest: "pol", VerifierDigest: "ver", GuardDigest: "guard", MessageDigest: "msg"}},
		{ID: "v", Type: VerificationPassed, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{VerificationID: "v", CandidateDigest: base, BaseDigest: "base", PolicyDigest: "pol", VerifierDigest: "ver", GuardDigest: "guard", EvidenceDigest: "evidence"}},
		{ID: "cr", Type: CommitRequested, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CommitJobID: "job", CandidateDigest: base, BaseDigest: "base", MessageDigest: "msg", PolicyDigest: "pol", VerifierDigest: "ver", GuardDigest: "guard"}},
	} {
		s, _ = r.Reduce(s, e)
	}
	if s.Commits["job"].State != CommitQueued {
		t.Fatalf("commit=%+v", s.Commits["job"])
	}
	next, bad := r.Reduce(s, Event{ID: "bad", Type: PushRequested, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{PushJobID: "push", CommitSHA: "deadbeef"}})
	if bad.ReasonCode != ReasonInvalidTransition || next.Pushes["push"].State != "" {
		t.Fatalf("bad push result=%+v push=%+v", bad, next.Pushes["push"])
	}
	next, _ = r.Reduce(s, Event{ID: "cc", Type: CommitCreated, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CommitJobID: "job", CandidateDigest: base, CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	next, ok := r.Reduce(next, Event{ID: "pr", Type: PushRequested, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{PushJobID: "push", CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RemoteDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: "refs/heads/main"}})
	if ok.ReasonCode != ReasonAccepted || next.Pushes["push"].State != PushQueued || next.Commits["job"].State != CommitCreatedStatus {
		t.Fatalf("push=%+v commit=%+v result=%+v", next.Pushes["push"], next.Commits["job"], ok)
	}
}

func TestReducerPolicyRevisionInvalidatesCandidateEvidence(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	base := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, e := range []Event{
		{ID: "policy-1", Type: PolicySet, Payload: Payload{PolicyDigest: "policy-a", Outcome: "yes", Visibility: "private"}},
		{ID: "change-1", Type: ChangeStaged, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CandidateDigest: base, BaseDigest: "base", PolicyDigest: "policy-a"}},
		{ID: "verify-1", Type: VerificationPassed, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{VerificationID: "v1", CandidateDigest: base, BaseDigest: "base", PolicyDigest: "policy-a", EvidenceDigest: "evidence"}},
	} {
		s, _ = r.Reduce(s, e)
	}
	next, result := r.Reduce(s, Event{ID: "policy-2", Type: PolicySet, Payload: Payload{PolicyDigest: "policy-b", Outcome: "yes", Visibility: "private"}})
	if result.ReasonCode != ReasonEvidenceInvalidated || next.Verifications["v1"].Reusable {
		t.Fatalf("result=%+v verification=%+v", result, next.Verifications["v1"])
	}
}

func TestReducerSequenceGapIsPendingUntilMissingSequenceArrives(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	seq2 := int64(2)
	next, result := r.Reduce(s, Event{ID: "e2", Type: SessionStarted, SessionID: "s1", StreamID: "stream", ProducerSeq: &seq2, Capabilities: Capabilities{MonotonicSequence: true}})
	if result.Disposition != Pending || len(next.Pending) != 1 {
		t.Fatalf("result=%+v pending=%+v", result, next.Pending)
	}
	seq1 := int64(1)
	next, result = r.Reduce(next, Event{ID: "e1", Type: SessionStarted, SessionID: "s1", StreamID: "stream", ProducerSeq: &seq1, Capabilities: Capabilities{MonotonicSequence: true}})
	if result.Disposition != Accepted || len(next.Pending) != 0 || next.Session.State != SessionActive {
		t.Fatalf("result=%+v pending=%+v session=%+v", result, next.Pending, next.Session)
	}
}

func TestReducerDoesNotMutateInputStateAndQuarantinedEventsDeduplicate(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	started, _ := r.Reduce(s, Event{ID: "start", Type: SessionStarted, SessionID: "s1"})
	if s.Revision != 0 || len(s.Receipts) != 0 || started.Revision != 1 {
		t.Fatalf("input state was mutated: input=%+v result=%+v", s, started)
	}
	bad := Event{ID: "bad", Type: PromptAnswered, SessionID: "s1", TaskID: "t1", Payload: Payload{PromptID: "missing"}}
	next, first := r.Reduce(started, bad)
	if first.Disposition != Rejected || len(next.Quarantine) != 1 {
		t.Fatalf("first invalid result=%+v quarantine=%+v", first, next.Quarantine)
	}
	next, duplicate := r.Reduce(next, bad)
	if duplicate.Disposition != Duplicate || len(next.Quarantine) != 1 {
		t.Fatalf("duplicate invalid result=%+v quarantine=%+v", duplicate, next.Quarantine)
	}
}

func TestReducerAdvancesSequenceThroughReplay(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	seq := func(n int64) *int64 { return &n }
	cap := Capabilities{MonotonicSequence: true}
	var result Result
	s, result = r.Reduce(s, Event{ID: "e2", Type: SessionIdle, SessionID: "s1", StreamID: "stream", ProducerSeq: seq(2), Capabilities: cap})
	if result.Disposition != Pending {
		t.Fatalf("e2 result=%+v", result)
	}
	s, result = r.Reduce(s, Event{ID: "e1", Type: SessionStarted, SessionID: "s1", StreamID: "stream", ProducerSeq: seq(1), Capabilities: cap})
	if result.Disposition != Accepted || s.LastSequence["stream"] != 2 || len(s.Pending) != 0 {
		t.Fatalf("after replay result=%+v sequence=%d pending=%v", result, s.LastSequence["stream"], s.Pending)
	}
	s, result = r.Reduce(s, Event{ID: "e3", Type: SessionIdle, SessionID: "s1", StreamID: "stream", ProducerSeq: seq(3), Capabilities: cap})
	if result.Disposition != Accepted || s.LastSequence["stream"] != 3 {
		t.Fatalf("e3 result=%+v sequence=%d", result, s.LastSequence["stream"])
	}
}

func TestReducerRejectsRegressiveProducerSequenceWithNewEventID(t *testing.T) {
	r := NewReducer(Config{})
	s := NewState("repo-1")
	seq := func(n int64) *int64 { return &n }
	cap := Capabilities{MonotonicSequence: true}
	s, _ = r.Reduce(s, Event{ID: "e1", Type: SessionStarted, SessionID: "s1", StreamID: "stream", ProducerSeq: seq(1), Capabilities: cap})
	next, result := r.Reduce(s, Event{ID: "e0", Type: SessionIdle, SessionID: "s1", StreamID: "stream", ProducerSeq: seq(1), Capabilities: cap})
	if result.Disposition != Rejected || result.ReasonCode != ReasonInvalidTransition || len(next.Quarantine) != 1 {
		t.Fatalf("result=%+v quarantine=%v", result, next.Quarantine)
	}
}

func evidenceState(t *testing.T) (Reducer, State, string) {
	t.Helper()
	r := NewReducer(Config{})
	s := NewState("repo-1")
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, e := range []Event{
		{ID: "start", Type: SessionStarted, SessionID: "s1", TaskID: "t1"},
		{ID: "change", Type: ChangeStaged, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CandidateDigest: digest, BaseDigest: "base", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard", MessageDigest: "message"}},
	} {
		s, _ = r.Reduce(s, e)
	}
	return r, s, digest
}

func TestReducerVerificationPassedRequiresEveryEvidenceField(t *testing.T) {
	fields := []struct {
		name   string
		mutate func(*Payload)
	}{
		{"candidate", func(p *Payload) { p.CandidateDigest = "sha256:" + strings.Repeat("b", 64) }},
		{"base", func(p *Payload) { p.BaseDigest = "other-base" }},
		{"policy", func(p *Payload) { p.PolicyDigest = "other-policy" }},
		{"verifier", func(p *Payload) { p.VerifierDigest = "other-verifier" }},
		{"guard", func(p *Payload) { p.GuardDigest = "other-guard" }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			r, s, digest := evidenceState(t)
			p := Payload{VerificationID: "v-" + field.name, CandidateDigest: digest, BaseDigest: "base", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard", EvidenceDigest: "evidence"}
			field.mutate(&p)
			next, result := r.Reduce(s, Event{ID: "verify-" + field.name, Type: VerificationPassed, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: p})
			if result.Disposition != Rejected || result.ReasonCode != ReasonInvalidTransition || len(next.Verifications) != 0 {
				t.Fatalf("result=%+v verifications=%v", result, next.Verifications)
			}
		})
	}
}

func TestReducerCommitRequestRequiresExactEvidenceAndMessage(t *testing.T) {
	fields := []struct {
		name   string
		mutate func(*Payload)
	}{
		{"candidate", func(p *Payload) { p.CandidateDigest = "sha256:" + strings.Repeat("b", 64) }},
		{"base", func(p *Payload) { p.BaseDigest = "other-base" }},
		{"policy", func(p *Payload) { p.PolicyDigest = "other-policy" }},
		{"verifier", func(p *Payload) { p.VerifierDigest = "other-verifier" }},
		{"guard", func(p *Payload) { p.GuardDigest = "other-guard" }},
		{"message", func(p *Payload) { p.MessageDigest = "other-message" }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			r, s, digest := evidenceState(t)
			s, _ = r.Reduce(s, Event{ID: "verify", Type: VerificationPassed, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{VerificationID: "v1", CandidateDigest: digest, BaseDigest: "base", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard", EvidenceDigest: "evidence"}})
			p := Payload{CommitJobID: "job-" + field.name, CandidateDigest: digest, BaseDigest: "base", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard", MessageDigest: "message"}
			field.mutate(&p)
			next, result := r.Reduce(s, Event{ID: "commit-" + field.name, Type: CommitRequested, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: p})
			if result.Disposition != Rejected || len(next.Commits) != 0 {
				t.Fatalf("result=%+v commits=%v", result, next.Commits)
			}
		})
	}
}

func TestReducerCommitAndPushRequireStrictSHAAndPushIdentity(t *testing.T) {
	r, s, digest := evidenceState(t)
	s, _ = r.Reduce(s, Event{ID: "verify", Type: VerificationPassed, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{VerificationID: "v1", CandidateDigest: digest, BaseDigest: "base", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard", EvidenceDigest: "evidence"}})
	s, _ = r.Reduce(s, Event{ID: "request", Type: CommitRequested, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CommitJobID: "job", CandidateDigest: digest, BaseDigest: "base", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard", MessageDigest: "message"}})
	for _, sha := range []string{"not-a-sha", strings.Repeat("a", 41), strings.Repeat("A", 40)} {
		next, result := r.Reduce(s, Event{ID: "created-" + sha, Type: CommitCreated, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CommitJobID: "job", CandidateDigest: digest, CommitSHA: sha}})
		if result.Disposition != Rejected || next.Commits["job"].State != CommitQueued {
			t.Fatalf("sha=%q result=%+v commit=%+v", sha, result, next.Commits["job"])
		}
	}
	validSHA := strings.Repeat("a", 40)
	s, _ = r.Reduce(s, Event{ID: "created-valid", Type: CommitCreated, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: Payload{CommitJobID: "job", CandidateDigest: digest, CommitSHA: validSHA}})
	for _, p := range []Payload{{PushJobID: "p1", CommitSHA: validSHA, RemoteDigest: "bad", Ref: "refs/heads/main"}, {PushJobID: "p2", CommitSHA: validSHA, RemoteDigest: "sha256:" + strings.Repeat("b", 64), Ref: "../main"}, {PushJobID: "p3", CommitSHA: strings.Repeat("b", 40), RemoteDigest: "sha256:" + strings.Repeat("b", 64), Ref: "refs/heads/main"}} {
		next, result := r.Reduce(s, Event{ID: "push-" + p.PushJobID, Type: PushRequested, SessionID: "s1", TaskID: "t1", ChangeID: "c1", Payload: p})
		if result.Disposition != Rejected || next.Pushes[p.PushJobID].State != "" {
			t.Fatalf("push=%+v result=%+v state=%+v", p, result, next.Pushes[p.PushJobID])
		}
	}
}

func TestReducerRejectsIngressDomainFactForgery(t *testing.T) {
	for _, typ := range []EventType{PolicySet, VerificationPassed, CommitRequested, PushRequested} {
		r := NewReducer(Config{})
		next, result := r.Reduce(NewState("repo-1"), Event{ID: "forged-" + string(typ), Type: typ, Class: Ingress, SessionID: "s1", TaskID: "t1", ChangeID: "c1"})
		if result.Disposition != Rejected || result.ReasonCode != ReasonInvalidTransition || len(next.Quarantine) != 1 {
			t.Fatalf("type=%s result=%+v quarantine=%v", typ, result, next.Quarantine)
		}
	}
}
