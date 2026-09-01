package gittransaction

import (
	"context"
	"errors"

	"autogit/internal/state"
)

// StateIntentPort adapts the durable state projection to the gittransaction
// IntentPort without coupling state to this package.
type StateIntentPort struct {
	store *state.Store
}

func NewStateIntentPort(store *state.Store) *StateIntentPort {
	return &StateIntentPort{store: store}
}

func (p *StateIntentPort) PutCommitIntent(ctx context.Context, i Intent) error {
	if p == nil || p.store == nil {
		return errors.New("state intent store is nil")
	}
	return p.store.WithTx(ctx, func(tx *state.Tx) error {
		return tx.PutGitCommitIntent(state.GitCommitIntent{
			ID: i.ID, RepoDir: i.RepoDir, Ref: i.Ref, ParentSHA: i.ParentSHA, TreeOID: i.TreeOID,
			Message: i.Message, CandidateDigest: i.CandidateDigest, MessageDigest: i.MessageDigest,
			SnapshotDigest: i.SnapshotDigest, PolicyDigest: i.PolicyDigest, VerifierDigest: i.VerifierDigest, GuardDigest: i.GuardDigest,
		})
	})
}

func (p *StateIntentPort) GetCommitIntent(ctx context.Context, id string) (Intent, error) {
	if p == nil || p.store == nil {
		return Intent{}, errors.New("state intent store is nil")
	}
	i, err := p.store.GitCommitIntent(ctx, id)
	if err != nil {
		return Intent{}, err
	}
	return Intent{ID: i.ID, RepoDir: i.RepoDir, Ref: i.Ref, ParentSHA: i.ParentSHA, TreeOID: i.TreeOID,
		Message: i.Message, CandidateDigest: i.CandidateDigest, MessageDigest: i.MessageDigest,
		SnapshotDigest: i.SnapshotDigest, PolicyDigest: i.PolicyDigest, VerifierDigest: i.VerifierDigest, GuardDigest: i.GuardDigest}, nil
}

func (p *StateIntentPort) RecordCommit(ctx context.Context, id, sha string) error {
	if p == nil || p.store == nil {
		return errors.New("state intent store is nil")
	}
	return p.store.RecordGitCommit(ctx, id, sha)
}

func (p *StateIntentPort) RecordReconcile(ctx context.Context, id, reason string) error {
	if p == nil || p.store == nil {
		return errors.New("state intent store is nil")
	}
	return p.store.RecordGitReconcile(ctx, id, reason)
}
