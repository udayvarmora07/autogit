// Package lifecycle is the side-effect-free projection of AutoGit's durable
// lifecycle facts. It deliberately knows nothing about Git, providers, or
// persistence; callers can replay the same facts to obtain the same state.
package lifecycle

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

type EventType string

const (
	RepositoryDiscovered    EventType = "repository.discovered"
	PolicyConsentRequested  EventType = "policy.consent_requested"
	PolicySet               EventType = "policy.set"
	SessionStarted          EventType = "session.started"
	SessionIdle             EventType = "session.idle"
	SessionEnded            EventType = "session.ended"
	SessionCrashed          EventType = "session.crashed"
	SessionRecovered        EventType = "session.recovered"
	PromptSubmitted         EventType = "prompt.submitted"
	PromptRequested         EventType = "prompt.requested"
	PromptQueued            EventType = "prompt.queued"
	PromptPresented         EventType = "prompt.presented"
	PromptAnswered          EventType = "prompt.answered"
	PromptExpired           EventType = "prompt.expired"
	PromptCancelled         EventType = "prompt.cancelled"
	TaskStarted             EventType = "task.started"
	TaskUpdated             EventType = "task.updated"
	TaskCompletionCandidate EventType = "task.completion_candidate"
	TaskCompleted           EventType = "task.completed"
	TaskFailed              EventType = "task.failed"
	TaskCancelled           EventType = "task.cancelled"
	ToolStarted             EventType = "tool.started"
	ToolCompleted           EventType = "tool.completed"
	FilesChanged            EventType = "files.changed"
	ModelStopped            EventType = "model.stopped"
	ChangeDetected          EventType = "change.detected"
	ChangeStaged            EventType = "change.staged"
	ChangeInvalidated       EventType = "change.invalidated"
	VerificationRequested   EventType = "verification.requested"
	VerificationStarted     EventType = "verification.started"
	VerificationPassed      EventType = "verification.passed"
	VerificationFailed      EventType = "verification.failed"
	VerificationInvalidated EventType = "verification.invalidated"
	CommitRequested         EventType = "commit.requested"
	CommitCreated           EventType = "commit.created"
	CommitFailed            EventType = "commit.failed"
	CommitReconciled        EventType = "commit.reconciled"
	PushRequested           EventType = "push.requested"
	PushSucceeded           EventType = "push.succeeded"
	PushFailed              EventType = "push.failed"
	PushSkipped             EventType = "push.skipped"
)

type EventClass string

const (
	Ingress EventClass = "ingress"
	Domain  EventClass = "domain"
)

type SessionStatus string

const (
	SessionCreated             SessionStatus = "CREATED"
	SessionActive              SessionStatus = "ACTIVE"
	SessionWaitingPrompt       SessionStatus = "WAITING_PROMPT"
	SessionSettling            SessionStatus = "SETTLING"
	SessionCompletionCandidate SessionStatus = "COMPLETION_CANDIDATE"
	SessionEndedStatus         SessionStatus = "ENDED"
	SessionCrashedStatus       SessionStatus = "CRASHED"
	SessionRecovering          SessionStatus = "RECOVERING"
)

type TaskStatus string

const (
	TaskCreated                   TaskStatus = "CREATED"
	TaskActive                    TaskStatus = "ACTIVE"
	TaskWaitingPrompt             TaskStatus = "WAITING_PROMPT"
	TaskSettling                  TaskStatus = "SETTLING"
	TaskCompletionCandidateStatus TaskStatus = "COMPLETION_CANDIDATE"
	TaskCompletedStatus           TaskStatus = "COMPLETED"
	TaskFailedStatus              TaskStatus = "FAILED"
	TaskCancelledStatus           TaskStatus = "CANCELLED"
)

type PromptKind string

const (
	PromptConsent      PromptKind = "consent"
	PromptVerification PromptKind = "verification"
	PromptRepair       PromptKind = "repair"
	PromptNotification PromptKind = "notification"
	PromptUnknown      PromptKind = "unknown"
)

type PromptStatus string

const (
	PromptRequestedStatus PromptStatus = "REQUESTED"
	PromptQueuedStatus    PromptStatus = "QUEUED"
	PromptPresentedStatus PromptStatus = "PRESENTED"
	PromptAnsweredStatus  PromptStatus = "ANSWERED"
	PromptExpiredStatus   PromptStatus = "EXPIRED"
	PromptCancelledStatus PromptStatus = "CANCELLED"
)

type ChangeStatus string

const (
	ChangeDetectedStatus    ChangeStatus = "DETECTED"
	ChangeReconciling       ChangeStatus = "RECONCILING"
	ChangeOwned             ChangeStatus = "OWNED"
	ChangeStagedStatus      ChangeStatus = "STAGED"
	ChangeBlocked           ChangeStatus = "BLOCKED"
	ChangeAmbiguous         ChangeStatus = "AMBIGUOUS"
	ChangeInvalidatedStatus ChangeStatus = "INVALIDATED"
)

type VerificationStatus string

const (
	VerificationRequestedStatus   VerificationStatus = "REQUESTED"
	VerificationRunning           VerificationStatus = "RUNNING"
	VerificationPassedStatus      VerificationStatus = "PASSED"
	VerificationFailedStatus      VerificationStatus = "FAILED"
	VerificationInvalidatedStatus VerificationStatus = "INVALIDATED"
)

type CommitStatus string

const (
	CommitRequestedStatus   CommitStatus = "COMMIT_REQUESTED"
	CommitQueued            CommitStatus = "QUEUED"
	CommitRunning           CommitStatus = "RUNNING"
	CommitCreatedStatus     CommitStatus = "CREATED"
	CommitFailedStatus      CommitStatus = "FAILED"
	CommitReconcileRequired CommitStatus = "RECONCILE_REQUIRED"
)

type PushStatus string

