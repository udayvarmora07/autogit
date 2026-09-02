// Package coordinator serializes durable side effects around intent records.
package coordinator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"autogit/internal/provider"
	"autogit/internal/state"
)

type CommitRequest struct{ ID, CandidateDigest, BaseSHA, MessageDigest, PolicyDigest, VerifierDigest, GuardDigest string }
type CommitEvidence struct{ CandidateDigest, BaseSHA, MessageDigest, PolicyDigest, VerifierDigest, GuardDigest string }
type PushRequest struct {
	ID, Owner, Name, Ref, CommitSHA string
	LocalOnly                       bool
}

// ConfirmPushOutcome is a typed remote postcondition. A missing ref permits
// one exact-SHA push; a present ref must already point to that exact SHA.
type ConfirmPushOutcome string

const (
	PushPresent  ConfirmPushOutcome = "present"
	PushMissing  ConfirmPushOutcome = "missing"
	PushConflict ConfirmPushOutcome = "conflict"
)

var ErrPushConflict = errors.New("remote ref points to a different commit")

type Store interface {
	PutCommitIntent(context.Context, CommitRequest) error
	CommitStatus(context.Context, string) (string, string, CommitRequest, error)
	RecordCommit(context.Context, string, string) error
	RecordReconcile(context.Context, string) error
	PutPushIntent(context.Context, PushRequest) error
	PushStatus(context.Context, string) (string, PushRequest, error)
	MarkPushSkipped(context.Context, string) error
	MarkPushSucceeded(context.Context, string) error
	MarkPushBlocked(context.Context, string) error
	MarkPushRetry(context.Context, string) error
}
type Git interface {
	Commit(context.Context, CommitRequest) (string, error)
	Inspect(context.Context, CommitRequest) (string, error)
}
type EvidenceInspector interface {
	InspectEvidence(context.Context, CommitRequest) (CommitEvidence, string, error)
}
type Provider interface {
	Push(context.Context, PushRequest) error
	ConfirmPush(context.Context, PushRequest) (ConfirmPushOutcome, error)
}
type Lease interface {
	Acquire(context.Context, string, string) error
	Release(context.Context, string, string) error
}
type Coordinator struct {
	Store    Store
	Git      Git
	Provider Provider
	Lease    Lease
	Owner    string
}

func (r CommitRequest) EvidenceMatches(e CommitEvidence) bool {
	return r.CandidateDigest == e.CandidateDigest && r.BaseSHA == e.BaseSHA && r.MessageDigest == e.MessageDigest && r.PolicyDigest == e.PolicyDigest && r.VerifierDigest == e.VerifierDigest && r.GuardDigest == e.GuardDigest
}
func (c Coordinator) CommitWithEvidence(ctx context.Context, r CommitRequest, e CommitEvidence) error {
	if !r.EvidenceMatches(e) {
		return errors.New("commit evidence mismatch")
	}
	return c.Commit(ctx, r)
}

// StateStore adapts the durable state package to the coordinator's narrow
// intent/result port. Keeping this adapter here prevents coordinators from
// reaching across package boundaries to SQL tables.
type StateStore struct{ DB *state.Store }

