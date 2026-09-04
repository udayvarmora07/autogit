package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autogit/internal/provider"
	"autogit/internal/state"
)

func TestCommitIntentPrecedesEffectAndRecoveryAvoidsDuplicate(t *testing.T) {
	s := newMemoryStore()
	g := &fakeGit{sha: "0123456789abcdef0123456789abcdef01234567", trace: s.trace}
	c := Coordinator{Store: s, Git: g}
	req := validCommitRequest("job-1")
	if err := c.Commit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(s.intents) != 1 || g.calls != 1 {
		t.Fatalf("intents=%d git=%d", len(s.intents), g.calls)
	}
	if got, want := strings.Join(*s.trace, ","), "persist_intent,git_effect,persist_result"; got != want {
		t.Fatalf("trace=%s", got)
	}
	if err := c.RecoverCommit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if g.calls != 1 {
		t.Fatal("recovery repeated commit")
	}
}

func TestCommitReportsLeaseReleaseFailureAfterCommitOutcome(t *testing.T) {
	s := newMemoryStore()
	lease := &recordingLease{releaseErr: errors.New("lease release failed")}
	g := &fakeGit{sha: "0123456789abcdef0123456789abcdef01234567", trace: s.trace}
	r := validCommitRequest("lease-release-commit")
	if err := (Coordinator{Store: s, Git: g, Lease: lease, Owner: "worker"}).Commit(context.Background(), r); err == nil || !strings.Contains(err.Error(), "lease release failed") {
		t.Fatalf("error=%v, want lease release failure", err)
	}
	if status, _, _, err := s.CommitStatus(context.Background(), r.ID); err != nil || status != state.CommitCreated {
		t.Fatalf("status=%q err=%v, want created commit retained", status, err)
	}
}

func TestRecoveryReconcilesUnknownOutcomeAndDoesNotRepeatGit(t *testing.T) {
	req := validCommitRequest("job")
	s := newMemoryStore()
	s.status[req.ID] = "COMMIT_REQUESTED"
	s.intents = append(s.intents, req)
	g := &fakeGit{inspect: "0123456789abcdef0123456789abcdef01234567"}
	if err := (Coordinator{Store: s, Git: g}).RecoverCommit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if g.calls != 0 || s.status[req.ID] != "CREATED" {
		t.Fatalf("calls=%d status=%s", g.calls, s.status[req.ID])
	}
	s2 := newMemoryStore()
	s2.status[req.ID] = "COMMIT_REQUESTED"
	s2.intents = append(s2.intents, req)
	if err := (Coordinator{Store: s2, Git: &fakeGit{}}).RecoverCommit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if s2.status[req.ID] != "RECONCILE_REQUIRED" {
		t.Fatalf("status=%s", s2.status[req.ID])
	}
}

func TestLocalOnlySkipsProvider(t *testing.T) {
	s := newMemoryStore()
	p := &fakeProvider{}
	c := Coordinator{Store: s, Provider: p}
	if err := c.Push(context.Background(), PushRequest{ID: "p", CommitSHA: "0123456789abcdef0123456789abcdef01234567", LocalOnly: true}); err != nil {
		t.Fatal(err)
	}
	if p.calls != 0 || !s.skipped {
		t.Fatalf("provider calls=%d skipped=%v", p.calls, s.skipped)
	}
}

