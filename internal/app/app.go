package app

import (
	"autogit/internal/events"
	"autogit/internal/lifecycle"
	"autogit/internal/policy"
	"autogit/internal/repository"
	"autogit/internal/session"
	"autogit/internal/verification"
	localworkflow "autogit/internal/workflow"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
)

// Provider is deliberately narrow: implementations must verify exact remote
// identity and ref postconditions. The local-only path never receives a call.
type Provider interface {
	EnsureRemote(ctx context.Context, owner, name, visibility string) (string, error)
	Push(ctx context.Context, remote, commitSHA, ref string) error
}
type Result struct {
	SchemaVersion string `json:"schema_version"`
	EventID       string `json:"event_id,omitempty"`
	Disposition   string `json:"disposition"`
	StateRevision int64  `json:"state_revision,omitempty"`
	Action        string `json:"action"`
	ReasonCode    string `json:"reason_code"`
	Retryable     bool   `json:"retryable,omitempty"`
}

func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result
	a := alias(r)
	if a.SchemaVersion == "" {
		a.SchemaVersion = "autogit.result/1"
	}
	return json.Marshal(a)
}

type App struct {
	Store           *events.Store
	Policy          policy.Policy
	Provider        Provider
	Resolver        Resolver
	Reducer         lifecycle.Reducer
	Baselines       *session.Service
	SessionWorkflow session.Workflow
	IdentityKey     []byte
	Completion      *CompletionProfile
}

// CompletionProfile is the explicit, trusted selection for lifecycle-driven
// local completion. Message and verifier selection are configuration, never
// inferred from an adapter claim. Load reconstructs the source-free session
// handoff after a hook process restart; Workflow remains the verified local
// mutation boundary.
type CompletionProfile struct {
	Message   string
	Verifiers *verification.VerifierRegistry
	Workflow  session.Workflow
	Baselines *session.Service
	Load      func(context.Context, session.Request) (session.Started, error)
}

// CaptureSessionBaseline is the application boundary for the session
// coordinator. It intentionally does not accept raw client facts; the
// session service captures from the trusted repository observation port.
func (a *App) CaptureSessionBaseline(ctx context.Context, req session.Request) (repository.Baseline, error) {
	if a == nil || a.Baselines == nil {
		return repository.Baseline{}, errors.New("session baseline service is not configured")
	}
	return a.Baselines.CaptureAndRecord(ctx, req)
}

// CompleteSession is the application boundary for session-owned local work.
// It accepts only the in-memory baseline handoff and a workflow port; callers
// cannot supply a raw Git transaction that skips scanning or verification.
func (a *App) CompleteSession(ctx context.Context, started session.Started, id, message string, p policy.Policy, verifiers *verification.VerifierRegistry) (localworkflow.Result, error) {
	if a == nil || a.Baselines == nil || a.SessionWorkflow == nil {
		return localworkflow.Result{}, errors.New("session completion services are not configured")
	}
	return a.Baselines.Complete(ctx, started, a.SessionWorkflow, id, message, p, verifiers)
}

type Resolver func(string) (repository.Info, error)

func New(s *events.Store, p policy.Policy, provider Provider) *App {
	return &App{Store: s, Policy: p, Provider: provider, Reducer: lifecycle.NewReducer(lifecycle.Config{})}
}
func (a *App) Hook(ctx context.Context, input []byte) (Result, error) {
	return a.hook(ctx, input, false)
}

// ApplyDomain is reserved for core/reconciler callers that already crossed
// the internal domain-fact boundary. External adapter hooks must use Hook and
// cannot submit authoritative domain events.
func (a *App) ApplyDomain(ctx context.Context, input []byte) (Result, error) {
	return a.hook(ctx, input, true)
}

