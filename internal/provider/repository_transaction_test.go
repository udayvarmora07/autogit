package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"autogit/internal/state"
)

type transactionBinder struct {
	url       string
	addErr    error
	inspect   int
	add       int
	addedURL  string
	addedName string
}

func (b *transactionBinder) RemoteURL(context.Context, string) (string, error) {
	b.inspect++
	if b.url == "" {
		return "", ErrRemoteAbsent
	}
	return b.url, nil
}

func (b *transactionBinder) AddRemote(_ context.Context, alias, url string) error {
	b.add++
	b.addedName, b.addedURL = alias, url
	if b.addErr != nil {
		return b.addErr
	}
	b.url = url
	return nil
}

type transactionProvider struct {
	created   int
	confirmed int
	createErr error
	identity  string
	seenState func() state.RemoteJob
}

func (p *transactionProvider) Create(_ context.Context, r RemoteRequest) (string, error) {
	p.created++
	if p.seenState != nil && p.seenState().State != state.RemoteRequested {
		return "", errors.New("remote intent was not durable before create")
	}
	if p.createErr != nil {
		return "", p.createErr
	}
	if p.identity != "" {
		return p.identity, nil
	}
	return r.Owner + "/" + r.Name, nil
}

func (p *transactionProvider) ConfirmRepository(context.Context, RemoteRequest) error {
	p.confirmed++
	return nil
}

func TestRepositoryTransactionCreatesThenAttachesExactRemote(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{}
	hosted := &transactionProvider{}
	hosted.seenState = func() state.RemoteJob { job, _ := db.RemoteJob("remote-1"); return job }
	tx := RepositoryTransaction{State: db, Hosted: hosted, Git: binder}
	got, err := tx.Create(context.Background(), RemoteCreateRequest{ID: "remote-1", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "owner/repo" || hosted.created != 1 || binder.add != 1 || binder.addedURL != "https://github.com/owner/repo.git" {
		t.Fatalf("result=%q provider=%+v binder=%+v", got, hosted, binder)
	}
	job, err := db.RemoteJob("remote-1")
	if err != nil || job.State != state.RemoteAttached || job.URL != binder.addedURL || job.Alias != "origin" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestRepositoryTransactionRejectsMismatchedExistingAliasWithoutCreation(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{url: "https://github.com/other/repo.git"}
	hosted := &transactionProvider{}
	_, err = (RepositoryTransaction{State: db, Hosted: hosted, Git: binder}).Create(context.Background(), RemoteCreateRequest{ID: "remote-2", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"})
	if !errors.Is(err, ErrRemoteBinding) || hosted.created != 0 || binder.add != 0 {
		t.Fatalf("err=%v provider=%d add=%d", err, hosted.created, binder.add)
	}
}

func TestRepositoryTransactionNeverAttachesAfterHostedCollision(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{}
	hosted := &transactionProvider{createErr: ErrCollision}
	_, err = (RepositoryTransaction{State: db, Hosted: hosted, Git: binder}).Create(context.Background(), RemoteCreateRequest{ID: "remote-3", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"})
	if !errors.Is(err, ErrCollision) || binder.add != 0 {
		t.Fatalf("err=%v add=%d", err, binder.add)
	}
	job, jobErr := db.RemoteJob("remote-3")
	if jobErr != nil || job.State != state.RemoteFailed {
		t.Fatalf("job=%+v err=%v", job, jobErr)
	}
}

func TestRepositoryTransactionExistingExactAliasConfirmsAndIsIdempotent(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{url: "https://github.com/owner/repo.git"}
	hosted := &transactionProvider{}
	got, err := (RepositoryTransaction{State: db, Hosted: hosted, Git: binder}).Create(context.Background(), RemoteCreateRequest{ID: "remote-4", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"})
	if err != nil || got != "owner/repo" || hosted.created != 0 || hosted.confirmed != 1 || binder.add != 0 {
		t.Fatalf("got=%q err=%v provider=%+v binder=%+v", got, err, hosted, binder)
	}
	job, jobErr := db.RemoteJob("remote-4")
	if jobErr != nil || job.State != state.RemoteAttached {
		t.Fatalf("job=%+v err=%v", job, jobErr)
	}
}

func TestRepositoryTransactionLeavesCreatedIntentForAttachRecovery(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{addErr: errors.New("local git failed")}
	hosted := &transactionProvider{}
	_, err = (RepositoryTransaction{State: db, Hosted: hosted, Git: binder}).Create(context.Background(), RemoteCreateRequest{ID: "remote-5", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"})
	if err == nil || !strings.Contains(err.Error(), "local git failed") {
		t.Fatalf("err=%v", err)
	}
	job, jobErr := db.RemoteJob("remote-5")
	if jobErr != nil || job.State != state.RemoteCreated {
		t.Fatalf("job=%+v err=%v", job, jobErr)
	}
}

func TestRepositoryTransactionResumesCreatedIntentWithoutRecreatingHostedRepo(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{addErr: errors.New("local git failed")}
	hosted := &transactionProvider{}
	tx := RepositoryTransaction{State: db, Hosted: hosted, Git: binder}
	req := RemoteCreateRequest{ID: "remote-6", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	if _, err := tx.Create(context.Background(), req); err == nil {
		t.Fatal("first attachment unexpectedly succeeded")
	}
	binder.addErr = nil
	got, err := tx.Create(context.Background(), req)
	if err != nil || got != "owner/repo" || hosted.created != 1 || binder.add != 2 {
		t.Fatalf("got=%q err=%v provider=%+v binder=%+v", got, err, hosted, binder)
	}
}