const (
	PushRequestedStatus PushStatus = "PUSH_REQUESTED"
	PushQueued          PushStatus = "QUEUED"
	PushRunning         PushStatus = "RUNNING"
	PushSucceededStatus PushStatus = "SUCCEEDED"
	PushRetryWait       PushStatus = "RETRY_WAIT"
	PushBlockedStatus   PushStatus = "BLOCKED"
	PushSkippedLocal    PushStatus = "SKIPPED_LOCAL"
)

type ReasonCode string

const (
	ReasonAccepted               ReasonCode = "ACCEPTED"
	ReasonDuplicate              ReasonCode = "DUPLICATE"
	ReasonPendingCausalGap       ReasonCode = "CAUSAL_GAP"
	ReasonInvalidTransition      ReasonCode = "E_INVALID_TRANSITION"
	ReasonIdempotencyConflict    ReasonCode = "E_IDEMPOTENCY_CONFLICT"
	ReasonWeakStop               ReasonCode = "WEAK_STOP"
	ReasonWeakCompletion         ReasonCode = "WEAK_COMPLETION"
	ReasonEvidenceInvalidated    ReasonCode = "EVIDENCE_INVALIDATED"
	ReasonWaitingForVerification ReasonCode = "WAITING_FOR_VERIFICATION"
	ReasonWaitingForPrompt       ReasonCode = "WAITING_FOR_PROMPT"
	ReasonReconcileRequired      ReasonCode = "RECONCILE_REQUIRED"
	ReasonGuardFailure           ReasonCode = "GUARD_FAILURE"
	ReasonLocalOnly              ReasonCode = "LOCAL_ONLY"
	ReasonCausalGap              ReasonCode = ReasonPendingCausalGap
	ReasonInvalid                ReasonCode = ReasonInvalidTransition
	ReasonEvidenceStale          ReasonCode = ReasonEvidenceInvalidated
)

type Disposition string

const (
	Accepted  Disposition = "accepted"
	Duplicate Disposition = "duplicate"
	Pending   Disposition = "pending"
	Rejected  Disposition = "rejected"
)

type Action string

const (
	ActionNone       Action = "none"
	ActionAskConsent Action = "ask_consent"
	ActionCheckpoint Action = "checkpoint"
	ActionVerify     Action = "verify"
	ActionCommit     Action = "commit"
	ActionPush       Action = "push"
	ActionNotify     Action = "notify"
	ActionBlocked    Action = "blocked"
)

type Capabilities struct {
	QueueState        string // native, none, unknown
	TaskBoundaries    string // native, synthetic
	ChangedPaths      string // reported, derived, none
	MonotonicSequence bool
}

const (
	QueueNative   = "native"
	QueueNone     = "none"
	QueueUnknown  = "unknown"
	TaskNative    = "native"
	TaskSynthetic = "synthetic"
)

// Payload contains only bounded facts. Raw prompt text, paths, source, and
type Payload struct {
	Status, Outcome, Reason                                     string
	PromptID                                                    string
	PromptKind                                                  PromptKind
	Blocking                                                    bool
	Answer                                                      string
	BaselineHead, BaselineIndex, StatusDigest                   string
	BaselinePathsDigest                                         string
	CandidateDigest, BaseDigest, TreeDigest, IndexDigest        string
	PolicyDigest, VerifierDigest, GuardDigest, MessageDigest    string
	EvidenceDigest, VerificationID                              string
	CommitJobID, CommitSHA                                      string
	PushJobID, RemoteDigest, Ref                                string
	ErrorCode                                                   string
	ExplicitComplete, CompletionEligible, ActiveTool, Ambiguous bool
	LocalOnly                                                   bool
	PublicConsent                                               bool
	Visibility                                                  string
	QueueState                                                  string
}

type Event struct {
	ID    string
	Type  EventType
	Class EventClass
	// Envelope-name aliases make it convenient to project the canonical event
	// without an intermediate allocation. Reduce normalizes them to ID/Type/Class.
	EventID                                              string
	EventType                                            EventType
	EventClass                                           EventClass
	RepoID, WorktreeID, SessionID, TaskID, ChangeID      string
	StreamID, CausationID, CorrelationID, IdempotencyKey string
	ProducerSeq                                          *int64
	Capabilities                                         Capabilities
	Payload                                              Payload
	// Digest carries the canonical envelope digest when the event came from
	// the ingress boundary. Direct reducer tests may leave it empty and use
	// the deterministic typed-event digest instead.
	Digest string
}

type Session struct {
	ID, RepositoryID                                               string
	State                                                          SessionStatus
	BaselineHead, BaselineIndex, StatusDigest, BaselinePathsDigest string
	Capabilities                                                   Capabilities
	ActiveTools                                                    int
	CompletionClaim, CompletionCandidate                           bool
	SettlingUntil                                                  int64
}
type Task struct {
	ID, SessionID                        string
	State                                TaskStatus
	ActiveTools                          int
	CompletionClaim, CompletionCandidate bool
}
type Prompt struct {
	ID, TaskID string
	Kind       PromptKind
	State      PromptStatus
	Blocking   bool
	Answer     string
}
type Candidate struct {
	ID, TaskID                                               string
	State                                                    ChangeStatus
	CandidateDigest, BaseDigest, TreeDigest, IndexDigest     string
	PolicyDigest, VerifierDigest, GuardDigest, MessageDigest string
	Ambiguous                                                bool
}
type Verification struct {
	ID, CandidateID                                                        string
	CandidateDigest, BaseDigest, PolicyDigest, VerifierDigest, GuardDigest string
	EvidenceDigest                                                         string
	State                                                                  VerificationStatus
	Reusable                                                               bool
}
type Commit struct {
	ID, CandidateID                                                                       string
	CandidateDigest, BaseDigest, PolicyDigest, VerifierDigest, GuardDigest, MessageDigest string
	CommitSHA                                                                             string
	State                                                                                 CommitStatus
}
type Push struct {
	ID, CommitID, RemoteDigest, Ref, CommitSHA string
	State                                      PushStatus
	LocalOnly                                  bool
}
type Quarantined struct {
	Event  Event
	Reason ReasonCode
}