func (a *App) hook(ctx context.Context, input []byte, allowDomain bool) (Result, error) {
	if err := policy.Validate(a.Policy); err != nil {
		return Result{}, &events.Error{Code: "E_POLICY", Message: "invalid effective policy"}
	}
	e, err := events.Decode(input, 64<<10)
	if err != nil {
		return Result{}, err
	}
	if !allowDomain && e.EventClass != "ingress" {
		return Result{}, &events.Error{Code: "E_EVENT_CLASS", Message: "external hooks accept ingress events only"}
	}
	// project is ephemeral ingress context. Resolve and compare it before the
	// receipt transaction; it is never written to durable state.
	var candidateRoot string
	if e.Project != nil {
		candidateRoot, _ = e.Project["candidate_root"].(string)
		candidate := candidateRoot
		if candidate == "" {
			return Result{}, &events.Error{Code: "E_SCOPE", Message: "event project root is required"}
		}
		resolve := a.Resolver
		if resolve == nil {
			return Result{}, &events.Error{Code: "E_SCOPE", Message: "trusted repository resolver is not configured"}
		}
		info, resolveErr := resolve(candidate)
		if resolveErr != nil || info.RepoID != stringValue(e.Scope["repo_id"]) || info.WorktreeID == "" || info.WorktreeID != stringValue(e.Scope["worktree_id"]) {
			return Result{}, &events.Error{Code: "E_SCOPE", Message: "event project does not match repository identity"}
		}
	}
	// A session baseline is a read-only repository observation, but it must be
	// captured before the ingress receipt is accepted. Otherwise a successful
	// session.started fact could outlive the baseline needed to attribute later
	// changes safely.
	if e.EventType == "session.started" && a.Baselines != nil {
		if candidateRoot == "" {
			return Result{}, &events.Error{Code: "E_SCOPE", Message: "session baseline requires an event project root"}
		}
		if _, err := a.CaptureSessionBaseline(ctx, session.Request{
			SessionID:    stringValue(e.Scope["session_id"]),
			RepositoryID: stringValue(e.Scope["repo_id"]),
			ClientID:     stringValue(e.Producer["adapter"]),
			Root:         candidateRoot,
			IdentityKey:  a.IdentityKey,
		}); err != nil {
			return Result{}, &events.Error{Code: "E_REPOSITORY", Message: "session baseline capture failed"}
		}
	}
	r, err := a.Store.AcceptAndProject(ctx, e, a.project)
	if err != nil {
		return Result{}, err
	}
	out := Result{SchemaVersion: "autogit.result/1", EventID: e.EventID, Disposition: string(r.Disposition), StateRevision: r.StateRevision, Action: "none"}
	completionCandidatePromoted := false
	if r.Disposition == events.Pending {
		out.Action = "none"
		out.ReasonCode = "CAUSAL_GAP"
		return out, nil
	}
	if r.Disposition == events.Duplicate {
		out.ReasonCode = "DUPLICATE"
	} else if r.Disposition == events.Rejected {
		out.Action = "blocked"
		out.ReasonCode = r.Reason
		return out, nil
	} else {
		out.ReasonCode = r.Reason
		if out.ReasonCode == "" {
			out.ReasonCode = string(lifecycle.ReasonAccepted)
		}
		out.Action = string(lifecycle.ActionFor(lifecycle.EventType(e.EventType), lifecycle.ReasonCode(out.ReasonCode)))
	}
	if (r.Disposition == events.Accepted || r.Disposition == events.Duplicate) && e.EventClass == "ingress" && e.EventType == string(lifecycle.TaskCompleted) {
		promoted, candidateResult, promoteErr := a.promoteCompletionCandidate(ctx, e)
		if promoteErr != nil {
			return Result{}, promoteErr
		}
		if promoted {
			completionCandidatePromoted = true
			out.StateRevision = candidateResult.StateRevision
			out.Action = string(lifecycle.ActionCheckpoint)
			out.ReasonCode = "COMPLETION_CANDIDATE"
		}
	}
	if a.Policy.Tracking == "" {
		out.Action = "ask_consent"
		out.ReasonCode = "CONSENT_REQUIRED"
		return out, nil
	}
	if a.Policy.Tracking == "no" {
		out.ReasonCode = "TRACKING_DISABLED"
		return out, nil
	}
	if a.Policy.LocalOnly && !completionCandidatePromoted {
		out.ReasonCode = "LOCAL_ONLY"
		return out, nil
	}
	if completionCandidatePromoted {
		completion, completionErr := a.completeLifecycle(ctx, e)
		if completionErr != nil {
			out.Action = string(lifecycle.ActionBlocked)
			out.ReasonCode = "SESSION_COMPLETION_BLOCKED"
			return out, nil
		}
		if completion {
			out.Action = string(lifecycle.ActionCommit)
			out.ReasonCode = "SESSION_COMMITTED"
		}
	}
	return out, nil
}

