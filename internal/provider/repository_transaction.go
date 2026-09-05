package provider

import (
	"context"
	"database/sql"
	"errors"
	"regexp"

	"autogit/internal/state"
)

// RemoteCreateRequest is the immutable identity for one hosted-repository
// creation and local binding attempt.
type RemoteCreateRequest struct {
	ID, RepositoryID, Alias, Owner, Name, Visibility string
}

// RepositoryTransaction coordinates the two side effects in a recoverable
// order: durable intent, hosted creation, local attachment, exact local
// postcondition. It never removes a hosted repository after a local failure.
type RepositoryTransaction struct {
	State  *state.Store
	Jobs   RemoteJobStore
	Hosted HostedRepository
	Git    RemoteBinder
	Lease  Lease
	Owner  string
}

// RemoteJobStore is the durable intent boundary for hosted repository
// creation. Production uses the state-backed adapter below; tests and other
// callers can inject a failure-capable implementation without replacing the
// hosted or local side-effect ports.
type RemoteJobStore interface {
	RemoteJob(string) (state.RemoteJob, error)
	PutRemoteJob(context.Context, state.RemoteJob) error
}

type stateRemoteJobStore struct{ db *state.Store }

func (s stateRemoteJobStore) RemoteJob(id string) (state.RemoteJob, error) {
	return s.db.RemoteJob(id)
}

func (s stateRemoteJobStore) PutRemoteJob(ctx context.Context, job state.RemoteJob) error {
	return s.db.WithTx(ctx, func(tx *state.Tx) error { return tx.PutRemoteJob(job) })
}

// Lease serializes hosted-repository creation and local attachment for one
// immutable destination identity. It is optional for source compatibility with
// older in-process callers; production callers should always provide the
// durable state-backed lease.
type Lease interface {
	Acquire(context.Context, string, string) error
	Release(context.Context, string, string) error
}

