package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"autogit/internal/repository"
)

// testRepoDir is a platform-absolute repository path. The Unix-only literal
// "/repo" is not absolute on Windows (filepath.IsAbs("/repo") is false), which
// made intent validation reject every test intent.
var testRepoDir = filepath.Join(os.TempDir(), "autogit-test-repo")

func TestStorePersistsTypedJobAndOutboxAtomically(t *testing.T) {
	d := t.TempDir()
	s, err := Open(filepath.Join(d, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	job := CommitJob{ID: "cj-1", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), State: CommitRequested}
	if err := s.WithTx(context.Background(), func(tx *Tx) error {
		if err := tx.PutCommitJob(job); err != nil {
			return err
		}
		return tx.EnqueueOutbox(Outbox{ID: "ob-1", Kind: "commit.requested", AggregateID: job.ID, Payload: []byte(`{"job":"cj-1"}`)})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.CommitJob("cj-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != CommitRequested || got.CandidateDigest != job.CandidateDigest {
		t.Fatalf("got %#v", got)
	}
	outs, err := s.PendingOutbox(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 1 || outs[0].Kind != "commit.requested" {
		t.Fatalf("outbox %#v", outs)
	}
}

func TestStoreRejectsMalformedPushIntent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, job := range []PushJob{
		{ID: "push-1", Owner: "owner", Name: "repo", Ref: "main", CommitSHA: "not-a-sha", State: PushRequested},
		{ID: "push-2", Owner: "owner", Name: "repo", Ref: "main", CommitSHA: strings.Repeat("a", 40), State: "UNKNOWN"},
		{ID: "push-3", Owner: "owner/name", Name: "repo", Ref: "main", CommitSHA: strings.Repeat("a", 40), State: PushRequested},
	} {
		err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutPushJob(job) })
		if err == nil {
			t.Fatalf("malformed push job accepted: %+v", job)
		}
	}
}

func TestStoreRejectsUnsafeFilePermissions(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "state.db")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("world-readable state accepted")
	}
}

func TestLeaseExpiresAndCanBeTakenOver(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := int64(100)
	if err := s.AcquireLease(context.Background(), Lease{Key: "repo/r", Owner: "a", ExpiresAt: now + 5}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireLease(context.Background(), Lease{Key: "repo/r", Owner: "a", ExpiresAt: now + 5}, now); err == nil {
		t.Fatal("same owner reacquired an active lease")
	}
	if err := s.AcquireLease(context.Background(), Lease{Key: "repo/r", Owner: "b", ExpiresAt: now + 5}, now+1); err == nil {
		t.Fatal("active lease taken over")
	}
	if err := s.AcquireLease(context.Background(), Lease{Key: "repo/r", Owner: "b", ExpiresAt: now + 10}, now+6); err != nil {
		t.Fatal(err)
	}
}

func TestCommitJobIdentityCannotBeReplacedByRetry(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := CommitJob{ID: "same", CandidateDigest: "sha256:" + repeated('a'), BaseSHA: "base", MessageDigest: "sha256:" + repeated('b'), State: CommitRequested}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutCommitJob(first) }); err != nil {
		t.Fatal(err)
	}
	second := first
	second.CandidateDigest = "sha256:" + repeated('c')
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutCommitJob(second) }); err == nil {
		t.Fatal("retry replaced immutable candidate evidence")
	}
}

func TestRecordCommitJobUpdatesAtomically(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	job := CommitJob{ID: "atomic-job", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), State: CommitRequested}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutCommitJob(job) }); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("d", 40)
	if err := s.RecordCommitJob(context.Background(), job.ID, sha); err != nil {
		t.Fatal(err)
	}
	got, err := s.CommitJob(job.ID)
	if err != nil || got.CommitSHA != sha || got.State != CommitCreated {
		t.Fatalf("job=%+v err=%v", got, err)
	}
}

