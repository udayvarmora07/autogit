package provider

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autogit/internal/state"
)

type transactionBinder struct {
	url               string
	addErr            error
	addAfterEffectErr error
	inspect           int
	add               int
	addedURL          string
	addedName         string
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
	if b.addAfterEffectErr != nil {
		return b.addAfterEffectErr
	}
	return nil
}

type transactionProvider struct {
	created   int
	confirmed int
	createErr error
	identity  string
	exists    bool
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
		p.exists = true
		return p.identity, nil
	}
	p.exists = true
	return r.Owner + "/" + r.Name, nil
}

func (p *transactionProvider) ConfirmRepository(context.Context, RemoteRequest) error {
	p.confirmed++
	if !p.exists {
		return &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
	}
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
	hosted := &transactionProvider{exists: true}
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

func TestRepositoryTransactionRecoversAfterLocalAttachEffectFailure(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{addAfterEffectErr: errors.New("local attach response lost")}
	hosted := &transactionProvider{}
	tx := RepositoryTransaction{State: db, Hosted: hosted, Git: binder}
	req := RemoteCreateRequest{ID: "remote-attach-recovery", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	if _, err := tx.Create(context.Background(), req); err == nil || !strings.Contains(err.Error(), "local attach response lost") {
		t.Fatalf("first attach error=%v", err)
	}
	binder.addAfterEffectErr = nil
	if got, err := tx.Create(context.Background(), req); err != nil || got != "owner/repo" {
		t.Fatalf("attach recovery got=%q err=%v", got, err)
	}
	if hosted.created != 1 || binder.add != 1 {
		t.Fatalf("hosted creates=%d local adds=%d, want one each", hosted.created, binder.add)
	}
	job, err := db.RemoteJob(req.ID)
	if err != nil || job.State != state.RemoteAttached {
		t.Fatalf("recovered job=%+v err=%v", job, err)
	}
}

func TestRepositoryTransactionSerializesHostedCreationWithDurableLease(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hosted := &blockingTransactionProvider{entered: make(chan struct{}), release: make(chan struct{})}
	lease := &transactionLease{}
	req := RemoteCreateRequest{ID: "remote-concurrent", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	makeTransaction := func(owner string) RepositoryTransaction {
		return RepositoryTransaction{State: db, Hosted: hosted, Git: &transactionBinder{}, Lease: lease, Owner: owner}
	}
	first := makeTransaction("worker-a")
	second := makeTransaction("worker-b")
	firstErr := make(chan error, 1)
	go func() { _, createErr := first.Create(context.Background(), req); firstErr <- createErr }()
	<-hosted.entered
	secondErr := make(chan error, 1)
	go func() { _, createErr := second.Create(context.Background(), req); secondErr <- createErr }()
	select {
	case <-hosted.second:
		t.Fatal("second repository transaction reached hosted creation")
	case <-time.After(50 * time.Millisecond):
	}
	close(hosted.release)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if err := <-secondErr; err == nil || !strings.Contains(err.Error(), "lease held") {
		t.Fatalf("second error=%v, want lease contention", err)
	}
	if got := hosted.created.Load(); got != 1 {
		t.Fatalf("hosted create calls=%d, want one", got)
	}
}

func TestRepositoryTransactionSeparateStateStoresRecoverOneHostedCreate(t *testing.T) {
	statePath := t.TempDir() + "/state.db"
	firstDB, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer firstDB.Close()
	secondDB, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDB.Close()

	hosted := &blockingTransactionProvider{entered: make(chan struct{}), second: make(chan struct{}), release: make(chan struct{})}
	req := RemoteCreateRequest{ID: "remote-separate-store", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	first := RepositoryTransaction{State: firstDB, Hosted: hosted, Git: &transactionBinder{}, Lease: durableTransactionLease{db: firstDB}, Owner: "process-a"}
	second := RepositoryTransaction{State: secondDB, Hosted: hosted, Git: &transactionBinder{}, Lease: durableTransactionLease{db: secondDB}, Owner: "process-b"}
	firstErr := make(chan error, 1)
	go func() { _, createErr := first.Create(context.Background(), req); firstErr <- createErr }()
	<-hosted.entered
	secondErr := make(chan error, 1)
	go func() { _, createErr := second.Create(context.Background(), req); secondErr <- createErr }()
	select {
	case <-hosted.second:
		t.Fatal("second process reached hosted creation while first held the lease")
	case <-time.After(50 * time.Millisecond):
	}
	close(hosted.release)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if err := <-secondErr; err != nil && !strings.Contains(err.Error(), "lease held") {
		t.Fatal(err)
	}
	if got := hosted.created.Load(); got != 1 {
		t.Fatalf("hosted create calls=%d, want one across separate stores", got)
	}
	job, err := secondDB.RemoteJob(req.ID)
	if err != nil || job.State != state.RemoteAttached {
		t.Fatalf("restarted remote job=%+v err=%v", job, err)
	}
}

func TestRepositoryTransactionIntentPersistenceFailurePreventsHostedCreate(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("remote intent unavailable")
	store := &failingRemoteJobStore{putErr: failure}
	hosted := &transactionProvider{}
	tx := RepositoryTransaction{State: db, Jobs: store, Hosted: hosted, Git: &transactionBinder{}}
	req := RemoteCreateRequest{ID: "remote-intent-failure", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	if _, err := tx.Create(context.Background(), req); !errors.Is(err, failure) {
		t.Fatalf("error=%v, want intent persistence failure", err)
	}
	if hosted.created != 0 {
		t.Fatalf("hosted create calls=%d after intent failure", hosted.created)
	}
}

func TestRepositoryTransactionRecoversHostedCreateAfterResultPersistenceFailure(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &recoverableRemoteJobStore{failOn: 2, failErr: errors.New("remote result unavailable")}
	hosted := &recoverableHostedProvider{}
	tx := RepositoryTransaction{State: db, Jobs: store, Hosted: hosted, Git: &transactionBinder{}}
	req := RemoteCreateRequest{ID: "remote-result-failure", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	if _, err := tx.Create(context.Background(), req); !errors.Is(err, store.failErr) {
		t.Fatalf("error=%v, want result persistence failure", err)
	}
	if hosted.created != 1 {
		t.Fatalf("hosted create calls=%d after first attempt", hosted.created)
	}
	got, err := tx.Create(context.Background(), req)
	if err != nil || got != "owner/repo" || hosted.created != 1 {
		t.Fatalf("retry got=%q err=%v hosted creates=%d, want recovery without recreation", got, err, hosted.created)
	}
}

func TestRepositoryTransactionFailsClosedWhenIntentReadFailsBeforeHostedCreate(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("remote intent read unavailable")
	store := &readFailingRemoteJobStore{inner: stateRemoteJobStore{db: db}, failOn: 2, err: failure}
	hosted := &transactionProvider{}
	req := RemoteCreateRequest{ID: "remote-read-failure", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	tx := RepositoryTransaction{Jobs: store, Hosted: hosted, Git: &transactionBinder{}}
	if _, err := tx.Create(context.Background(), req); !errors.Is(err, failure) {
		t.Fatalf("error=%v, want durable intent read failure", err)
	}
	if hosted.created != 0 {
		t.Fatalf("hosted create calls=%d after intent read failure", hosted.created)
	}
}

func TestRepositoryTransactionFailsClosedWhenCreatedIntentLacksHostedIdentity(t *testing.T) {
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	req := RemoteCreateRequest{ID: "remote-missing-identity", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	if err := db.WithTx(context.Background(), func(tx *state.Tx) error {
		return tx.PutRemoteJob(state.RemoteJob{ID: req.ID, RepositoryID: req.RepositoryID, Alias: req.Alias, Owner: req.Owner, Name: req.Name, Visibility: req.Visibility, URL: "https://github.com/owner/repo.git", State: state.RemoteCreated})
	}); err != nil {
		t.Fatal(err)
	}
	hosted := &transactionProvider{}
	if _, err := (RepositoryTransaction{State: db, Hosted: hosted, Git: &transactionBinder{}}).Create(context.Background(), req); err == nil {
		t.Fatal("incomplete created intent unexpectedly triggered recovery")
	}
	if hosted.created != 0 {
		t.Fatalf("hosted create calls=%d for incomplete created intent", hosted.created)
	}
}

func TestRepositoryTransactionReopensStateAfterHostedCreateResultFailure(t *testing.T) {
	statePath := t.TempDir() + "/state.db"
	firstDB, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	store := &faultRemoteJobStore{inner: stateRemoteJobStore{db: firstDB}, failOn: 2, failErr: errors.New("remote result unavailable")}
	hosted := &recoverableHostedProvider{}
	req := RemoteCreateRequest{ID: "remote-restart", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
	first := RepositoryTransaction{Jobs: store, Hosted: hosted, Git: &transactionBinder{}}
	if _, err := first.Create(context.Background(), req); !errors.Is(err, store.failErr) {
		t.Fatalf("error=%v, want durable result failure", err)
	}
	if err := firstDB.Close(); err != nil {
		t.Fatal(err)
	}
	secondDB, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDB.Close()
	second := RepositoryTransaction{Jobs: stateRemoteJobStore{db: secondDB}, Hosted: hosted, Git: &transactionBinder{}}
	if got, err := second.Create(context.Background(), req); err != nil || got != "owner/repo" {
		t.Fatalf("restart recovery got=%q err=%v", got, err)
	}
	if hosted.created != 1 {
		t.Fatalf("hosted create calls=%d, want one after restart", hosted.created)
	}
}

type transactionLease struct {
	mu   sync.Mutex
	held bool
}

type durableTransactionLease struct{ db *state.Store }

func (l durableTransactionLease) Acquire(ctx context.Context, key, owner string) error {
	now := time.Now()
	return l.db.AcquireLease(ctx, state.Lease{Key: key, Owner: owner, ExpiresAt: now.Add(time.Minute).UnixNano()}, now.UnixNano())
}

func (l durableTransactionLease) Release(_ context.Context, key, owner string) error {
	return l.db.ReleaseLease(key, owner)
}

func (l *transactionLease) Acquire(context.Context, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		return errors.New("lease held")
	}
	l.held = true
	return nil
}

func (l *transactionLease) Release(context.Context, string, string) error {
	l.mu.Lock()
	l.held = false
	l.mu.Unlock()
	return nil
}

type blockingTransactionProvider struct {
	created atomic.Int32
	entered chan struct{}
	second  chan struct{}
	release chan struct{}
	exists  atomic.Bool
}

type failingRemoteJobStore struct {
	putErr error
}

func (s *failingRemoteJobStore) RemoteJob(string) (state.RemoteJob, error) {
	return state.RemoteJob{}, sql.ErrNoRows
}

func (s *failingRemoteJobStore) PutRemoteJob(context.Context, state.RemoteJob) error {
	return s.putErr
}

type recoverableRemoteJobStore struct {
	job     state.RemoteJob
	hasJob  bool
	puts    int
	failOn  int
	failErr error
}

type faultRemoteJobStore struct {
	inner   RemoteJobStore
	puts    int
	failOn  int
	failErr error
}

type readFailingRemoteJobStore struct {
	inner  RemoteJobStore
	reads  int
	failOn int
	err    error
}

func (s *readFailingRemoteJobStore) RemoteJob(id string) (state.RemoteJob, error) {
	s.reads++
	if s.reads == s.failOn {
		return state.RemoteJob{}, s.err
	}
	return s.inner.RemoteJob(id)
}

func (s *readFailingRemoteJobStore) PutRemoteJob(ctx context.Context, job state.RemoteJob) error {
	return s.inner.PutRemoteJob(ctx, job)
}

func (s *faultRemoteJobStore) RemoteJob(id string) (state.RemoteJob, error) {
	return s.inner.RemoteJob(id)
}

func (s *faultRemoteJobStore) PutRemoteJob(ctx context.Context, job state.RemoteJob) error {
	s.puts++
	if s.puts == s.failOn {
		return s.failErr
	}
	return s.inner.PutRemoteJob(ctx, job)
}

func (s *recoverableRemoteJobStore) RemoteJob(string) (state.RemoteJob, error) {
	if !s.hasJob {
		return state.RemoteJob{}, sql.ErrNoRows
	}
	return s.job, nil
}

func (s *recoverableRemoteJobStore) PutRemoteJob(_ context.Context, job state.RemoteJob) error {
	s.puts++
	if s.puts == s.failOn {
		return s.failErr
	}
	s.job, s.hasJob = job, true
	return nil
}

type recoverableHostedProvider struct {
	created int
	exists  bool
}

func (p *recoverableHostedProvider) Create(_ context.Context, r RemoteRequest) (string, error) {
	p.created++
	p.exists = true
	return r.Owner + "/" + r.Name, nil
}

func (p *recoverableHostedProvider) ConfirmRepository(context.Context, RemoteRequest) error {
	if !p.exists {
		return &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
	}
	return nil
}

func (p *blockingTransactionProvider) Create(_ context.Context, r RemoteRequest) (string, error) {
	count := p.created.Add(1)
	p.exists.Store(true)
	if count == 1 {
		close(p.entered)
		<-p.release
	} else if count == 2 {
		close(p.second)
	}
	return r.Owner + "/" + r.Name, nil
}

func (p *blockingTransactionProvider) ConfirmRepository(context.Context, RemoteRequest) error {
	if !p.exists.Load() {
		return &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
	}
	return nil
}