func (a *App) completeLifecycle(ctx context.Context, ingress events.Event) (bool, error) {
	profile := a.Completion
	if profile == nil {
		return false, nil
	}
	if profile.Workflow == nil || profile.Load == nil || profile.Message == "" || profile.Verifiers == nil {
		return false, errors.New("lifecycle completion profile is incomplete")
	}
	repoID := stringValue(ingress.Scope["repo_id"])
	taskID := stringValue(ingress.Scope["task_id"])
	sessionID := stringValue(ingress.Scope["session_id"])
	data, _, err := a.Store.LifecycleProjection(repoID)
	if err != nil {
		return false, err
	}
	var projected lifecycle.State
	if err := json.Unmarshal(data, &projected); err != nil || projected.RepositoryID != repoID {
		return false, errors.New("invalid lifecycle projection")
	}
	if projected.TaskCompleted(taskID) {
		return false, nil
	}
	root := ""
	if ingress.Project != nil {
		root = stringValue(ingress.Project["candidate_root"])
	}
	started, err := profile.Load(ctx, session.Request{
		SessionID: sessionID, RepositoryID: repoID, ClientID: stringValue(ingress.Producer["adapter"]), Root: root,
		IdentityKey: append([]byte(nil), a.IdentityKey...),
	})
	if err != nil {
		return false, err
	}
	if !completionScopeMatches(ingress, started) {
		return false, errors.New("lifecycle session handoff does not match completion event")
	}
	baselines := profile.Baselines
	if baselines == nil {
		baselines = a.Baselines
	}
	if baselines == nil {
		return false, errors.New("lifecycle session baseline service is missing")
	}
	jobID := lifecycleCompletionID(repoID, stringValue(ingress.Scope["worktree_id"]), sessionID, taskID)
	if _, err := baselines.Complete(ctx, started, profile.Workflow, jobID, profile.Message, a.Policy, profile.Verifiers); err != nil {
		return false, err
	}
	completed, err := events.NewDomainEvent(events.DomainEventRequest{
		EventType: string(lifecycle.TaskCompleted), OccurredAt: ingress.OccurredAt,
		RepoID: repoID, WorktreeID: stringValue(ingress.Scope["worktree_id"]), SessionID: sessionID, TaskID: taskID,
		CorrelationID: "completion/" + jobID, CausationID: ingress.EventID, IdempotencyKey: "session-completed/" + jobID,
		Payload: map[string]any{"outcome": "success"},
	})
	if err != nil {
		return false, err
	}
	result, err := a.ApplyDomain(ctx, completed)
	if err != nil {
		return false, err
	}
	if result.Disposition != string(events.Accepted) && result.Disposition != string(events.Duplicate) {
		return false, fmt.Errorf("session completion fact was %s", result.Disposition)
	}
	return true, nil
}

func completionScopeMatches(ingress events.Event, started session.Started) bool {
	req := started.Request
	if req.SessionID != stringValue(ingress.Scope["session_id"]) || req.RepositoryID != stringValue(ingress.Scope["repo_id"]) || req.ClientID != stringValue(ingress.Producer["adapter"]) || req.Root == "" {
		return false
	}
	if ingress.Project == nil {
		return false
	}
	root := stringValue(ingress.Project["candidate_root"])
	if root == "" || req.Root != root {
		return false
	}
	return true
}

func lifecycleCompletionID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "completion-" + fmt.Sprintf("%x", h.Sum(nil))
}