func TestStoreUpgradesLegacyEvidenceColumnsAndSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE commits (id TEXT PRIMARY KEY,candidate_digest TEXT NOT NULL,base_sha TEXT NOT NULL,message_digest TEXT NOT NULL,commit_sha TEXT,state TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL); CREATE TABLE state_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL); INSERT INTO state_meta(key,value) VALUES('schema_version','1')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version string
	if err := s.db.QueryRow(`SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "7" {
		t.Fatalf("schema version=%q, want 7", version)
	}
	rows, err := s.db.Query(`PRAGMA table_info(commits)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"policy_digest", "verifier_digest", "guard_digest"} {
		if !columns[name] {
			t.Fatalf("legacy upgrade omitted %s", name)
		}
	}
}

func TestStoreFailsClosedForFutureSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE state_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL); INSERT INTO state_meta(key,value) VALUES('schema_version','99')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "unsupported state schema") {
		t.Fatalf("Open future schema error=%v", err)
	}
}

func TestGitCommitIntentPersistsAcrossRestartAndContainsNoSnapshotBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.db")
	intent := GitCommitIntent{
		ID: "intent-1", RepoDir: testRepoDir, Ref: "refs/autogit/commits/intent-1", ParentSHA: repeated('1'),
		TreeOID: repeated('2'), Message: "feat: durable intent\n", CandidateDigest: "sha256:" + repeated('a'),
		MessageDigest: "sha256:" + repeated('b'), SnapshotDigest: "sha256:" + repeated('c'),
		PolicyDigest: "sha256:" + repeated('d'), VerifierDigest: "sha256:" + repeated('e'), GuardDigest: "sha256:" + repeated('f'),
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(intent) }); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GitCommitIntent(context.Background(), intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != intent {
		t.Fatalf("intent changed across restart: got=%+v want=%+v", got, intent)
	}
	var tables int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='git_commit_intents'`).Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("intent table missing: count=%d err=%v", tables, err)
	}
}

func TestSessionBaselinePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Session{ID: "session-1", RepositoryID: "repo-1", State: "ACTIVE", BaselineHead: "0123456789012345678901234567890123456789", BaselineIndex: "sha256:" + repeated('a'), StatusDigest: "sha256:" + repeated('b'), BaselinePathsDigest: "sha256:" + repeated('c'), ClientID: "codex", Revision: 1}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutSession(want) }); err != nil {
		t.Fatal(err)
	}
	got, err := s.Session(context.Background(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("session=%+v want %+v", got, want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.Session(context.Background(), want.ID)
	if err != nil || got != want {
		t.Fatalf("restarted session=%+v err=%v", got, err)
	}
}

func TestRecordSessionBaselinePersistsOnlyBoundedRepositoryEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	baseline := repository.Baseline{
		Head:        "0123456789012345678901234567890123456789",
		IndexDigest: "sha256:" + repeated('a'), StatusDigest: "sha256:" + repeated('b'), PathsDigest: "sha256:" + repeated('c'),
		Paths: []string{"private.txt"}, Files: map[string]repository.FileObservation{"private.txt": {Content: []byte("private source"), Present: true}},
	}
	if err := s.RecordSessionBaseline(context.Background(), "session-2", "repo-2", "codex", baseline); err != nil {
		t.Fatal(err)
	}
	got, err := s.Session(context.Background(), "session-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaselineHead != baseline.Head || got.BaselineIndex != baseline.IndexDigest || got.StatusDigest != baseline.StatusDigest || got.BaselinePathsDigest != baseline.PathsDigest {
		t.Fatalf("session baseline=%+v", got)
	}
	if got.State != "ACTIVE" || got.ClientID != "codex" {
		t.Fatalf("session metadata=%+v", got)
	}
}

func TestRecordSessionBaselinePersistsSourceFreeDurableEvidence(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	baseline := repository.Baseline{
		Head: "0123456789012345678901234567890123456789", IndexDigest: "sha256:" + repeated('a'), StatusDigest: "sha256:" + repeated('b'),
		PathsDigest: repository.DigestPaths([]string{"private.txt"}), Paths: []string{"private.txt"},
		Files: map[string]repository.FileObservation{"private.txt": {Content: []byte("private source"), Mode: 0644, Present: true}},
	}
	baseline.DurableEvidence, err = repository.EncodeDurableBaseline(baseline, []byte("identity-key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSessionBaseline(context.Background(), "session-evidence", "repo-evidence", "codex", baseline); err != nil {
		t.Fatal(err)
	}
	got, err := s.Session(context.Background(), "session-evidence")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaselineEvidence != baseline.DurableEvidence || strings.Contains(got.BaselineEvidence, "private source") || strings.Contains(got.BaselineEvidence, "private.txt") {
		t.Fatalf("stored durable evidence=%q", got.BaselineEvidence)
	}
}

func TestRecordSessionBaselineRejectsIdentityChangeOnReplay(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := repository.Baseline{Head: "0123456789012345678901234567890123456789", IndexDigest: "sha256:" + repeated('a'), StatusDigest: "sha256:" + repeated('b'), PathsDigest: "sha256:" + repeated('c')}
	if err := s.RecordSessionBaseline(context.Background(), "session-3", "repo-3", "codex", base); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSessionBaseline(context.Background(), "session-3", "repo-3", "codex", base); err != nil {
		t.Fatalf("exact replay rejected: %v", err)
	}
	changed := base
	changed.StatusDigest = "sha256:" + repeated('d')
	if err := s.RecordSessionBaseline(context.Background(), "session-3", "repo-3", "codex", changed); err == nil {
		t.Fatal("changed baseline replay accepted")
	}
	got, err := s.Session(context.Background(), "session-3")
	if err != nil || got.StatusDigest != base.StatusDigest {
		t.Fatalf("stored baseline changed after conflict: %+v err=%v", got, err)
	}
}

func TestGitCommitIntentRejectsIdentityChangeAndAllowsExactRetry(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := GitCommitIntent{ID: "same", RepoDir: testRepoDir, Ref: "refs/autogit/commits/same", TreeOID: repeated('1'), Message: "feat: immutable identity\n", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), SnapshotDigest: "sha256:" + repeated('c'), PolicyDigest: "sha256:" + repeated('d'), VerifierDigest: "sha256:" + repeated('e'), GuardDigest: "sha256:" + repeated('f')}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(first) }); err != nil {
		t.Fatal(err)
	}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(first) }); err != nil {
		t.Fatalf("exact retry rejected: %v", err)
	}
	changed := first
	changed.Message = "different"
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(changed) }); !errors.Is(err, ErrGitCommitIntentConflict) {
		t.Fatalf("identity change error=%v, want conflict", err)
	}
}

func TestGitCommitIntentRejectsMalformedObjectRefsAndDigests(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, width := range []int{39, 41, 63, 65} {
		intent := GitCommitIntent{ID: "width-" + strconv.Itoa(width), RepoDir: testRepoDir, Ref: "refs/autogit/commits/width-" + strconv.Itoa(width), TreeOID: strings.Repeat("a", width), Message: "feat: object width\n", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), SnapshotDigest: "sha256:" + repeated('c'), PolicyDigest: "sha256:" + repeated('d'), VerifierDigest: "sha256:" + repeated('e'), GuardDigest: "sha256:" + repeated('f')}
		if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(intent) }); !errors.Is(err, ErrInvalidGitCommitIntent) {
			t.Fatalf("tree width=%d error=%v", width, err)
		}
	}
	badRef := GitCommitIntent{ID: "bad-ref", RepoDir: testRepoDir, Ref: "refs/heads/main", TreeOID: repeated('a'), Message: "feat: owned ref\n", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), SnapshotDigest: "sha256:" + repeated('c'), PolicyDigest: "sha256:" + repeated('d'), VerifierDigest: "sha256:" + repeated('e'), GuardDigest: "sha256:" + repeated('f')}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(badRef) }); !errors.Is(err, ErrInvalidGitCommitIntent) {
		t.Fatalf("malformed ref error=%v", err)
	}
}

func TestGitCommitIntentCommitAndReconcileAreBoundedAndIdempotent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	intent := GitCommitIntent{ID: "record", RepoDir: testRepoDir, Ref: "refs/autogit/commits/record", TreeOID: repeated('1'), Message: "feat: record commit\n", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), SnapshotDigest: "sha256:" + repeated('c'), PolicyDigest: "sha256:" + repeated('d'), VerifierDigest: "sha256:" + repeated('e'), GuardDigest: "sha256:" + repeated('f')}
	if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(intent) }); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGitCommit(context.Background(), intent.ID, "bad"); !errors.Is(err, ErrInvalidGitCommitSHA) {
		t.Fatalf("invalid SHA error=%v", err)
	}
	sha := repeated('9')
	if err := s.RecordGitCommit(context.Background(), intent.ID, sha); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGitCommit(context.Background(), intent.ID, sha); err != nil {
		t.Fatalf("same SHA retry rejected: %v", err)
	}
	if err := s.RecordGitCommit(context.Background(), intent.ID, repeated('8')); !errors.Is(err, ErrGitCommitIntentConflict) {
		t.Fatalf("different SHA error=%v", err)
	}
	if err := s.RecordGitReconcile(context.Background(), intent.ID, "commit-tree output leaked secret: top-secret"); err != nil {
		t.Fatal(err)
	}
	record, err := s.GitCommitIntentRecord(context.Background(), intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ReasonCode == "" || record.ReasonCode == "commit-tree output leaked secret: top-secret" || len(record.ReasonCode) > 64 {
		t.Fatalf("unbounded/raw reconcile reason: %+v", record)
	}
}

func TestStoreMigratesV2ToV3IntentTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE state_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL); INSERT INTO state_meta(key,value) VALUES('schema_version','2');`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version string
	if err := s.db.QueryRow(`SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "7" {
		t.Fatalf("schema version=%q, want 7", version)
	}
}

func TestGitCommitIntentRejectsNonCanonicalRepositoryAndMessage(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intent-invariants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := GitCommitIntent{ID: "invariant", RepoDir: testRepoDir, Ref: "refs/autogit/commits/invariant", TreeOID: repeated('a'), Message: "feat: valid message\n", CandidateDigest: "sha256:" + repeated('a'), MessageDigest: "sha256:" + repeated('b'), SnapshotDigest: "sha256:" + repeated('c'), PolicyDigest: "sha256:" + repeated('d'), VerifierDigest: "sha256:" + repeated('e'), GuardDigest: "sha256:" + repeated('f')}
	for name, mutate := range map[string]func(*GitCommitIntent){
		"non-canonical repo":       func(i *GitCommitIntent) { i.RepoDir = "/repo/../repo" },
		"NUL repository":           func(i *GitCommitIntent) { i.RepoDir = "/repo\x00" },
		"non-conventional message": func(i *GitCommitIntent) { i.Message = "not a conventional commit\n" },
		"empty message":            func(i *GitCommitIntent) { i.Message = "" },
		"oversized message":        func(i *GitCommitIntent) { i.Message = "feat: " + strings.Repeat("x", 1<<20) },
	} {
		candidate := base
		candidate.ID = "invariant-" + strings.ReplaceAll(name, " ", "-")
		candidate.Ref = "refs/autogit/commits/" + candidate.ID
		mutate(&candidate)
		if err := s.WithTx(context.Background(), func(tx *Tx) error { return tx.PutGitCommitIntent(candidate) }); !errors.Is(err, ErrInvalidGitCommitIntent) {
			t.Errorf("%s error=%v", name, err)
		}
	}
}

func TestGitCommitIntentAPIsRejectInvalidIDs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "intent-id.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"", "../escape", "bad/slash"} {
		if _, err := s.GitCommitIntent(context.Background(), id); !errors.Is(err, ErrInvalidGitCommitIntent) {
			t.Errorf("Get id %q error=%v", id, err)
		}
		if _, err := s.GitCommitIntentRecord(context.Background(), id); !errors.Is(err, ErrInvalidGitCommitIntent) {
			t.Errorf("Get record id %q error=%v", id, err)
		}
		if err := s.RecordGitCommit(context.Background(), id, repeated('a')); !errors.Is(err, ErrInvalidGitCommitIntent) {
			t.Errorf("Record commit id %q error=%v", id, err)
		}
		if err := s.RecordGitReconcile(context.Background(), id, "unknown"); !errors.Is(err, ErrInvalidGitCommitIntent) {
			t.Errorf("Record reconcile id %q error=%v", id, err)
		}
	}
}

func repeated(r byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = r
	}
	return string(b)
}