func NewStateStore(db *state.Store) *StateStore { return &StateStore{DB: db} }
func (s *StateStore) PutCommitIntent(ctx context.Context, r CommitRequest) error {
	return s.DB.WithTx(ctx, func(tx *state.Tx) error {
		return tx.PutCommitJob(state.CommitJob{ID: r.ID, CandidateDigest: r.CandidateDigest, BaseSHA: r.BaseSHA, MessageDigest: r.MessageDigest, PolicyDigest: r.PolicyDigest, VerifierDigest: r.VerifierDigest, GuardDigest: r.GuardDigest, State: state.CommitRequested})
	})
}
func (s *StateStore) CommitStatus(_ context.Context, id string) (string, string, CommitRequest, error) {
	j, e := s.DB.CommitJob(id)
	if errors.Is(e, sql.ErrNoRows) {
		return "", "", CommitRequest{}, nil
	}
	return j.State, j.CommitSHA, CommitRequest{ID: j.ID, CandidateDigest: j.CandidateDigest, BaseSHA: j.BaseSHA, MessageDigest: j.MessageDigest, PolicyDigest: j.PolicyDigest, VerifierDigest: j.VerifierDigest, GuardDigest: j.GuardDigest}, e
}
func (s *StateStore) RecordCommit(ctx context.Context, id, sha string) error {
	j, e := s.DB.CommitJob(id)
	if e != nil {
		return e
	}
	j.CommitSHA = sha
	j.State = state.CommitCreated
	return s.DB.WithTx(ctx, func(tx *state.Tx) error { return tx.PutCommitJob(j) })
}
func (s *StateStore) RecordReconcile(ctx context.Context, id string) error {
	j, e := s.DB.CommitJob(id)
	if e != nil {
		return e
	}
	j.State = "RECONCILE_REQUIRED"
	return s.DB.WithTx(ctx, func(tx *state.Tx) error { return tx.PutCommitJob(j) })
}
func (s *StateStore) PutPushIntent(ctx context.Context, r PushRequest) error {
	return s.DB.WithTx(ctx, func(tx *state.Tx) error {
		return tx.PutPushJob(state.PushJob{ID: r.ID, Owner: r.Owner, Name: r.Name, Ref: r.Ref, CommitSHA: r.CommitSHA, State: state.PushRequested, LocalOnly: r.LocalOnly})
	})
}
func (s *StateStore) MarkPushSkipped(ctx context.Context, id string) error {
	j, e := s.DB.PushJob(id)
	if e != nil {
		return e
	}
	j.State = state.PushSkippedLocal
	return s.DB.WithTx(ctx, func(tx *state.Tx) error { return tx.PutPushJob(j) })
}
func (s *StateStore) MarkPushSucceeded(ctx context.Context, id string) error {
	j, e := s.DB.PushJob(id)
	if e != nil {
		return e
	}
	j.State = state.PushSucceeded
	return s.DB.WithTx(ctx, func(tx *state.Tx) error { return tx.PutPushJob(j) })
}
func (s *StateStore) PushStatus(_ context.Context, id string) (string, PushRequest, error) {
	j, e := s.DB.PushJob(id)
	if errors.Is(e, sql.ErrNoRows) {
		return "", PushRequest{}, nil
	}
	return j.State, PushRequest{ID: j.ID, Owner: j.Owner, Name: j.Name, Ref: j.Ref, CommitSHA: j.CommitSHA, LocalOnly: j.LocalOnly}, e
}
func (s *StateStore) MarkPushBlocked(ctx context.Context, id string) error {
	j, e := s.DB.PushJob(id)
	if e != nil {
		return e
	}
	j.State = state.PushBlocked
	return s.DB.WithTx(ctx, func(tx *state.Tx) error { return tx.PutPushJob(j) })
}

func (s *StateStore) MarkPushRetry(ctx context.Context, id string) error {
	j, e := s.DB.PushJob(id)
	if e != nil {
		return e
	}
	j.State = state.PushRetryWait
	return s.DB.WithTx(ctx, func(tx *state.Tx) error { return tx.PutPushJob(j) })
}