type State struct {
	RepositoryID  string
	Revision      int64
	Policy        Policy
	Session       Session
	Tasks         map[string]Task
	Prompts       map[string]Prompt
	Candidates    map[string]Candidate
	Verifications map[string]Verification
	Commits       map[string]Commit
	Pushes        map[string]Push
	Receipts      map[string]string
	Idempotency   map[string]string
	Pending       map[string]Event
	Quarantine    []Quarantined
	LastSequence  map[string]int64
}

// Policy is the immutable policy snapshot to which candidate and verification
// evidence is bound.
type Policy struct {
	Decision, Visibility, Workflow, Digest string
	LocalOnly, PublicConsent               bool
	Revision                               int64
}

func NewState(repositoryID string) State {
	return State{RepositoryID: repositoryID, Tasks: map[string]Task{}, Prompts: map[string]Prompt{}, Candidates: map[string]Candidate{}, Verifications: map[string]Verification{}, Commits: map[string]Commit{}, Pushes: map[string]Push{}, Receipts: map[string]string{}, Idempotency: map[string]string{}, Pending: map[string]Event{}, LastSequence: map[string]int64{}}
}

func (s State) Clone() State {
	n := s
	n.Tasks = cloneMap(s.Tasks)
	n.Prompts = cloneMap(s.Prompts)
	n.Candidates = cloneMap(s.Candidates)
	n.Verifications = cloneMap(s.Verifications)
	n.Commits = cloneMap(s.Commits)
	n.Pushes = cloneMap(s.Pushes)
	n.Receipts = cloneMap(s.Receipts)
	n.Idempotency = cloneMap(s.Idempotency)
	n.Pending = cloneMap(s.Pending)
	n.LastSequence = cloneMap(s.LastSequence)
	n.Quarantine = append([]Quarantined(nil), s.Quarantine...)
	if n.Tasks == nil {
		n.Tasks = map[string]Task{}
	}
	if n.Prompts == nil {
		n.Prompts = map[string]Prompt{}
	}
	if n.Candidates == nil {
		n.Candidates = map[string]Candidate{}
	}
	if n.Verifications == nil {
		n.Verifications = map[string]Verification{}
	}
	if n.Commits == nil {
		n.Commits = map[string]Commit{}
	}
	if n.Pushes == nil {
		n.Pushes = map[string]Push{}
	}
	if n.Receipts == nil {
		n.Receipts = map[string]string{}
	}
	if n.Idempotency == nil {
		n.Idempotency = map[string]string{}
	}
	if n.Pending == nil {
		n.Pending = map[string]Event{}
	}
	if n.LastSequence == nil {
		n.LastSequence = map[string]int64{}
	}
	return n
}
func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type Config struct {
	Now          func() time.Time
	NewID        func(kind string) string
	SettlePeriod time.Duration
}
type Reducer struct{ config Config }

func NewReducer(config Config) Reducer { return Reducer{config: config} }
func (r Reducer) now() time.Time {
	if r.config.Now != nil {
		return r.config.Now()
	}
	return time.Unix(0, 0).UTC()
}
func (r Reducer) id(kind string, fallback string) string {
	if r.config.NewID != nil {
		if id := r.config.NewID(kind); id != "" {
			return id
		}
	}
	return fallback
}

type Result struct {
	Disposition Disposition
	Action      Action
	ReasonCode  ReasonCode
	Retryable   bool
	Revision    int64
	EventID     string
}

