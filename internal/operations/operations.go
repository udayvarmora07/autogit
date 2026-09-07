// Package operations provides the durable, redacted operation UX used by the
// CLI and MCP adapters. It knows how to inspect AutoGit-owned effects without
// exposing the state database schema or raw repository paths.
package operations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"

	"autogit/internal/gitport"
	"autogit/internal/state"
)

type Snapshot struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	CommitSHA  string `json:"commit_sha,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Name       string `json:"name,omitempty"`
	Retryable  bool   `json:"retryable"`
	NextAction string `json:"next_action,omitempty"`
}

var ErrUnsupportedOperation = errors.New("operation is not resumable or cancellable")

func Inspect(ctx context.Context, store *state.Store, id string) (Snapshot, error) {
	if store == nil || strings.TrimSpace(id) == "" {
		return Snapshot{}, errors.New("operation identity is required")
	}
	if intent, err := store.GitCommitIntentRecord(ctx, id); err == nil {
		snapshot := Snapshot{ID: id, Kind: "commit", State: intent.State, Reason: intent.ReasonCode, CommitSHA: intent.SHA, Ref: intent.Intent.Ref}
		snapshot.NextAction = nextCommitAction(intent.State, intent.ReasonCode)
		return snapshot, nil
	}
	if job, err := store.CommitJobContext(ctx, id); err == nil {
		return Snapshot{ID: id, Kind: "commit", State: job.State, CommitSHA: job.CommitSHA, Retryable: job.State == state.CommitFailed, NextAction: nextCommitAction(job.State, "")}, nil
	}
	if job, err := store.PushJobContext(ctx, id); err == nil {
		return Snapshot{ID: id, Kind: "push", State: job.State, CommitSHA: job.CommitSHA, Owner: job.Owner, Name: job.Name, Ref: job.Ref, Retryable: job.State == state.PushRetryWait, NextAction: nextPushAction(job.State)}, nil
	}
	if job, err := store.RemoteJobContext(ctx, id); err == nil {
		return Snapshot{ID: id, Kind: "remote", State: job.State, Owner: job.Owner, Name: job.Name, Retryable: job.State == state.RemoteFailed, NextAction: nextRemoteAction(job.State)}, nil
	}
	return Snapshot{}, os.ErrNotExist
}

func nextCommitAction(status, reason string) string {
	if reason == "CANCELLED" {
		return "none"
	}
	switch status {
	case state.CommitIntentReconcile, state.CommitFailed:
		return "inspect repository and rerun with a new operation id"
	case state.CommitCreated:
		return "publish or inspect the AutoGit-owned commit"
	default:
		return "wait for durable reconciliation"
	}
}

func nextPushAction(status string) string {
	if status == state.PushRetryWait {
		return "autogit retry --id ID --repo DIR --remote REMOTE"
	}
	if status == state.PushSucceeded {
		return "none"
	}
	return "inspect provider identity and retry only after the recorded precondition is true"
}

func nextRemoteAction(status string) string {
	if status == state.RemoteFailed {
		return "rerun remote create with the same durable operation id"
	}
	return "none"
}

func Cancel(ctx context.Context, store *state.Store, id string) (Snapshot, error) {
	snapshot, err := Inspect(ctx, store, id)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Kind != "commit" || snapshot.CommitSHA != "" || snapshot.State == state.CommitCreated {
		return Snapshot{}, ErrUnsupportedOperation
	}
	if err := store.RecordGitCancel(ctx, id); err != nil {
		return Snapshot{}, err
	}
	return Inspect(ctx, store, id)
}

// UndoCommitRef deletes only the exact AutoGit-owned ref and only when it
// still names the recorded commit. It cannot rewrite a user branch or delete
// a hosted ref.
func UndoCommitRef(ctx context.Context, store *state.Store, repo, id, executable string) (Snapshot, error) {
	if repo == "" || executable == "" {
		return Snapshot{}, errors.New("repository and executable are required")
	}
	snapshot, err := Inspect(ctx, store, id)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Kind != "commit" || snapshot.CommitSHA == "" || snapshot.Ref != "refs/autogit/commits/"+id {
		return Snapshot{}, ErrUnsupportedOperation
	}
	runner := gitport.Runner{Executable: executable}
	actual, err := runner.Run(ctx, repo, "rev-parse", "--verify", snapshot.Ref)
	if err != nil || strings.TrimSpace(actual.Output) != snapshot.CommitSHA {
		return Snapshot{}, errors.New("AutoGit ref no longer names the recorded commit")
	}
	if _, err := runner.Run(ctx, repo, "update-ref", "-d", "--", snapshot.Ref, snapshot.CommitSHA); err != nil {
		return Snapshot{}, err
	}
	if err := store.RecordGitUndo(ctx, id); err != nil {
		return Snapshot{}, err
	}
	return Inspect(ctx, store, id)
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist)
}