func (a *App) promoteCompletionCandidate(ctx context.Context, ingress events.Event) (bool, Result, error) {
	repoID := stringValue(ingress.Scope["repo_id"])
	taskID := stringValue(ingress.Scope["task_id"])
	if a.Store == nil || taskID == "" || repoID == "" {
		return false, Result{}, nil
	}
	data, _, err := a.Store.LifecycleProjection(repoID)
	if err != nil {
		return false, Result{}, err
	}
	var projected lifecycle.State
	if err := json.Unmarshal(data, &projected); err != nil || projected.RepositoryID != repoID || !projected.CompletionEligible(taskID) {
		return false, Result{}, nil
	}
	correlation := stringValue(ingress.Ordering["correlation_id"])
	if correlation == "" {
		correlation = "completion/" + ingress.EventID
	}
	b, err := events.NewDomainEvent(events.DomainEventRequest{
		EventType: "task.completion_candidate", OccurredAt: ingress.OccurredAt,
		RepoID: repoID, WorktreeID: stringValue(ingress.Scope["worktree_id"]), SessionID: stringValue(ingress.Scope["session_id"]), TaskID: taskID,
		CorrelationID: correlation, CausationID: ingress.EventID, IdempotencyKey: "completion-candidate/" + ingress.EventID,
		Payload: map[string]any{"completion_eligible": true, "queue_state": projected.Session.Capabilities.QueueState},
	})
	if err != nil {
		return false, Result{}, err
	}
	result, err := a.ApplyDomain(ctx, b)
	if err != nil {
		return false, Result{}, err
	}
	return result.Disposition == string(events.Accepted) || result.Disposition == string(events.Duplicate), result, nil
}

// project is the application adapter for the pure lifecycle reducer. The
// events package owns the transaction and passes only opaque bytes here,
// keeping the dependency direction one-way.
func (a *App) project(current []byte, canonical events.Event) (events.ProjectionResult, error) {
	repoID := stringValue(canonical.Scope["repo_id"])
	state := lifecycle.NewState(repoID)
	if len(current) != 0 {
		if err := json.Unmarshal(current, &state); err != nil {
			return events.ProjectionResult{}, errors.New("invalid lifecycle projection")
		}
		if state.RepositoryID != repoID {
			return events.ProjectionResult{}, errors.New("lifecycle projection repository mismatch")
		}
	}
	before := state
	next, reduced := a.Reducer.ReduceCanonical(state, canonical)
	next = bounded(next)
	data, err := json.Marshal(next)
	if err != nil {
		return events.ProjectionResult{}, err
	}
	if len(data) > 1<<20 {
		return events.ProjectionResult{}, errors.New("lifecycle projection exceeds size limit")
	}
	result := events.ProjectionResult{Data: data, Reason: string(reduced.ReasonCode), Revision: reduced.Revision}
	switch reduced.Disposition {
	case lifecycle.Accepted:
		result.Disposition = events.Accepted
	case lifecycle.Pending:
		result.Disposition = events.Pending
	case lifecycle.Rejected:
		result.Disposition = events.Rejected
	default:
		return events.ProjectionResult{}, errors.New("unknown lifecycle disposition")
	}
	le := lifecycle.FromCanonical(canonical)
	if reduced.Disposition != lifecycle.Pending {
		result.Receipts = append(result.Receipts, events.ProjectionReceipt{EventID: le.ID, Digest: canonical.Digest, Disposition: result.Disposition})
	}
	for id, pending := range before.Pending {
		if _, promoted := next.Receipts[id]; !promoted {
			continue
		}
		disposition := events.Accepted
		for _, q := range next.Quarantine {
			if q.Event.ID == id {
				disposition = events.Rejected
				break
			}
		}
		result.Receipts = append(result.Receipts, events.ProjectionReceipt{EventID: id, Digest: pending.Digest, Disposition: disposition})
	}
	return result, nil
}

// bounded strips answer text from durable state, including buffered and
// quarantined facts. Prompt identity and status remain available for replay,
// while potentially sensitive user content never enters the projection.
func bounded(s lifecycle.State) lifecycle.State {
	n := s.Clone()
	for id, p := range n.Prompts {
		p.Answer = ""
		n.Prompts[id] = p
	}
	for id, e := range n.Pending {
		e.Payload.Answer = ""
		e.Payload.Reason = ""
		n.Pending[id] = e
	}
	for i := range n.Quarantine {
		n.Quarantine[i].Event.Payload.Answer = ""
		n.Quarantine[i].Event.Payload.Reason = ""
	}
	return n
}

func stringValue(v any) string { s, _ := v.(string); return s }
