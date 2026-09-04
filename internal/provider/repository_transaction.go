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
	Hosted HostedRepository
	Git    RemoteBinder
}

func (t RepositoryTransaction) Create(ctx context.Context, req RemoteCreateRequest) (string, error) {
	if t.State == nil || t.Hosted == nil || t.Git == nil {
		return "", errors.New("repository transaction dependencies are missing")
	}
	if !validRemoteCreateRequest(req) {
		return "", errors.New("invalid repository creation request")
	}
	identity := req.Owner + "/" + req.Name
	remoteURL := "https://github.com/" + identity + ".git"
	job, err := t.State.RemoteJob(req.ID)
	if err == nil {
		if job.RepositoryID != req.RepositoryID || job.Owner != req.Owner || job.Name != req.Name || job.Alias != req.Alias || job.Visibility != req.Visibility || job.URL != remoteURL {
			return "", errors.New("repository creation intent identity conflict")
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

	job, _ = t.State.RemoteJob(req.ID)
	hostedIdentity := job.HostedIdentity
	if job.State != state.RemoteCreated || hostedIdentity == "" {
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
	return t.State.WithTx(ctx, func(tx *state.Tx) error { return tx.PutRemoteJob(j) })
}

func (t RepositoryTransaction) markJob(ctx context.Context, req RemoteCreateRequest, status, hostedIdentity string) error {
	return t.putJob(ctx, state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Owner: req.Owner, Name: req.Name, Alias: req.Alias, Visibility: req.Visibility, URL: "https://github.com/" + req.Owner + "/" + req.Name + ".git", HostedIdentity: hostedIdentity, State: status})
}
