package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
	p := &fakeProvider{confirmErr: errors.New("remote unavailable")}
	r := PushRequest{ID: "p", Owner: "o", Name: "n", Ref: "main", CommitSHA: strings.Repeat("a", 40)}
	if err := (Coordinator{Store: s, Provider: p}).Push(context.Background(), r); err == nil {
		t.Fatal("confirmation failure was accepted")
	}
	if p.calls != 0 || s.status[r.ID] != "RETRY_WAIT" {
		t.Fatalf("push=%d status=%s", p.calls, s.status[r.ID])
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
	intents    []CommitRequest
	status     map[string]string
	createdSHA string
	skipped    bool
	trace      *[]string
	pushes     map[string]PushRequest
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
	s.status[id] = "BLOCKED"
	return nil
}

func (s *memoryStore) MarkPushRetry(_ context.Context, id string) error {
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
}

func (p *fakeProvider) Push(_ context.Context, _ PushRequest) error { p.calls++; return nil }
func (p *fakeProvider) ConfirmPush(_ context.Context, _ PushRequest) (ConfirmPushOutcome, error) {
	p.confirms++
	if p.confirmErr != nil {
		return "", p.confirmErr
	}
	if p.confirmed {
		return PushPresent, nil
	}
	if p.confirmOutcome != "" {
		return p.confirmOutcome, nil
	}
	return PushMissing, errors.New("not present")
}