func TestPushRequiresExactPostconditionAndIsIdempotent(t *testing.T) {
	s := newMemoryStore()
	p := &fakeProvider{confirmed: true}
	c := Coordinator{Store: s, Provider: p}
	r := PushRequest{ID: "p", Owner: "o", Name: "n", Ref: "main", CommitSHA: "0123456789abcdef0123456789abcdef01234567"}
	if err := c.Push(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if p.calls != 0 || p.confirms != 1 {
		t.Fatalf("push=%d confirms=%d", p.calls, p.confirms)
	}
	if err := c.Push(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if p.calls != 0 || p.confirms != 1 {
		t.Fatal("succeeded push repeated provider effect")
	}
	s2 := newMemoryStore()
	p2 := &fakeProvider{}
	if err := (Coordinator{Store: s2, Provider: p2}).Push(context.Background(), r); err == nil || s2.status[r.ID] != "RETRY_WAIT" {
		t.Fatalf("err=%v status=%s", err, s2.status[r.ID])
	}
}

func TestPushSerializesProviderEffectsWithWriterLease(t *testing.T) {
	s := newMemoryStore()
	lease := &recordingLease{}
	p := &fakeProvider{confirmed: true}
	r := PushRequest{ID: "leased-push", Owner: "owner", Name: "repo", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
	if err := (Coordinator{Store: s, Provider: p, Lease: lease, Owner: "worker"}).Push(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if lease.acquired != "owner/repo/main" || lease.owner != "worker" || lease.released != lease.acquired || p.confirms != 1 {
		t.Fatalf("lease=%+v confirms=%d", lease, p.confirms)
	}
}

func TestPushReportsLeaseReleaseFailureAfterProviderOutcome(t *testing.T) {
	s := newMemoryStore()
	lease := &recordingLease{releaseErr: errors.New("lease release failed")}
	p := &fakeProvider{confirmed: true}
	r := PushRequest{ID: "lease-release", Owner: "owner", Name: "repo", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
	if err := (Coordinator{Store: s, Provider: p, Lease: lease, Owner: "worker"}).Push(context.Background(), r); err == nil || !strings.Contains(err.Error(), "lease release failed") {
		t.Fatalf("error=%v, want lease release failure", err)
	}
	if s.status[r.ID] != state.PushSucceeded {
		t.Fatalf("state=%q, want succeeded provider outcome retained", s.status[r.ID])
	}
}

func TestStateStoreCommitStatusReturnsAllImmutableEvidence(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStateStore(db)
	want := CommitRequest{ID: "job", CandidateDigest: "candidate", BaseSHA: "base", MessageDigest: "message", PolicyDigest: "policy", VerifierDigest: "verifier", GuardDigest: "guard"}
	if err := store.PutCommitIntent(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	status, _, got, err := store.CommitStatus(context.Background(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status != state.CommitRequested || !want.EvidenceMatches(CommitEvidence{CandidateDigest: got.CandidateDigest, BaseSHA: got.BaseSHA, MessageDigest: got.MessageDigest, PolicyDigest: got.PolicyDigest, VerifierDigest: got.VerifierDigest, GuardDigest: got.GuardDigest}) {
		t.Fatalf("status=%q evidence=%+v", status, got)
	}
}

func TestStateStorePushIntentRetainsRemoteDigestBinding(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStateStore(db)
	want := PushRequest{ID: "push-binding", Owner: "owner", Name: "repo", Ref: "main", CommitSHA: strings.Repeat("a", 40), RemoteDigest: "sha256:" + strings.Repeat("b", 64)}
	if err := store.PutPushIntent(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	status, got, err := store.PushStatus(context.Background(), want.ID)
	if err != nil || status != state.PushRequested || got.RemoteDigest != want.RemoteDigest {
		t.Fatalf("status=%q request=%+v err=%v", status, got, err)
	}
}

func TestPushBlocksWrongExistingSHAWithoutPushing(t *testing.T) {
	s := newMemoryStore()
	p := &fakeProvider{confirmOutcome: PushConflict}
	r := PushRequest{ID: "p", Owner: "o", Name: "n", Ref: "main", CommitSHA: strings.Repeat("a", 64)}
	if err := (Coordinator{Store: s, Provider: p}).Push(context.Background(), r); err == nil {
		t.Fatal("wrong existing SHA was accepted")
	}
	if p.calls != 0 || s.status[r.ID] != "BLOCKED" {
		t.Fatalf("push=%d status=%s", p.calls, s.status[r.ID])
	}
}

func TestPushDoesNotMutateWhenRemoteConfirmationFails(t *testing.T) {
	s := newMemoryStore()
	p := &fakeProvider{confirmErr: provider.ErrOffline}
	r := PushRequest{ID: "p", Owner: "o", Name: "n", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
	if err := (Coordinator{Store: s, Provider: p}).Push(context.Background(), r); err == nil {
		t.Fatal("confirmation failure was accepted")
	}
	if p.calls != 0 || s.status[r.ID] != "RETRY_WAIT" {
		t.Fatalf("push=%d status=%s", p.calls, s.status[r.ID])
	}
}

func TestRetryPushResumesOnlyRetryableExactIntent(t *testing.T) {
	s := newMemoryStore()
	r := PushRequest{ID: "retry", Owner: "o", Name: "n", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
	s.pushes[r.ID] = r
	s.status[r.ID] = "RETRY_WAIT"
	p := &fakeProvider{confirmed: true}
	if err := (Coordinator{Store: s, Provider: p}).RetryPush(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	if s.status[r.ID] != "SUCCEEDED" || p.calls != 0 || p.confirms != 1 {
		t.Fatalf("status=%q pushes=%d confirms=%d", s.status[r.ID], p.calls, p.confirms)
	}
	if err := (Coordinator{Store: s, Provider: p}).RetryPush(context.Background(), r.ID); err == nil {
		t.Fatal("succeeded push was retried")
	}
	if p.confirms != 1 {
		t.Fatal("non-retryable state invoked provider")
	}
}

func TestStateLeaseSerializesOwnersUntilRelease(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Unix(100, 0)
	lease := StateLease{DB: db, TTL: time.Minute, Now: func() time.Time { return now }}
	if err := lease.Acquire(context.Background(), "repo/worktree/main", "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Acquire(context.Background(), "repo/worktree/main", "owner-b"); err == nil {
		t.Fatal("second owner acquired an active lease")
	}
	if err := lease.Release(context.Background(), "repo/worktree/main", "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Acquire(context.Background(), "repo/worktree/main", "owner-b"); err != nil {
		t.Fatalf("owner acquired after release: %v", err)
	}
}

func TestPushPersistsTypedProviderFailureStatesAtEveryBoundary(t *testing.T) {
	cases := []struct {
		name                     string
		err                      error
		want                     string
		boundary                 string
		wantPushes, wantConfirms int
	}{
		{"offline initial confirmation", provider.ErrOffline, "RETRY_WAIT", "initial", 0, 1},
		{"timeout initial confirmation", provider.ErrTimeout, "RETRY_WAIT", "initial", 0, 1},
		{"rate limit initial confirmation", provider.ErrRateLimit, "RETRY_WAIT", "initial", 0, 1},
		{"auth initial confirmation", provider.ErrAuth, "BLOCKED", "initial", 0, 1},
		{"non fast forward initial confirmation", provider.ErrNonFastForward, "BLOCKED", "initial", 0, 1},
		{"protected branch initial confirmation", provider.ErrProtectedBranch, "BLOCKED", "initial", 0, 1},
		{"secret scanning initial confirmation", provider.ErrSecretScanning, "BLOCKED", "initial", 0, 1},
		{"collision initial confirmation", provider.ErrCollision, "BLOCKED", "initial", 0, 1},
		{"ref conflict initial confirmation", provider.ErrRefConflict, "BLOCKED", "initial", 0, 1},
		{"postcondition initial confirmation", provider.ErrPostcondition, "BLOCKED", "initial", 0, 1},
		{"output limit initial confirmation", provider.ErrOutputLimit, "BLOCKED", "initial", 0, 1},
		{"local only initial confirmation", provider.ErrLocalOnly, "BLOCKED", "initial", 0, 1},
		{"unsupported push initial confirmation", provider.ErrUnsupportedPush, "BLOCKED", "initial", 0, 1},
		{"remote binding initial confirmation", provider.ErrRemoteBinding, "BLOCKED", "initial", 0, 1},
		{"unknown initial confirmation", errors.New("unknown provider failure"), "BLOCKED", "initial", 0, 1},
		{"offline publish", provider.ErrOffline, "RETRY_WAIT", "publish", 1, 1},
		{"auth publish", provider.ErrAuth, "BLOCKED", "publish", 1, 1},
		{"unknown publish", errors.New("unknown provider failure"), "BLOCKED", "publish", 1, 1},
		{"offline post confirmation", provider.ErrOffline, "RETRY_WAIT", "post", 1, 2},
		{"auth post confirmation", provider.ErrAuth, "BLOCKED", "post", 1, 2},
		{"unknown post confirmation", errors.New("unknown provider failure"), "BLOCKED", "post", 1, 2},
	}
	request := PushRequest{ID: "p", Owner: "o", Name: "n", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemoryStore()
			p := &fakeProvider{confirmed: tc.boundary == "post"}
			switch tc.boundary {
			case "initial":
				p.confirmErr = tc.err
			case "publish":
				p.confirmErrors = []error{nil}
				p.pushErr = tc.err
			case "post":
				p.confirmErrors = []error{nil, tc.err}
			}
			if err := (Coordinator{Store: s, Provider: p}).Push(context.Background(), request); err == nil {
				t.Fatal("provider failure returned nil")
			}
			if got := s.status[request.ID]; got != tc.want {
				t.Fatalf("state=%q, want %q", got, tc.want)
			}
			if p.calls != tc.wantPushes || p.confirms != tc.wantConfirms {
				t.Fatalf("pushes=%d confirms=%d, want pushes=%d confirms=%d", p.calls, p.confirms, tc.wantPushes, tc.wantConfirms)
			}
		})
	}
}

func TestPushJoinsProviderAndFailureStatePersistenceErrors(t *testing.T) {
	cases := []struct {
		name        string
		providerErr error
		persistErr  error
		retry       bool
	}{
		{"blocked", provider.ErrAuth, errors.New("blocked state database unavailable"), false},
		{"retry", provider.ErrOffline, errors.New("retry state database unavailable"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemoryStore()
			if tc.retry {
				s.markRetryErr = tc.persistErr
			} else {
				s.markBlockedErr = tc.persistErr
			}
			p := &fakeProvider{confirmErr: tc.providerErr}
			r := PushRequest{ID: "p", Owner: "o", Name: "n", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
			err := (Coordinator{Store: s, Provider: p}).Push(context.Background(), r)
			if !errors.Is(err, tc.providerErr) || !errors.Is(err, tc.persistErr) {
				t.Fatalf("error=%v, want provider and persistence causes", err)
			}
		})
	}
}

func TestPublicationProviderAdapterMapsExactRequestsOutcomesAndErrors(t *testing.T) {
	publication := &fakePublicationProvider{outcome: provider.PushPresent}
	adapter := PublicationProviderAdapter{Provider: publication}
	r := PushRequest{ID: "id", Owner: "owner", Name: "repo", Ref: "feature/x", CommitSHA: strings.Repeat("a", 40)}
	if err := adapter.Push(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if publication.published != (provider.PushRequest{Owner: r.Owner, Name: r.Name, Ref: r.Ref, SHA: r.CommitSHA}) {
		t.Fatalf("published request=%+v", publication.published)
	}
	for _, tc := range []struct {
		name    string
		outcome provider.PushOutcome
		want    ConfirmPushOutcome
	}{
		{"missing", provider.PushMissing, PushMissing},
		{"present", provider.PushPresent, PushPresent},
		{"conflict", provider.PushConflict, PushConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			publication.outcome = tc.outcome
			got, err := adapter.ConfirmPush(context.Background(), r)
			if err != nil || got != tc.want {
				t.Fatalf("outcome=%q err=%v, want %q", got, err, tc.want)
			}
			if publication.confirmed != (provider.PushRequest{Owner: r.Owner, Name: r.Name, Ref: r.Ref, SHA: r.CommitSHA}) {
				t.Fatalf("confirmed request=%+v", publication.confirmed)
			}
		})
	}
	typedErr := provider.ErrProtectedBranch
	publication.publishErr = typedErr
	if err := adapter.Push(context.Background(), r); !errors.Is(err, typedErr) {
		t.Fatalf("push error=%v, want %v", err, typedErr)
	}
	publication.confirmErr = typedErr
	if _, err := adapter.ConfirmPush(context.Background(), r); !errors.Is(err, typedErr) {
		t.Fatalf("confirm error=%v, want %v", err, typedErr)
	}
	publication.outcome = provider.PushConflict
	publication.confirmErr = provider.ErrRefConflict
	got, err := adapter.ConfirmPush(context.Background(), r)
	if got != PushConflict || !errors.Is(err, provider.ErrRefConflict) {
		t.Fatalf("conflict outcome=%q err=%v, want mapped conflict and typed error", got, err)
	}
	publication.outcome = provider.PushOutcome("unexpected")
	publication.confirmErr = provider.ErrAuth
	if _, err := adapter.ConfirmPush(context.Background(), r); !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("unknown outcome error=%v, want typed provider error", err)
	}
}

func TestCommitAccepts64CharacterCommitSHA(t *testing.T) {
	s := newMemoryStore()
	g := &fakeGit{sha: strings.Repeat("a", 64)}
	r := validCommitRequest("job")
	if err := (Coordinator{Store: s, Git: g}).Commit(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestCommitRejectsNonCanonicalEvidenceBeforeIntent(t *testing.T) {
	s := newMemoryStore()
	r := validCommitRequest("invalid")
	r.PolicyDigest = "policy"
	if err := (Coordinator{Store: s, Git: &fakeGit{sha: strings.Repeat("a", 40)}}).Commit(context.Background(), r); err == nil {
		t.Fatal("non-canonical policy digest accepted")
	}
	if len(s.intents) != 0 {
		t.Fatal("invalid evidence persisted intent")
	}
}

func TestCreatedWithoutValidSHAReconcilesInsteadOfSucceeding(t *testing.T) {
	s := newMemoryStore()
	r := validCommitRequest("created-invalid")
	s.status[r.ID] = "CREATED"
	s.createdSHA = "not-a-sha"
	s.intents = append(s.intents, r)
	if err := (Coordinator{Store: s, Git: &fakeGit{sha: strings.Repeat("a", 40)}}).Commit(context.Background(), r); err == nil {
		t.Fatal("invalid CREATED record returned success")
	}
	if s.status[r.ID] != "RECONCILE_REQUIRED" {
		t.Fatalf("status=%q, want RECONCILE_REQUIRED", s.status[r.ID])
	}
}

type memoryStore struct {
	intents        []CommitRequest
	status         map[string]string
	createdSHA     string
	skipped        bool
	trace          *[]string
	pushes         map[string]PushRequest
	markBlockedErr error
	markRetryErr   error
}

type recordingLease struct {
	acquired, released, owner string
	releaseErr                error
}

func (l *recordingLease) Acquire(_ context.Context, key, owner string) error {
	l.acquired, l.owner = key, owner
	return nil
}

func (l *recordingLease) Release(_ context.Context, key, owner string) error {
	if owner != l.owner {
		return errors.New("lease owner changed")
	}
	l.released = key
	return l.releaseErr
}

func newMemoryStore() *memoryStore {
	return &memoryStore{status: map[string]string{}, trace: &[]string{}, pushes: map[string]PushRequest{}}
}
func (s *memoryStore) PutCommitIntent(_ context.Context, r CommitRequest) error {
	*s.trace = append(*s.trace, "persist_intent")
	s.intents = append(s.intents, r)
	s.status[r.ID] = "COMMIT_REQUESTED"
	return nil
}
func (s *memoryStore) CommitStatus(_ context.Context, id string) (string, string, CommitRequest, error) {
	for _, r := range s.intents {
		if r.ID == id {
			return s.status[id], s.createdSHA, r, nil
		}
	}
	return s.status[id], "", CommitRequest{}, nil
}
func (s *memoryStore) RecordCommit(_ context.Context, id, sha string) error {
	*s.trace = append(*s.trace, "persist_result")
	s.status[id] = "CREATED"
	return nil
}
func (s *memoryStore) RecordReconcile(_ context.Context, id string) error {
	s.status[id] = "RECONCILE_REQUIRED"
	return nil
}
func (s *memoryStore) PutPushIntent(_ context.Context, r PushRequest) error {
	s.pushes[r.ID] = r
	s.status[r.ID] = "PUSH_REQUESTED"
	return nil
}
func (s *memoryStore) PushStatus(_ context.Context, id string) (string, PushRequest, error) {
	return s.status[id], s.pushes[id], nil
}
func (s *memoryStore) MarkPushSkipped(_ context.Context, id string) error {
	s.skipped = true
	s.status[id] = "SKIPPED_LOCAL"
	return nil
}
func (s *memoryStore) MarkPushSucceeded(_ context.Context, id string) error {
	s.status[id] = "SUCCEEDED"
	return nil
}
func (s *memoryStore) MarkPushBlocked(_ context.Context, id string) error {
	if s.markBlockedErr != nil {
		return s.markBlockedErr
	}
	s.status[id] = "BLOCKED"
	return nil
}

func (s *memoryStore) MarkPushRetry(_ context.Context, id string) error {
	if s.markRetryErr != nil {
		return s.markRetryErr
	}
	s.status[id] = "RETRY_WAIT"
	return nil
}

func validCommitRequest(id string) CommitRequest {
	return CommitRequest{ID: id, CandidateDigest: "sha256:" + strings.Repeat("a", 64), BaseSHA: strings.Repeat("b", 40), MessageDigest: "sha256:" + strings.Repeat("c", 64), PolicyDigest: "sha256:" + strings.Repeat("d", 64), VerifierDigest: "sha256:" + strings.Repeat("e", 64), GuardDigest: "sha256:" + strings.Repeat("f", 64)}
}

type fakeGit struct {
	sha     string
	calls   int
	trace   *[]string
	inspect string
}

func (g *fakeGit) Commit(_ context.Context, _ CommitRequest) (string, error) {
	if g.trace != nil {
		*g.trace = append(*g.trace, "git_effect")
	}
	g.calls++
	return g.sha, nil
}
func (g *fakeGit) Inspect(_ context.Context, _ CommitRequest) (string, error) { return g.inspect, nil }

type fakeProvider struct {
	calls          int
	confirms       int
	confirmed      bool
	confirmOutcome ConfirmPushOutcome
	confirmErr     error
	confirmErrors  []error
	pushErr        error
}

func (p *fakeProvider) Push(_ context.Context, _ PushRequest) error { p.calls++; return p.pushErr }
func (p *fakeProvider) ConfirmPush(_ context.Context, _ PushRequest) (ConfirmPushOutcome, error) {
	p.confirms++
	if len(p.confirmErrors) > 0 {
		err := p.confirmErrors[0]
		p.confirmErrors = p.confirmErrors[1:]
		if err != nil {
			return "", err
		}
		return PushMissing, nil
	}
	if p.confirmErr != nil {
		return "", p.confirmErr
	}
	if p.confirmed {
		return PushPresent, nil
	}
	if p.confirmOutcome != "" {
		return p.confirmOutcome, nil
	}
	return PushMissing, provider.ErrOffline
}

type fakePublicationProvider struct {
	published  provider.PushRequest
	confirmed  provider.PushRequest
	outcome    provider.PushOutcome
	publishErr error
	confirmErr error
}

func (p *fakePublicationProvider) Create(context.Context, provider.RemoteRequest) (string, error) {
	return "", errors.New("create must not be called")
}
func (p *fakePublicationProvider) Publish(_ context.Context, r provider.PushRequest) error {
	p.published = r
	return p.publishErr
}
func (p *fakePublicationProvider) ConfirmPush(_ context.Context, r provider.PushRequest) (provider.PushOutcome, error) {
	p.confirmed = r
	return p.outcome, p.confirmErr
}