// Reduce returns a new state. Neither the input state nor the event is
// modified, making replay and crash recovery deterministic.
func (r Reducer) Reduce(state State, event Event) (State, Result) {
	s := state.Clone()
	if event.ID == "" {
		event.ID = event.EventID
	}
	if event.Type == "" {
		event.Type = event.EventType
	}
	if event.Class == "" {
		event.Class = event.EventClass
	}
	if event.Class == "" {
		event.Class = inferredClass(event.Type)
	}
	result := Result{Disposition: Accepted, Action: ActionNone, ReasonCode: ReasonAccepted, EventID: event.ID}
	if event.ID == "" || event.Type == "" {
		return r.reject(s, event, ReasonInvalidTransition)
	}
	if !classAllows(event.Class, event.Type) {
		return r.quarantine(s, event, ReasonInvalidTransition)
	}
	eventDigest := digest(event)
	if prior, ok := s.Receipts[event.ID]; ok {
		if prior == eventDigest {
			return s, Result{Disposition: Duplicate, ReasonCode: ReasonDuplicate, Action: ActionNone, Revision: s.Revision, EventID: event.ID}
		}
		return r.quarantine(s, event, ReasonIdempotencyConflict)
	}
	if event.IdempotencyKey != "" {
		if prior, ok := s.Idempotency[event.IdempotencyKey]; ok {
			if prior == eventDigest {
				return s, Result{Disposition: Duplicate, ReasonCode: ReasonDuplicate, Action: ActionNone, Revision: s.Revision, EventID: event.ID}
			}
			return r.quarantine(s, event, ReasonIdempotencyConflict)
		}
	}
	if prior, ok := s.Pending[event.ID]; ok {
		if digest(prior) == eventDigest {
			return s, Result{Disposition: Pending, Action: ActionNone, ReasonCode: ReasonPendingCausalGap, Revision: s.Revision, EventID: event.ID}
		}
		return r.quarantine(s, event, ReasonIdempotencyConflict)
	}
	if event.CausationID != "" {
		if _, ok := s.Receipts[event.CausationID]; !ok {
			s.Pending[event.ID] = event
			return s, Result{Disposition: Pending, Action: ActionNone, ReasonCode: ReasonPendingCausalGap, Revision: s.Revision, EventID: event.ID}
		}
	}
	if event.ProducerSeq != nil && (event.Capabilities.MonotonicSequence || s.Session.Capabilities.MonotonicSequence) {
		last := s.LastSequence[event.StreamID]
		if last > 0 && *event.ProducerSeq <= last {
			return r.quarantine(s, event, ReasonInvalidTransition)
		}
		if (*event.ProducerSeq > 1 && last == 0) || (last > 0 && *event.ProducerSeq > last+1) {
			s.Pending[event.ID] = event
			return s, Result{Disposition: Pending, Action: ActionNone, ReasonCode: ReasonPendingCausalGap, Revision: s.Revision, EventID: event.ID}
		}
	}
	if reason, ok := r.apply(&s, event); !ok {
		return r.quarantine(s, event, reason)
	} else if reason != ReasonAccepted {
		result.ReasonCode = reason
	}
	result.Action = actionFor(event.Type, result.ReasonCode)
	s.Receipts[event.ID] = eventDigest
	if event.IdempotencyKey != "" {
		s.Idempotency[event.IdempotencyKey] = eventDigest
	}
	if event.ProducerSeq != nil && *event.ProducerSeq > s.LastSequence[event.StreamID] {
		s.LastSequence[event.StreamID] = *event.ProducerSeq
	}
	s.Revision++
	result.Revision = s.Revision
	// Causal dependants are replayed in event-ID order, not wall-clock order.
	for {
		ids := make([]string, 0)
		for id, p := range s.Pending {
			if p.CausationID == event.ID || (p.ProducerSeq != nil && p.StreamID == event.StreamID && *p.ProducerSeq <= s.LastSequence[event.StreamID]+1) {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			break
		}
		sort.Strings(ids)
		progress := false
		for _, id := range ids {
			p := s.Pending[id]
			if p.CausationID != "" {
				if _, ok := s.Receipts[p.CausationID]; !ok {
					continue
				}
			}
			if p.ProducerSeq != nil && (p.Capabilities.MonotonicSequence || s.Session.Capabilities.MonotonicSequence) && s.LastSequence[p.StreamID] > 0 && *p.ProducerSeq > s.LastSequence[p.StreamID]+1 {
				continue
			}
			delete(s.Pending, id)
			if reason, ok := r.apply(&s, p); ok {
				pendingDigest := digest(p)
				s.Receipts[id] = pendingDigest
				if p.IdempotencyKey != "" {
					s.Idempotency[p.IdempotencyKey] = pendingDigest
				}
				s.Revision++
				if p.ProducerSeq != nil && *p.ProducerSeq > s.LastSequence[p.StreamID] {
					s.LastSequence[p.StreamID] = *p.ProducerSeq
				}
				if reason != ReasonAccepted {
					result.ReasonCode = reason
				}
				progress = true
			} else {
				pendingDigest := digest(p)
				s.Quarantine = append(s.Quarantine, Quarantined{Event: p, Reason: reason})
				s.Receipts[id] = pendingDigest
				if p.IdempotencyKey != "" {
					if _, exists := s.Idempotency[p.IdempotencyKey]; !exists {
						s.Idempotency[p.IdempotencyKey] = pendingDigest
					}
				}
				s.Revision++
				result.ReasonCode = reason
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	result.Revision = s.Revision
	return s, result
}

func actionFor(event EventType, reason ReasonCode) Action {
	if reason == ReasonEvidenceInvalidated || event == VerificationRequested || event == VerificationStarted {
		return ActionVerify
	}
	switch event {
	case PolicyConsentRequested:
		return ActionAskConsent
	case ChangeStaged:
		return ActionVerify
	case VerificationPassed:
		return ActionCommit
	case CommitCreated:
		return ActionPush
	case PushSucceeded:
		return ActionNotify
	case PromptRequested, PromptQueued, PromptPresented:
		return ActionNotify
	}
	return ActionNone
}

// ActionFor exposes the deterministic action mapping to application adapters
// without exposing persistence or side-effect implementations to the reducer.
func ActionFor(event EventType, reason ReasonCode) Action { return actionFor(event, reason) }

func inferredClass(t EventType) EventClass {
	switch t {
	case SessionStarted, SessionIdle, SessionEnded, PromptSubmitted, TaskStarted, TaskUpdated, TaskCompleted, TaskFailed, ToolStarted, ToolCompleted, FilesChanged, ModelStopped:
		return Ingress
	default:
		return Domain
	}
}

func classAllows(class EventClass, t EventType) bool {
	if t == TaskCompleted {
		// The same wire event is an ingress completion claim or a domain fact;
		// its class is what gives the event its authority.
		return class == Ingress || class == Domain
	}
	return (class == Ingress && inferredClass(t) == Ingress) || (class == Domain && inferredClass(t) == Domain)
}

func digest(e Event) string {
	if e.Digest != "" {
		return e.Digest
	}
	b, _ := json.Marshal(e)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func (r Reducer) reject(s State, e Event, reason ReasonCode) (State, Result) {
	return s, Result{Disposition: Rejected, Action: ActionBlocked, ReasonCode: reason, Revision: s.Revision, EventID: e.ID}
}
func (r Reducer) quarantine(s State, e Event, reason ReasonCode) (State, Result) {
	s.Quarantine = append(s.Quarantine, Quarantined{Event: e, Reason: reason})
	s.Receipts[e.ID] = digest(e)
	if e.IdempotencyKey != "" {
		if _, exists := s.Idempotency[e.IdempotencyKey]; !exists {
			s.Idempotency[e.IdempotencyKey] = digest(e)
		}
	}
	s.Revision++
	return r.reject(s, e, reason)
}

func (r Reducer) apply(s *State, e Event) (ReasonCode, bool) {
	if s.Session.ID == "" && e.SessionID != "" {
		s.Session = Session{ID: e.SessionID, RepositoryID: s.RepositoryID, State: SessionCreated}
	}
	if e.Capabilities != (Capabilities{}) && s.Session.Capabilities == (Capabilities{}) {
		s.Session.Capabilities = e.Capabilities
	}
	if e.SessionID != "" && s.Session.ID != "" && e.SessionID != s.Session.ID {
		return ReasonInvalidTransition, false
	}
	switch e.Type {
	case RepositoryDiscovered, PolicyConsentRequested:
		return ReasonAccepted, true
	case FilesChanged:
		for id := range s.Candidates {
			invalidateEvidence(s, id)
		}
		for id, t := range s.Tasks {
			t.CompletionCandidate = false
			if t.State == TaskCompletionCandidateStatus {
				t.State = TaskActive
			}
			s.Tasks[id] = t
		}
		if len(s.Candidates) > 0 {
			return ReasonEvidenceInvalidated, true
		}
		return ReasonAccepted, true
	case PolicySet:
		old := s.Policy
		s.Policy.Decision, s.Policy.Visibility, s.Policy.Workflow = e.Payload.Outcome, e.Payload.Visibility, e.Payload.Status
		s.Policy.LocalOnly = e.Payload.LocalOnly
		s.Policy.PublicConsent = e.Payload.PublicConsent || (e.Payload.Visibility == "public" && e.Payload.ExplicitComplete)
		s.Policy.Digest = e.Payload.PolicyDigest
		s.Policy.Revision++
		if old.Revision > 0 && (old.Digest != s.Policy.Digest || old.Decision != s.Policy.Decision || old.Visibility != s.Policy.Visibility || old.LocalOnly != s.Policy.LocalOnly) {
			for id := range s.Candidates {
				invalidateEvidence(s, id)
			}
			return ReasonEvidenceInvalidated, true
		}
		return ReasonAccepted, true
	case SessionStarted:
		s.Session.State = SessionActive
		s.Session.BaselineHead = e.Payload.BaselineHead
		s.Session.BaselineIndex = e.Payload.BaselineIndex
		s.Session.StatusDigest = e.Payload.StatusDigest
		s.Session.BaselinePathsDigest = e.Payload.BaselinePathsDigest
		if e.TaskID != "" {
			t := r.task(s, e)
			t.State = TaskActive
			s.Tasks[t.ID] = t
		}
		return ReasonAccepted, true
	case SessionIdle:
		if s.Session.State == SessionEndedStatus || s.Session.State == SessionCrashedStatus {
			return ReasonInvalidTransition, false
		}
		if s.Session.ActiveTools > 0 || hasBlockingPrompt(*s, "") {
			s.Session.State = SessionWaitingPrompt
		} else {
			s.Session.State = SessionSettling
		}
		s.Session.SettlingUntil = r.settleUntil()
		return ReasonAccepted, true
	case ModelStopped:
		if s.Session.State == SessionEndedStatus {
			return ReasonInvalidTransition, false
		}
		if s.Session.ActiveTools > 0 || hasBlockingPrompt(*s, "") {
			s.Session.State = SessionWaitingPrompt
		} else {
			s.Session.State = SessionSettling
			s.Session.SettlingUntil = r.settleUntil()
		}
		return ReasonWeakStop, true
	case SessionEnded:
		s.Session.State = SessionEndedStatus
		return ReasonAccepted, true
	case SessionCrashed:
		s.Session.State = SessionCrashedStatus
		return ReasonAccepted, true
	case SessionRecovered:
		if s.Session.State != SessionCrashedStatus {
			return ReasonInvalidTransition, false
		}
		s.Session.State = SessionActive
		return ReasonAccepted, true
	case TaskStarted:
		t := r.task(s, e)
		t.State = TaskActive
		s.Tasks[t.ID] = t
		s.Session.State = SessionActive
		return ReasonAccepted, true
	case TaskUpdated, ToolStarted, ToolCompleted:
		t, ok := s.Tasks[e.TaskID]
		if !ok {
			return ReasonInvalidTransition, false
		}
		if e.Type == ToolStarted {
			t.ActiveTools++
			s.Session.ActiveTools++
		}
		if e.Type == ToolCompleted && t.ActiveTools > 0 {
			t.ActiveTools--
			if s.Session.ActiveTools > 0 {
				s.Session.ActiveTools--
			}
		}
		s.Tasks[t.ID] = t
		return ReasonAccepted, true
	case TaskCompleted:
		t, ok := s.Tasks[e.TaskID]
		if !ok {
			return ReasonInvalidTransition, false
		}
		t.CompletionClaim = true
		s.Tasks[t.ID] = t
		if e.Class != Domain {
			if t.ActiveTools > 0 || hasBlockingPrompt(*s, t.ID) {
				t.State = TaskWaitingPrompt
				s.Tasks[t.ID] = t
				s.Session.State = SessionWaitingPrompt
			} else {
				s.Session.State = SessionSettling
				s.Session.SettlingUntil = r.settleUntil()
			}
			return ReasonWeakCompletion, true
		}
		if !completionEligible(*s, t) {
			return ReasonInvalidTransition, false
		}
		t.State = TaskCompletedStatus
		s.Tasks[t.ID] = t
		return ReasonAccepted, true
	case TaskCompletionCandidate:
		t, ok := s.Tasks[e.TaskID]
		if !ok {
			return ReasonInvalidTransition, false
		}
		if !t.CompletionClaim || t.ActiveTools > 0 || hasBlockingPrompt(*s, t.ID) {
			return ReasonInvalidTransition, false
		}
		t.CompletionCandidate = true
		t.State = TaskCompletionCandidateStatus
		s.Tasks[t.ID] = t
		s.Session.State = SessionCompletionCandidate
		return ReasonAccepted, true
	case TaskFailed:
		t, ok := s.Tasks[e.TaskID]
		if !ok {
			return ReasonInvalidTransition, false
		}
		t.State = TaskFailedStatus
		s.Tasks[t.ID] = t
		return ReasonAccepted, true
	case TaskCancelled:
		t, ok := s.Tasks[e.TaskID]
		if !ok {
			return ReasonInvalidTransition, false
		}
		t.State = TaskCancelledStatus
		s.Tasks[t.ID] = t
		return ReasonAccepted, true
	case PromptSubmitted, PromptRequested, PromptQueued, PromptPresented, PromptAnswered, PromptExpired, PromptCancelled:
		return r.applyPrompt(s, e)
	case ChangeDetected, ChangeStaged, ChangeInvalidated:
		return r.applyChange(s, e)
	case VerificationRequested, VerificationStarted, VerificationPassed, VerificationFailed, VerificationInvalidated:
		return r.applyVerification(s, e)
	case CommitRequested, CommitCreated, CommitFailed, CommitReconciled:
		return r.applyCommit(s, e)
	case PushRequested, PushSucceeded, PushFailed, PushSkipped:
		return r.applyPush(s, e)
	default:
		return ReasonInvalidTransition, false
	}
}

func (r Reducer) settleUntil() int64 {
	period := r.config.SettlePeriod
	if period < 0 {
		period = 0
	}
	return r.now().Add(period).UnixNano()
}

func (r Reducer) task(s *State, e Event) Task {
	id := e.TaskID
	if id == "" || (e.Capabilities.TaskBoundaries == TaskSynthetic && id == "") {
		id = r.id("task", "synthetic-task")
	}
	if t, ok := s.Tasks[id]; ok {
		return t
	}
	return Task{ID: id, SessionID: s.Session.ID, State: TaskCreated}
}
func completionEligible(s State, t Task) bool {
	return t.CompletionCandidate && completionConditions(s, t)
}

func completionConditions(s State, t Task) bool {
	if t.ActiveTools > 0 {
		return false
	}
	for _, p := range s.Prompts {
		if p.TaskID == t.ID && p.Blocking && (p.State == PromptQueuedStatus || p.State == PromptPresentedStatus) {
			return false
		}
	}
	return s.Session.Capabilities.QueueState == QueueNative || s.Session.Capabilities.QueueState == QueueNone
}

// CompletionEligible reports whether a previously claimed task has crossed
// the core-owned completion-candidate boundary. The claim itself is only an
// observation; queue and prompt/tool state must independently be safe.
func (s State) CompletionEligible(taskID string) bool {
	t, ok := s.Tasks[taskID]
	return ok && t.CompletionClaim && completionConditions(s, t)
}

// TaskCompleted reports whether the core has already recorded the durable
// completion fact for a task. It is used by replaying application boundaries
// to avoid re-running a local workflow for a duplicate ingress claim.
func (s State) TaskCompleted(taskID string) bool {
	t, ok := s.Tasks[taskID]
	return ok && t.State == TaskCompletedStatus
}

func hasBlockingPrompt(s State, taskID string) bool {
	for _, p := range s.Prompts {
		if (taskID == "" || p.TaskID == taskID) && p.Blocking && (p.State == PromptRequestedStatus || p.State == PromptQueuedStatus || p.State == PromptPresentedStatus) {
			return true
		}
	}
	return false
}

// PromptIdentity returns the stable queue identity specified by the event
// contract. The key is supplied by the state owner; no raw repository path is
// persisted by this package.
func PromptIdentity(key []byte, repositoryID, worktreeID, taskID string, kind PromptKind, candidateDigest, policyRevision string) string {
	m := hmac.New(sha256.New, key)
	for _, part := range []string{repositoryID, worktreeID, taskID, string(kind), candidateDigest, policyRevision} {
		_, _ = m.Write([]byte(part))
		_, _ = m.Write([]byte{0})
	}
	return "hmac-sha256:" + hex.EncodeToString(m.Sum(nil))
}

// DeterministicPromptID is a convenience for callers that keep their key as
// a string rather than a byte slice.
func DeterministicPromptID(key, repositoryID, worktreeID, taskID string, kind PromptKind, candidateDigest, policyRevision string) string {
	return PromptIdentity([]byte(key), repositoryID, worktreeID, taskID, kind, candidateDigest, policyRevision)
}

func (r Reducer) applyPrompt(s *State, e Event) (ReasonCode, bool) {
	id := e.Payload.PromptID
	if id == "" {
		return ReasonInvalidTransition, false
	}
	if e.TaskID == "" {
		t := r.task(s, e)
		if t.State == TaskCreated {
			t.State = TaskActive
			s.Tasks[t.ID] = t
		}
		e.TaskID = t.ID
	}
	p := s.Prompts[id]
	if p.ID == "" {
		if e.Type == PromptAnswered {
			return ReasonInvalidTransition, false
		}
		p = Prompt{ID: id, TaskID: e.TaskID, Kind: e.Payload.PromptKind, State: PromptRequestedStatus, Blocking: e.Payload.Blocking}
	}
	if p.TaskID == "" {
		p.TaskID = e.TaskID
	}
	if p.Kind == "" {
		p.Kind = e.Payload.PromptKind
	}
	switch e.Type {
	case PromptSubmitted:
		p.State = PromptQueuedStatus
		if e.Capabilities.QueueState == QueueNone {
			p.State = PromptPresentedStatus
		}
	case PromptRequested:
		p.State = PromptRequestedStatus
	case PromptQueued:
		p.State = PromptQueuedStatus
	case PromptPresented:
		p.State = PromptPresentedStatus
	case PromptAnswered:
		if p.State != PromptRequestedStatus && p.State != PromptQueuedStatus && p.State != PromptPresentedStatus {
			return ReasonInvalidTransition, false
		}
		p.State = PromptAnsweredStatus
		p.Answer = e.Payload.Answer
	case PromptExpired:
		p.State = PromptExpiredStatus
	case PromptCancelled:
		p.State = PromptCancelledStatus
	}
	s.Prompts[id] = p
	if t, ok := s.Tasks[p.TaskID]; ok && p.Blocking {
		t.CompletionCandidate = false
		if p.State == PromptQueuedStatus || p.State == PromptPresentedStatus {
			t.State = TaskWaitingPrompt
			s.Session.State = SessionWaitingPrompt
			s.Tasks[t.ID] = t
		} else if p.State == PromptRequestedStatus {
			t.State = TaskWaitingPrompt
			s.Session.State = SessionWaitingPrompt
			s.Tasks[t.ID] = t
		} else if p.State == PromptAnsweredStatus || p.State == PromptExpiredStatus || p.State == PromptCancelledStatus {
			if t.State == TaskWaitingPrompt {
				t.State = TaskActive
			}
			if s.Session.State == SessionWaitingPrompt {
				s.Session.State = SessionActive
			}
			s.Tasks[t.ID] = t
		}
	}
	return ReasonAccepted, true
}

func (r Reducer) applyChange(s *State, e Event) (ReasonCode, bool) {
	id := e.ChangeID
	if id == "" {
		return ReasonInvalidTransition, false
	}
	c := s.Candidates[id]
	before := c
	if c.ID == "" {
		c.ID = id
		c.TaskID = e.TaskID
	}
	c.CandidateDigest, c.BaseDigest, c.TreeDigest, c.IndexDigest = e.Payload.CandidateDigest, e.Payload.BaseDigest, e.Payload.TreeDigest, e.Payload.IndexDigest
	c.PolicyDigest, c.VerifierDigest, c.GuardDigest, c.MessageDigest = e.Payload.PolicyDigest, e.Payload.VerifierDigest, e.Payload.GuardDigest, e.Payload.MessageDigest
	c.Ambiguous = e.Payload.Ambiguous
	switch e.Type {
	case ChangeDetected:
		c.State = ChangeDetectedStatus
		if c.Ambiguous {
			c.State = ChangeAmbiguous
		}
	case ChangeStaged:
		c.State = ChangeStagedStatus
	case ChangeInvalidated:
		c.State = ChangeInvalidatedStatus
	}
	changed := before.CandidateDigest != "" && (before.CandidateDigest != c.CandidateDigest || before.BaseDigest != c.BaseDigest || before.TreeDigest != c.TreeDigest || before.IndexDigest != c.IndexDigest || before.PolicyDigest != c.PolicyDigest || before.VerifierDigest != c.VerifierDigest || before.GuardDigest != c.GuardDigest || before.MessageDigest != c.MessageDigest)
	s.Candidates[id] = c
	if changed || e.Type == ChangeInvalidated {
		invalidateEvidence(s, id)
		return ReasonEvidenceInvalidated, true
	}
	return ReasonAccepted, true
}
func invalidateEvidence(s *State, candidateID string) {
	for id, v := range s.Verifications {
		if v.CandidateID == candidateID {
			v.Reusable = false
			v.State = VerificationInvalidatedStatus
			s.Verifications[id] = v
		}
	}
	for id, c := range s.Commits {
		if c.CandidateID == candidateID && c.State != CommitCreatedStatus {
			c.State = CommitReconcileRequired
			s.Commits[id] = c
		}
	}
}

func (r Reducer) applyVerification(s *State, e Event) (ReasonCode, bool) {
	id := e.Payload.VerificationID
	if id == "" {
		id = e.ID
	}
	c, ok := s.Candidates[e.ChangeID]
	if !ok {
		return ReasonInvalidTransition, false
	}
	v := s.Verifications[id]
	if v.ID == "" {
		v = Verification{ID: id, CandidateID: e.ChangeID}
	}
	v.CandidateDigest, v.BaseDigest, v.PolicyDigest, v.VerifierDigest, v.GuardDigest, v.EvidenceDigest = e.Payload.CandidateDigest, e.Payload.BaseDigest, e.Payload.PolicyDigest, e.Payload.VerifierDigest, e.Payload.GuardDigest, e.Payload.EvidenceDigest
	switch e.Type {
	case VerificationRequested:
		v.State = VerificationRequestedStatus
	case VerificationStarted:
		v.State = VerificationRunning
	case VerificationPassed:
		if !exactEvidence(v.CandidateDigest, v.BaseDigest, v.PolicyDigest, v.VerifierDigest, v.GuardDigest, c) {
			return ReasonInvalidTransition, false
		}
		v.State = VerificationPassedStatus
		v.Reusable = true
	case VerificationFailed:
		v.State = VerificationFailedStatus
		v.Reusable = false
	case VerificationInvalidated:
		v.State = VerificationInvalidatedStatus
		v.Reusable = false
	}
	s.Verifications[id] = v
	if e.Type == VerificationPassed && !v.Reusable {
		return ReasonInvalidTransition, false
	}
	return ReasonAccepted, true
}

func (r Reducer) applyCommit(s *State, e Event) (ReasonCode, bool) {
	id := e.Payload.CommitJobID
	if id == "" {
		return ReasonInvalidTransition, false
	}
	c, ok := s.Candidates[e.ChangeID]
	if !ok {
		return ReasonInvalidTransition, false
	}
	j := s.Commits[id]
	if j.ID == "" {
		j = Commit{ID: id, CandidateID: e.ChangeID}
	}
	if e.Payload.CandidateDigest != "" && e.Payload.CandidateDigest != c.CandidateDigest {
		return ReasonInvalidTransition, false
	}
	j.CandidateDigest, j.BaseDigest, j.PolicyDigest, j.VerifierDigest, j.GuardDigest, j.MessageDigest = e.Payload.CandidateDigest, e.Payload.BaseDigest, e.Payload.PolicyDigest, e.Payload.VerifierDigest, e.Payload.GuardDigest, e.Payload.MessageDigest
	if j.CandidateDigest == "" {
		j.CandidateDigest = c.CandidateDigest
	}
	if j.BaseDigest == "" {
		j.BaseDigest = c.BaseDigest
	}
	if j.PolicyDigest == "" {
		j.PolicyDigest = c.PolicyDigest
	}
	if j.VerifierDigest == "" {
		j.VerifierDigest = c.VerifierDigest
	}
	if j.GuardDigest == "" {
		j.GuardDigest = c.GuardDigest
	}
	if j.MessageDigest == "" {
		j.MessageDigest = c.MessageDigest
	}
	switch e.Type {
	case CommitRequested:
		if !exactEvidence(j.CandidateDigest, j.BaseDigest, j.PolicyDigest, j.VerifierDigest, j.GuardDigest, c) || j.MessageDigest == "" || j.MessageDigest != c.MessageDigest {
			return ReasonInvalidTransition, false
		}
		if !hasReusableVerification(*s, c, j) {
			return ReasonWaitingForVerification, false
		}
		j.State = CommitQueued
	case CommitCreated, CommitReconciled:
		if j.State == "" || !exactEvidence(j.CandidateDigest, j.BaseDigest, j.PolicyDigest, j.VerifierDigest, j.GuardDigest, c) || j.MessageDigest == "" || j.MessageDigest != c.MessageDigest {
			return ReasonInvalidTransition, false
		}
		j.CommitSHA = e.Payload.CommitSHA
		if !validSHA(j.CommitSHA) {
			return ReasonInvalidTransition, false
		}
		j.State = CommitCreatedStatus
	case CommitFailed:
		j.State = CommitFailedStatus
	}
	s.Commits[id] = j
	return ReasonAccepted, true
}
func exactEvidence(candidateDigest, baseDigest, policyDigest, verifierDigest, guardDigest string, c Candidate) bool {
	return candidateDigest != "" && baseDigest != "" && policyDigest != "" && verifierDigest != "" && guardDigest != "" && candidateDigest == c.CandidateDigest && baseDigest == c.BaseDigest && policyDigest == c.PolicyDigest && verifierDigest == c.VerifierDigest && guardDigest == c.GuardDigest
}

func hasReusableVerification(s State, c Candidate, expected Commit) bool {
	for _, v := range s.Verifications {
		if v.CandidateID == c.ID && v.Reusable && v.State == VerificationPassedStatus && exactEvidence(v.CandidateDigest, v.BaseDigest, v.PolicyDigest, v.VerifierDigest, v.GuardDigest, c) && expected.CandidateDigest == v.CandidateDigest && expected.BaseDigest == v.BaseDigest && expected.PolicyDigest == v.PolicyDigest && expected.VerifierDigest == v.VerifierDigest && expected.GuardDigest == v.GuardDigest {
			return true
		}
	}
	return false
}

var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func validSHA(s string) bool { return shaRE.MatchString(s) }

func (r Reducer) applyPush(s *State, e Event) (ReasonCode, bool) {
	id := e.Payload.PushJobID
	if id == "" {
		return ReasonInvalidTransition, false
	}
	p := s.Pushes[id]
	if p.ID != "" && ((e.Payload.CommitSHA != "" && e.Payload.CommitSHA != p.CommitSHA) || (e.Payload.RemoteDigest != "" && e.Payload.RemoteDigest != p.RemoteDigest) || (e.Payload.Ref != "" && e.Payload.Ref != p.Ref)) {
		return ReasonInvalidTransition, false
	}
	if p.ID == "" {
		p = Push{ID: id, CommitID: e.Payload.CommitJobID, RemoteDigest: e.Payload.RemoteDigest, Ref: e.Payload.Ref, CommitSHA: e.Payload.CommitSHA, LocalOnly: e.Payload.LocalOnly}
	}
	if p.CommitSHA == "" {
		p.CommitSHA = e.Payload.CommitSHA
	}
	if !validSHA(p.CommitSHA) {
		return ReasonInvalidTransition, false
	}
	if e.Type == PushRequested {
		if !validDigest(p.RemoteDigest) || !validRef(p.Ref) {
			return ReasonInvalidTransition, false
		}
		if existing := s.Pushes[id]; existing.ID != "" && (existing.CommitSHA != p.CommitSHA || existing.RemoteDigest != p.RemoteDigest || existing.Ref != p.Ref) {
			return ReasonInvalidTransition, false
		}
		found := false
		for _, c := range s.Commits {
			if c.State == CommitCreatedStatus && c.CommitSHA == p.CommitSHA {
				found = true
				if p.CommitID == "" {
					p.CommitID = c.ID
				}
				break
			}
		}
		if !found {
			return ReasonInvalidTransition, false
		}
		p.State = PushQueued
	} else if e.Type == PushSucceeded {
		if p.State == "" || p.CommitSHA != e.Payload.CommitSHA || !validSHA(e.Payload.CommitSHA) {
			return ReasonInvalidTransition, false
		}
		p.State = PushSucceededStatus
	} else if e.Type == PushFailed {
		p.State = PushRetryWait
		if e.Payload.ErrorCode == "auth" || e.Payload.ErrorCode == "non-fast-forward" || e.Payload.ErrorCode == "unsafe" || e.Payload.ErrorCode == "collision" {
			p.State = PushBlockedStatus
		}
	} else {
		p.State = PushSkippedLocal
		p.LocalOnly = true
	}
	s.Pushes[id] = p
	return ReasonAccepted, true
}

var digestRE = regexp.MustCompile(`^(?:sha256|hmac-sha256):[0-9a-f]{64}$`)

func validDigest(s string) bool { return digestRE.MatchString(s) }
func validRef(s string) bool {
	return strings.HasPrefix(s, "refs/") && !strings.Contains(s, "..") && !strings.Contains(s, "//") && !strings.ContainsAny(s, "\x00\r\n") && len(s) > len("refs/")
}

var ErrInvalidTransition = errors.New("invalid lifecycle transition")