func (c Coordinator) Commit(ctx context.Context, r CommitRequest) error {
	if c.Store == nil || c.Git == nil {
		return errors.New("commit coordinator dependencies missing")
	}
	if err := validateCommitRequest(r); err != nil {
		return err
	}
	status, sha, persisted, err := c.Store.CommitStatus(ctx, r.ID)
	if err != nil {
		return err
	}
	if status != "" && !r.EvidenceMatches(CommitEvidence{CandidateDigest: persisted.CandidateDigest, BaseSHA: persisted.BaseSHA, MessageDigest: persisted.MessageDigest, PolicyDigest: persisted.PolicyDigest, VerifierDigest: persisted.VerifierDigest, GuardDigest: persisted.GuardDigest}) {
		return errors.New("commit job identity conflict")
	}
	if status == "CREATED" && sha != "" {
		if shaRE.MatchString(sha) {
			return nil
		}
	}
	if status == "CREATED" {
		_ = c.Store.RecordReconcile(ctx, r.ID)
		return errors.New("created commit record has invalid SHA")
	}
	if status == "COMMIT_REQUESTED" || status == "RUNNING" {
		return c.RecoverCommit(ctx, r)
	}
	if c.Lease != nil {
		if err := c.Lease.Acquire(ctx, r.ID, c.Owner); err != nil {
			return err
		}
		defer c.Lease.Release(ctx, r.ID, c.Owner)
	}
	if err := c.Store.PutCommitIntent(ctx, r); err != nil {
		return err
	}
	sha, err = c.Git.Commit(ctx, r)
	if err != nil {
		return err
	}
	if !shaRE.MatchString(sha) {
		return errors.New("git returned invalid commit sha")
	}
	return c.Store.RecordCommit(ctx, r.ID, sha)
}
func (c Coordinator) RecoverCommit(ctx context.Context, r CommitRequest) error {
	if c.Store == nil || c.Git == nil {
		return errors.New("commit coordinator dependencies missing")
	}
	if err := validateCommitRequest(r); err != nil {
		return err
	}
	status, _, persisted, err := c.Store.CommitStatus(ctx, r.ID)
	if err != nil {
		return err
	}
	if status != "" && !r.EvidenceMatches(CommitEvidence{CandidateDigest: persisted.CandidateDigest, BaseSHA: persisted.BaseSHA, MessageDigest: persisted.MessageDigest, PolicyDigest: persisted.PolicyDigest, VerifierDigest: persisted.VerifierDigest, GuardDigest: persisted.GuardDigest}) {
		return errors.New("commit job identity conflict")
	}
	if status != "COMMIT_REQUESTED" && status != "RUNNING" {
		return nil
	}
	var evidence CommitEvidence
	var sha string
	if inspector, ok := c.Git.(EvidenceInspector); ok {
		evidence, sha, err = inspector.InspectEvidence(ctx, r)
		if err == nil && !r.EvidenceMatches(evidence) {
			err = errors.New("commit evidence mismatch")
		}
	} else {
		sha, err = c.Git.Inspect(ctx, r)
	}
	if err != nil {
		return c.Store.RecordReconcile(ctx, r.ID)
	}
	if !shaRE.MatchString(sha) {
		return c.Store.RecordReconcile(ctx, r.ID)
	}
	return c.Store.RecordCommit(ctx, r.ID, sha)
}
func (c Coordinator) Push(ctx context.Context, r PushRequest) error {
	if c.Store == nil {
		return errors.New("push coordinator store missing")
	}
	if r.ID == "" || !shaRE.MatchString(r.CommitSHA) {
		return errors.New("invalid push evidence")
	}
	status, persisted, err := c.Store.PushStatus(ctx, r.ID)
	if err != nil {
		return err
	}
	if status != "" && (persisted.Owner != r.Owner || persisted.Name != r.Name || persisted.Ref != r.Ref || persisted.CommitSHA != r.CommitSHA || persisted.LocalOnly != r.LocalOnly) {
		return errors.New("push job identity conflict")
	}
	if status == "SUCCEEDED" {
		return nil
	}
	if status == "BLOCKED" {
		return errors.New("push job is blocked")
	}
	if err := c.Store.PutPushIntent(ctx, r); err != nil {
		return err
	}
	if r.LocalOnly {
		return c.Store.MarkPushSkipped(ctx, r.ID)
	}
	if c.Provider == nil {
		return errors.New("push provider missing")
	}
	outcome, confirmErr := c.Provider.ConfirmPush(ctx, r)
	if outcome == PushConflict {
		cause := error(ErrPushConflict)
		if confirmErr != nil {
			cause = errors.Join(cause, confirmErr)
		}
		return c.recordPushBlocked(ctx, r.ID, cause)
	}
	if confirmErr != nil {
		return c.recordPushFailure(ctx, r.ID, confirmErr)
	}
	if confirmErr == nil && outcome == PushPresent {
		return c.Store.MarkPushSucceeded(ctx, r.ID)
	}
	if confirmErr == nil && outcome == PushConflict {
		return c.recordPushBlocked(ctx, r.ID, ErrPushConflict)
	}
	if confirmErr == nil && outcome != PushMissing {
		return c.recordPushFailure(ctx, r.ID, errors.New("invalid remote confirmation outcome"))
	}
	if err := c.Provider.Push(ctx, r); err != nil {
		return c.recordPushFailure(ctx, r.ID, err)
	}
	outcome, err = c.Provider.ConfirmPush(ctx, r)
	if outcome == PushConflict {
		cause := error(ErrPushConflict)
		if err != nil {
			cause = errors.Join(cause, err)
		}
		return c.recordPushBlocked(ctx, r.ID, cause)
	}
	if err != nil {
		return c.recordPushFailure(ctx, r.ID, err)
	}
	if outcome != PushPresent {
		return c.recordPushFailure(ctx, r.ID, errors.New("remote push postcondition not met"))
	}
	return c.Store.MarkPushSucceeded(ctx, r.ID)
}

// RetryPush resumes only a durable transient-failure intent. The stored
// request is passed back through Push so identity checks and exact-SHA
// postconditions remain mandatory on every retry.
func (c Coordinator) RetryPush(ctx context.Context, id string) error {
	if c.Store == nil || id == "" {
		return errors.New("push retry dependencies are missing")
	}
	status, request, err := c.Store.PushStatus(ctx, id)
	if err != nil {
		return err
	}
	if status != state.PushRetryWait {
		return fmt.Errorf("push job %q is not retryable", id)
	}
	return c.Push(ctx, request)
}

func (c Coordinator) recordPushFailure(ctx context.Context, id string, cause error) error {
	if provider.IsRetryable(cause) {
		if err := c.Store.MarkPushRetry(ctx, id); err != nil {
			return errors.Join(cause, err)
		}
		return cause
	}
	return c.recordPushBlocked(ctx, id, cause)
}

func (c Coordinator) recordPushBlocked(ctx context.Context, id string, cause error) error {
	if err := c.Store.MarkPushBlocked(ctx, id); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var digestRE = regexp.MustCompile(`^(?:sha256|hmac-sha256):[a-f0-9]{64}$`)

func validateCommitRequest(r CommitRequest) error {
	if r.ID == "" || !digestRE.MatchString(r.CandidateDigest) || !shaRE.MatchString(r.BaseSHA) || !digestRE.MatchString(r.MessageDigest) || !digestRE.MatchString(r.PolicyDigest) || !digestRE.MatchString(r.VerifierDigest) || !digestRE.MatchString(r.GuardDigest) {
		return fmt.Errorf("invalid canonical commit evidence")
	}
	return nil
}