func (t RepositoryTransaction) Create(ctx context.Context, req RemoteCreateRequest) (identity string, err error) {
	if (t.State == nil && t.Jobs == nil) || t.Hosted == nil || t.Git == nil {
		return "", errors.New("repository transaction dependencies are missing")
	}
	if !validRemoteCreateRequest(req) {
		return "", errors.New("invalid repository creation request")
	}
	identity = req.Owner + "/" + req.Name
	if t.Lease != nil {
		owner := t.Owner
		if owner == "" {
			owner = req.ID
		}
		leaseKey := "remote-create/" + req.RepositoryID + "/" + identity
		if err := t.Lease.Acquire(ctx, leaseKey, owner); err != nil {
			return "", err
		}
		defer func() {
			if releaseErr := t.Lease.Release(ctx, leaseKey, owner); releaseErr != nil && err == nil {
				err = releaseErr
				identity = ""
			}
		}()
	}
	remoteURL := "https://github.com/" + identity + ".git"
	jobs := t.remoteJobs()
	job, err := jobs.RemoteJob(req.ID)
	if err == nil {
		if job.RepositoryID != req.RepositoryID || job.Owner != req.Owner || job.Name != req.Name || job.Alias != req.Alias || job.Visibility != req.Visibility || job.URL != remoteURL {
			return "", errors.New("repository creation intent identity conflict")
		}
		if (job.State == state.RemoteCreated || job.State == state.RemoteAttached) && job.HostedIdentity == "" {
			return "", errors.New("repository creation intent lacks hosted identity")
		}
		if job.State == state.RemoteAttached {
			return identity, nil
		}
		if job.State == state.RemoteFailed {
			return "", errors.New("repository creation requires reconciliation")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	} else {
		job = state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: remoteURL, State: state.RemoteRequested}
		if err := t.putJob(ctx, job); err != nil {
			return "", err
		}
	}

	localURL, localErr := t.Git.RemoteURL(ctx, req.Alias)
	if localErr == nil {
		if !canonicalGitHubRemoteMatches(localURL, identity) {
			_ = t.putJob(ctx, state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: remoteURL, State: state.RemoteFailed})
			return "", remoteBindingError()
		}
		if err := t.Hosted.ConfirmRepository(ctx, RemoteRequest{Owner: req.Owner, Name: req.Name, Visibility: req.Visibility}); err != nil {
			_ = t.putJob(ctx, state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: remoteURL, State: state.RemoteFailed})
			return "", err
		}
		if err := t.markJob(ctx, req, state.RemoteAttached, identity); err != nil {
			return "", err
		}
		return identity, nil
	}
	if !errors.Is(localErr, ErrRemoteAbsent) {
		return "", localErr
	}

	job, err = jobs.RemoteJob(req.ID)
	if err != nil {
		// The intent was durably written above. A read failure here leaves the
		// hosted side effect unknown; creating again would risk a duplicate
		// repository. Retry/reconcile only after the durable record is readable.
		return "", err
	}
	hostedIdentity := job.HostedIdentity
	if job.State == state.RemoteRequested {
		// A crash can occur after hosted creation and before the durable
		// RemoteCreated result. Confirm the exact destination first so retry
		// reconciles an already-created repository instead of issuing a second
		// create operation.
		confirmErr := t.Hosted.ConfirmRepository(ctx, RemoteRequest{Owner: req.Owner, Name: req.Name, Visibility: req.Visibility})
		switch {
		case confirmErr == nil:
			hostedIdentity = identity
			if err := t.markJob(ctx, req, state.RemoteCreated, hostedIdentity); err != nil {
				return "", err
			}
		case !errors.Is(confirmErr, ErrRefAbsent):
			return "", confirmErr
		}
	}
	if hostedIdentity == "" {
		created, createErr := t.Hosted.Create(ctx, RemoteRequest{Owner: req.Owner, Name: req.Name, Visibility: req.Visibility})
		if createErr != nil {
			_ = t.putJob(ctx, state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: remoteURL, State: state.RemoteFailed})
			return "", createErr
		}
		if created != identity {
			_ = t.putJob(ctx, state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: remoteURL, HostedIdentity: created, State: state.RemoteFailed})
			return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
		}
		hostedIdentity = created
		if err := t.markJob(ctx, req, state.RemoteCreated, hostedIdentity); err != nil {
			return "", err
		}
	}
	if err := t.Git.AddRemote(ctx, req.Alias, remoteURL); err != nil {
		return "", err
	}
	attachedURL, err := t.Git.RemoteURL(ctx, req.Alias)
	if err != nil {
		return "", err
	}
	if !canonicalGitHubRemoteMatches(attachedURL, identity) {
		return "", remoteBindingError()
	}
	if err := t.markJob(ctx, req, state.RemoteAttached, hostedIdentity); err != nil {
		return "", err
	}
	return identity, nil
}

func validRemoteCreateRequest(req RemoteCreateRequest) bool {
	return validIdentity(RemoteRequest{Owner: req.Owner, Name: req.Name, Visibility: req.Visibility}) == nil && validGitRemoteAlias(req.Alias) && remoteJobIDRE.MatchString(req.ID) && remoteRepositoryIDRE.MatchString(req.RepositoryID)
}

var remoteJobIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var remoteRepositoryIDRE = regexp.MustCompile(`^(?:sha256|hmac-sha256):[a-f0-9]{64}$`)

func (t RepositoryTransaction) putJob(ctx context.Context, j state.RemoteJob) error {
	return t.remoteJobs().PutRemoteJob(ctx, j)
}

func (t RepositoryTransaction) markJob(ctx context.Context, req RemoteCreateRequest, status, hostedIdentity string) error {
	return t.putJob(ctx, state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: "https://github.com/" + req.Owner + "/" + req.Name + ".git", HostedIdentity: hostedIdentity, State: status})
}

func (t RepositoryTransaction) remoteJobs() RemoteJobStore {
	if t.Jobs != nil {
		return t.Jobs
	}
	return stateRemoteJobStore{db: t.State}
}
