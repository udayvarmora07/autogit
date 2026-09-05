package gittransaction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/state"
)

// testRepoDir mirrors internal/state's platform-absolute test repository path.
var testRepoDir = filepath.Join(os.TempDir(), "autogit-test-repo")

func TestStateIntentPortPersistsIntentAndRedactsReconcileReason(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	port := NewStateIntentPort(store)
	intent := testPersistentIntent("persistent")
	if err := port.PutCommitIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	got, err := port.GetCommitIntent(context.Background(), intent.ID)
	if err != nil || got != intent {
		t.Fatalf("intent=%+v err=%v", got, err)
	}
	if err := port.RecordCommit(context.Background(), intent.ID, "not-a-sha"); !errors.Is(err, state.ErrInvalidGitCommitSHA) {
		t.Fatalf("invalid SHA error=%v", err)
	}
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := port.RecordCommit(context.Background(), intent.ID, sha); err != nil {
		t.Fatal(err)
	}
	if err := port.RecordCommit(context.Background(), intent.ID, sha); err != nil {
		t.Fatalf("idempotent SHA retry failed: %v", err)
	}
	if err := port.RecordReconcile(context.Background(), intent.ID, "raw command output secret=do-not-store"); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentIntentPortRecoversRealGitRefAfterLostResponse(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "base\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "a.txt"), "candidate\n")

	statePath := filepath.Join(t.TempDir(), "state.db")
	store, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	port := NewStateIntentPort(store)
	req := Request{ID: "persistent-recovery", RepoDir: repo, Snapshot: []SnapshotEntry{snapshotFile(t, filepath.Join(repo, "a.txt"), "candidate\n")}, Message: "feat: persistent recovery", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
	if _, err := New(&failAfterRefRunner{base: SystemRunner{}}, port).Create(context.Background(), req); err == nil {
		t.Fatal("lost ref response reported as success")
	}
	record, err := store.GitCommitIntentRecord(context.Background(), req.ID)
	if err != nil || record.State != state.CommitIntentReconcile || record.ReasonCode != "REF_UPDATE_UNKNOWN" {
		t.Fatalf("durable reconcile record=%+v err=%v", record, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := New(SystemRunner{}, NewStateIntentPort(store)).Recover(context.Background(), req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA == "" || got.State != "CREATED" || git(t, repo, "rev-parse", "--verify", got.Ref) != got.SHA {
		t.Fatalf("recovered commit=%+v", got)
	}
	record, err = store.GitCommitIntentRecord(context.Background(), req.ID)
	if err != nil || record.State != state.CommitCreated || record.SHA != got.SHA || record.ReasonCode != "" {
		t.Fatalf("recovery record=%+v err=%v", record, err)
	}
}

func TestPersistentIntentPortReopensAfterCommitResultFailure(t *testing.T) {
	repo := newRepo(t)
	statePath := filepath.Join(t.TempDir(), "state.db")
	store, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("commit result unavailable")
	fault := &failRecordOncePort{inner: NewStateIntentPort(store), err: failure}
	runner := &countingRunner{base: SystemRunner{}}
	req := Request{ID: "persistent-result-recovery", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "a.txt", Content: []byte("candidate\n"), Mode: 0644}}, Message: "feat: persistent result recovery", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
	if _, err := New(runner, fault).Create(context.Background(), req); !errors.Is(err, failure) {
		t.Fatalf("error=%v, want result persistence failure", err)
	}
	if runner.commitTrees != 1 {
		t.Fatalf("commit-tree calls=%d, want one before restart", runner.commitTrees)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, err := New(runner, NewStateIntentPort(restarted)).Recover(context.Background(), req.ID)
	if err != nil || got.SHA == "" {
		t.Fatalf("restart recovery commit=%+v err=%v", got, err)
	}
	if runner.commitTrees != 1 {
		t.Fatalf("commit-tree calls=%d after restart, want one", runner.commitTrees)
	}
	record, err := restarted.GitCommitIntentRecord(context.Background(), req.ID)
	if err != nil || record.State != state.CommitCreated || record.SHA != got.SHA {
		t.Fatalf("durable record=%+v err=%v", record, err)
	}
}

func TestPersistentIntentPortRejectsSnapshotIdentityCollision(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	port := NewStateIntentPort(store)
	one := testPersistentIntent("collision")
	if err := port.PutCommitIntent(context.Background(), one); err != nil {
		t.Fatal(err)
	}
	two := one
	two.SnapshotDigest = "sha256:" + strings.Repeat("f", 64)
	if err := port.PutCommitIntent(context.Background(), two); !errors.Is(err, state.ErrGitCommitIntentConflict) {
		t.Fatalf("collision error=%v", err)
	}
}

func TestValidateIntentAcceptsOnlyGitObjectWidths(t *testing.T) {
	for _, width := range []int{40, 64} {
		intent := testPersistentIntent("oid-valid-" + strings.TrimSpace(string(rune('0'+width%10))))
		intent.TreeOID = strings.Repeat("a", width)
		if err := validateIntent(intent); err != nil {
			t.Fatalf("tree width=%d rejected: %v", width, err)
		}
	}
	for _, width := range []int{39, 41, 63, 65} {
		intent := testPersistentIntent("bad-oid-" + strings.TrimSpace(string(rune('0'+width%10))))
		intent.TreeOID = strings.Repeat("a", width)
		if err := validateIntent(intent); err == nil {
			t.Fatalf("tree width=%d accepted", width)
		}
	}
}

func testPersistentIntent(id string) Intent {
	d := func(ch byte) string {
		b := make([]byte, 64)
		for i := range b {
			b[i] = ch
		}
		return "sha256:" + string(b)
	}
	return Intent{ID: id, RepoDir: testRepoDir, Ref: "refs/autogit/commits/" + id, ParentSHA: "", TreeOID: "0123456789012345678901234567890123456789", Message: "feat: persist\n", CandidateDigest: d('a'), MessageDigest: d('b'), SnapshotDigest: d('c'), PolicyDigest: d('d'), VerifierDigest: d('e'), GuardDigest: d('f')}
}

type failRecordOncePort struct {
	inner IntentPort
	err   error
}

func (p *failRecordOncePort) PutCommitIntent(ctx context.Context, intent Intent) error {
	return p.inner.PutCommitIntent(ctx, intent)
}

func (p *failRecordOncePort) GetCommitIntent(ctx context.Context, id string) (Intent, error) {
	return p.inner.GetCommitIntent(ctx, id)
}

func (p *failRecordOncePort) RecordCommit(ctx context.Context, id, sha string) error {
	if p.err != nil {
		err := p.err
		p.err = nil
		return err
	}
	return p.inner.RecordCommit(ctx, id, sha)
}

func (p *failRecordOncePort) RecordReconcile(ctx context.Context, id, reason string) error {
	return p.inner.RecordReconcile(ctx, id, reason)
}
