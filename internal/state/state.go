// Package state contains the small, durable projection used by workflow
// coordinators. The package deliberately exposes records rather than tables.
package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"autogit/internal/commit"
	"autogit/internal/repository"

	_ "modernc.org/sqlite"
)

const (
	currentSchemaVersion  = 6
	CommitRequested       = "COMMIT_REQUESTED"
	CommitQueued          = "QUEUED"
	CommitRunning         = "RUNNING"
	CommitCreated         = "CREATED"
	CommitFailed          = "FAILED"
	PushRequested         = "PUSH_REQUESTED"
	PushRetryWait         = "RETRY_WAIT"
	PushSucceeded         = "SUCCEEDED"
	PushBlocked           = "BLOCKED"
	PushSkippedLocal      = "SKIPPED_LOCAL"
	RemoteRequested       = "REMOTE_REQUESTED"
	RemoteCreated         = "REMOTE_CREATED"
	RemoteAttached        = "REMOTE_ATTACHED"
	RemoteFailed          = "REMOTE_FAILED"
	CommitIntentRequested = "COMMIT_REQUESTED"
	CommitIntentReconcile = "RECONCILE_REQUIRED"
)

var (
	ErrGitCommitIntentConflict = errors.New("git commit intent identity conflict")
	ErrInvalidGitCommitIntent  = errors.New("invalid git commit intent")
	ErrInvalidGitCommitSHA     = errors.New("invalid git commit SHA")
)

type CommitJob struct {
	ID, CandidateDigest, BaseSHA, MessageDigest, PolicyDigest, VerifierDigest, GuardDigest, CommitSHA, State string
	CreatedAt, UpdatedAt                                                                                     int64
}

// GitCommitIntent mirrors the immutable identity accepted by gittransaction,
// without importing that package. Snapshot bytes and command diagnostics are
// intentionally not represented here.
type GitCommitIntent struct {
	ID, RepoDir, Ref, ParentSHA, TreeOID, Message  string
	CandidateDigest, MessageDigest, SnapshotDigest string
	PolicyDigest, VerifierDigest, GuardDigest      string
}

type GitCommitIntentRecord struct {
	Intent                 GitCommitIntent
	SHA, State, ReasonCode string
	CreatedAt, UpdatedAt   int64
}
type PushJob struct {
	ID, CommitJobID, RemoteDigest, Owner, Name, Ref, CommitSHA, State string
	LocalOnly                                                         bool
	CreatedAt, UpdatedAt                                              int64
}
type RemoteJob struct {
	ID, RepositoryID, Owner, Name, Alias, Visibility, URL, HostedIdentity, State string
	CreatedAt, UpdatedAt                                                         int64
}
type Policy struct {
	ID, RepositoryID, Decision, Visibility, Workflow string
	LocalOnly, PublicConsent                         bool
	Revision                                         int64
}
type Session struct {
	ID, RepositoryID, State                                                  string
	BaselineHead, BaselineIndex, StatusDigest, BaselinePathsDigest, ClientID string
	Revision                                                                 int64
}
type Task struct {
	ID, SessionID, State string
	Revision             int64
}
type Prompt struct {
	ID, TaskID, Kind, State, IdempotencyKey string
	Blocking                                bool
	Revision                                int64
}
type ChangeSet struct {
	ID, TaskID, BaseSHA, TreeDigest, IndexDigest, State string
	Revision                                            int64
}
type VerificationJob struct {
	ID, ChangeSetID, CandidateDigest, PolicyDigest, VerifierDigest, State string
	EvidenceDigest                                                        string
	Revision                                                              int64
}
type AuditEvent struct {
	ID, RepositoryID, ReasonCode, Metadata, PrevDigest, Digest string
	At                                                         int64
}
type Lease struct {
	Key, Owner string
	ExpiresAt  int64
}
type Outbox struct {
	ID, Kind, AggregateID string
	Payload               []byte
	CreatedAt             int64
	PublishedAt           sql.NullInt64
}

type Store struct {
	db   *sql.DB
	mu   sync.Mutex
	path string
}
type Tx struct{ tx *sql.Tx }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("state path is empty")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, fmt.Errorf("state directory: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("state database permissions are too broad")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0600); err != nil && !os.IsNotExist(err) {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	// Read the version before changing any tables. A newer state database is
	// intentionally rejected rather than partially mutated by an older binary.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS state_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL)`); err != nil {
		return err
	}
	version := 0
	var rawVersion string
	versionErr := s.db.QueryRow(`SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&rawVersion)
	if versionErr == nil {
		version, versionErr = strconv.Atoi(rawVersion)
		if versionErr != nil || version < 1 {
			return fmt.Errorf("unsupported state schema version %q", rawVersion)
		}
	} else if !errors.Is(versionErr, sql.ErrNoRows) {
		return versionErr
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("unsupported state schema version %d", version)
	}
	_, err := s.db.Exec(`PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS commits (id TEXT PRIMARY KEY,candidate_digest TEXT NOT NULL,base_sha TEXT,message_digest TEXT NOT NULL,policy_digest TEXT NOT NULL DEFAULT '',verifier_digest TEXT NOT NULL DEFAULT '',guard_digest TEXT NOT NULL DEFAULT '',commit_sha TEXT,state TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS git_commit_intents (id TEXT PRIMARY KEY,repo_dir TEXT NOT NULL,ref TEXT NOT NULL,parent_sha TEXT NOT NULL,tree_oid TEXT NOT NULL,message TEXT NOT NULL,candidate_digest TEXT NOT NULL,message_digest TEXT NOT NULL,snapshot_digest TEXT NOT NULL,policy_digest TEXT NOT NULL,verifier_digest TEXT NOT NULL,guard_digest TEXT NOT NULL,sha TEXT NOT NULL DEFAULT '',state TEXT NOT NULL,reason_code TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS pushes (id TEXT PRIMARY KEY,commit_job_id TEXT NOT NULL,remote_digest TEXT,owner TEXT,name TEXT,ref TEXT,commit_sha TEXT NOT NULL,state TEXT NOT NULL,local_only INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS remote_jobs (id TEXT PRIMARY KEY,repository_id TEXT NOT NULL,owner TEXT NOT NULL,name TEXT NOT NULL,alias TEXT NOT NULL,visibility TEXT NOT NULL,url TEXT NOT NULL,hosted_identity TEXT NOT NULL DEFAULT '',state TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS policies (id TEXT PRIMARY KEY,repository_id TEXT NOT NULL,decision TEXT,visibility TEXT,workflow TEXT,local_only INTEGER NOT NULL,public_consent INTEGER NOT NULL,revision INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY,repository_id TEXT NOT NULL,state TEXT,baseline_head TEXT,baseline_index TEXT,status_digest TEXT,baseline_paths_digest TEXT,client_id TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY,session_id TEXT NOT NULL,state TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS prompts (id TEXT PRIMARY KEY,task_id TEXT NOT NULL,kind TEXT,state TEXT,idempotency_key TEXT UNIQUE,blocking INTEGER NOT NULL,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS changesets (id TEXT PRIMARY KEY,task_id TEXT NOT NULL,base_sha TEXT,tree_digest TEXT,index_digest TEXT,state TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS verifications (id TEXT PRIMARY KEY,changeset_id TEXT NOT NULL,candidate_digest TEXT,policy_digest TEXT,verifier_digest TEXT,state TEXT,evidence_digest TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS leases (lease_key TEXT PRIMARY KEY,owner TEXT NOT NULL,expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS outbox (id TEXT PRIMARY KEY,kind TEXT NOT NULL,aggregate_id TEXT NOT NULL,payload BLOB NOT NULL,created_at INTEGER NOT NULL,published_at INTEGER);
CREATE TABLE IF NOT EXISTS audit (id TEXT PRIMARY KEY,repository_id TEXT,reason_code TEXT,metadata TEXT,prev_digest TEXT,digest TEXT,at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS outbox_pending ON outbox(published_at,created_at);
	CREATE TABLE IF NOT EXISTS state_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL);`)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := ensureCommitEvidenceColumns(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := ensureSessionBaselineColumns(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := ensureRemoteJobColumns(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if version < currentSchemaVersion {
		if version != 0 && version != 1 && version != 2 && version != 3 && version != 4 && version != 5 {
			_ = tx.Rollback()
			return fmt.Errorf("unsupported state schema version %d", version)
		}
		if _, err := tx.Exec(`INSERT INTO state_meta(key,value) VALUES('schema_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(currentSchemaVersion)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func ensureRemoteJobColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(remote_jobs)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !present["repository_id"] {
		if _, err := tx.Exec(`ALTER TABLE remote_jobs ADD COLUMN repository_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add remote_jobs.repository_id: %w", err)
		}
	}
	return nil
}

func ensureSessionBaselineColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range []string{"status_digest", "baseline_paths_digest"} {
		if present[column] {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add sessions.%s: %w", column, err)
		}
	}
	return nil
}

func ensureCommitEvidenceColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(commits)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, col := range []string{"policy_digest", "verifier_digest", "guard_digest"} {
		if present[col] {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE commits ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add commits.%s: %w", col, err)
		}
	}
	return nil
}
func (s *Store) IntegrityCheck() error {
	var result string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("state integrity check: %s", result)
	}
	return nil
}
func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = fn(&Tx{tx}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
func (t *Tx) PutCommitJob(j CommitJob) error {
	now := j.UpdatedAt
	if now == 0 {
		now = time.Now().UnixNano()
	}
	if j.CreatedAt == 0 {
		j.CreatedAt = now
	}
	var old CommitJob
	err := t.tx.QueryRow(`SELECT id,candidate_digest,base_sha,message_digest,policy_digest,verifier_digest,guard_digest,commit_sha,state,created_at,updated_at FROM commits WHERE id=?`, j.ID).Scan(&old.ID, &old.CandidateDigest, &old.BaseSHA, &old.MessageDigest, &old.PolicyDigest, &old.VerifierDigest, &old.GuardDigest, &old.CommitSHA, &old.State, &old.CreatedAt, &old.UpdatedAt)
	if err == nil && (old.CandidateDigest != j.CandidateDigest || old.BaseSHA != j.BaseSHA || old.MessageDigest != j.MessageDigest || old.PolicyDigest != j.PolicyDigest || old.VerifierDigest != j.VerifierDigest || old.GuardDigest != j.GuardDigest || old.CommitSHA != "" && j.CommitSHA != "" && old.CommitSHA != j.CommitSHA) {
		return errors.New("commit job identity conflict")
	}
	_, err = t.tx.Exec(`INSERT INTO commits(id,candidate_digest,base_sha,message_digest,policy_digest,verifier_digest,guard_digest,commit_sha,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET commit_sha=CASE WHEN excluded.commit_sha='' THEN commits.commit_sha ELSE excluded.commit_sha END,state=excluded.state,updated_at=excluded.updated_at`, j.ID, j.CandidateDigest, j.BaseSHA, j.MessageDigest, j.PolicyDigest, j.VerifierDigest, j.GuardDigest, j.CommitSHA, j.State, j.CreatedAt, now)
	return err
}
func (s *Store) CommitJob(id string) (CommitJob, error) {
	var j CommitJob
	err := s.db.QueryRow(`SELECT id,candidate_digest,base_sha,message_digest,policy_digest,verifier_digest,guard_digest,commit_sha,state,created_at,updated_at FROM commits WHERE id=?`, id).Scan(&j.ID, &j.CandidateDigest, &j.BaseSHA, &j.MessageDigest, &j.PolicyDigest, &j.VerifierDigest, &j.GuardDigest, &j.CommitSHA, &j.State, &j.CreatedAt, &j.UpdatedAt)
	return j, err
}

// RecordCommitJob performs the commit result transition in the same database
// transaction as its identity check. This prevents a second process from
// overwriting a result after a caller read the job state.
func (s *Store) RecordCommitJob(ctx context.Context, id, sha string) error {
	if !gitCommitSHARE.MatchString(sha) || id == "" {
		return ErrInvalidGitCommitSHA
	}
	return s.WithTx(ctx, func(tx *Tx) error {
		result, err := tx.tx.Exec(`UPDATE commits SET commit_sha=?,state=?,updated_at=? WHERE id=? AND (commit_sha='' OR commit_sha=?)`, sha, CommitCreated, time.Now().UnixNano(), id, sha)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			var exists int
			if err := tx.tx.QueryRow(`SELECT 1 FROM commits WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
				return os.ErrNotExist
			} else if err != nil {
				return err
			}
			return errors.New("commit result identity conflict")
		}
		return nil
	})
}

var (
	gitCommitSHARE     = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	gitIntentIDRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	gitRefRE           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	gitDigestRE        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	repositoryDigestRE = regexp.MustCompile(`^(?:sha256|hmac-sha256):[0-9a-f]{64}$`)
)

func (t *Tx) PutGitCommitIntent(i GitCommitIntent) error {
	if !gitIntentIDRE.MatchString(i.ID) {
		return ErrInvalidGitCommitIntent
	}
	var old GitCommitIntent
	err := t.tx.QueryRow(`SELECT id,repo_dir,ref,parent_sha,tree_oid,message,candidate_digest,message_digest,snapshot_digest,policy_digest,verifier_digest,guard_digest FROM git_commit_intents WHERE id=?`, i.ID).Scan(&old.ID, &old.RepoDir, &old.Ref, &old.ParentSHA, &old.TreeOID, &old.Message, &old.CandidateDigest, &old.MessageDigest, &old.SnapshotDigest, &old.PolicyDigest, &old.VerifierDigest, &old.GuardDigest)
	if err == nil {
		if old != i {
			return ErrGitCommitIntentConflict
		}
		if !validGitCommitIntent(i) {
			return ErrInvalidGitCommitIntent
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !validGitCommitIntent(i) {
		return ErrInvalidGitCommitIntent
	}
	now := time.Now().UnixNano()
	_, err = t.tx.Exec(`INSERT INTO git_commit_intents(id,repo_dir,ref,parent_sha,tree_oid,message,candidate_digest,message_digest,snapshot_digest,policy_digest,verifier_digest,guard_digest,sha,state,reason_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, i.ID, i.RepoDir, i.Ref, i.ParentSHA, i.TreeOID, i.Message, i.CandidateDigest, i.MessageDigest, i.SnapshotDigest, i.PolicyDigest, i.VerifierDigest, i.GuardDigest, "", CommitIntentRequested, "", now, now)
	return err
}

func validGitCommitIntent(i GitCommitIntent) bool {
	if !gitIntentIDRE.MatchString(i.ID) || !filepath.IsAbs(i.RepoDir) || strings.IndexByte(i.RepoDir, 0) >= 0 || filepath.Clean(i.RepoDir) != i.RepoDir || i.Ref != "refs/autogit/commits/"+i.ID || !gitCommitSHARE.MatchString(i.TreeOID) || (i.ParentSHA != "" && !gitCommitSHARE.MatchString(i.ParentSHA)) || len(i.Message) > 1<<20 || strings.IndexByte(i.Message, 0) >= 0 || !gitDigestRE.MatchString(i.CandidateDigest) || !gitDigestRE.MatchString(i.MessageDigest) || !gitDigestRE.MatchString(i.SnapshotDigest) || !gitDigestRE.MatchString(i.PolicyDigest) || !gitDigestRE.MatchString(i.VerifierDigest) || !gitDigestRE.MatchString(i.GuardDigest) {
		return false
	}
	return commit.Validate(i.Message) == nil
}

// PutCommitIntent is the concise transaction-level alias used by callers
// that already operate in the commit-intent namespace.
func (t *Tx) PutCommitIntent(i GitCommitIntent) error { return t.PutGitCommitIntent(i) }

func (s *Store) PutGitCommitIntent(ctx context.Context, i GitCommitIntent) error {
	return s.WithTx(ctx, func(tx *Tx) error { return tx.PutGitCommitIntent(i) })
}

func (s *Store) PutCommitIntent(ctx context.Context, i GitCommitIntent) error {
	return s.PutGitCommitIntent(ctx, i)
}

func (s *Store) GitCommitIntent(ctx context.Context, id string) (GitCommitIntent, error) {
	if !gitIntentIDRE.MatchString(id) {
		return GitCommitIntent{}, ErrInvalidGitCommitIntent
	}
	var i GitCommitIntent
	err := s.db.QueryRowContext(ctx, `SELECT id,repo_dir,ref,parent_sha,tree_oid,message,candidate_digest,message_digest,snapshot_digest,policy_digest,verifier_digest,guard_digest FROM git_commit_intents WHERE id=?`, id).Scan(&i.ID, &i.RepoDir, &i.Ref, &i.ParentSHA, &i.TreeOID, &i.Message, &i.CandidateDigest, &i.MessageDigest, &i.SnapshotDigest, &i.PolicyDigest, &i.VerifierDigest, &i.GuardDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return GitCommitIntent{}, os.ErrNotExist
	}
	return i, err
}

func (s *Store) GetGitCommitIntent(ctx context.Context, id string) (GitCommitIntent, error) {
	return s.GitCommitIntent(ctx, id)
}

func (s *Store) GetCommitIntent(ctx context.Context, id string) (GitCommitIntent, error) {
	return s.GitCommitIntent(ctx, id)
}

func (s *Store) GitCommitIntentRecord(ctx context.Context, id string) (GitCommitIntentRecord, error) {
	if !gitIntentIDRE.MatchString(id) {
		return GitCommitIntentRecord{}, ErrInvalidGitCommitIntent
	}
	var r GitCommitIntentRecord
	err := s.db.QueryRowContext(ctx, `SELECT id,repo_dir,ref,parent_sha,tree_oid,message,candidate_digest,message_digest,snapshot_digest,policy_digest,verifier_digest,guard_digest,sha,state,reason_code,created_at,updated_at FROM git_commit_intents WHERE id=?`, id).Scan(&r.Intent.ID, &r.Intent.RepoDir, &r.Intent.Ref, &r.Intent.ParentSHA, &r.Intent.TreeOID, &r.Intent.Message, &r.Intent.CandidateDigest, &r.Intent.MessageDigest, &r.Intent.SnapshotDigest, &r.Intent.PolicyDigest, &r.Intent.VerifierDigest, &r.Intent.GuardDigest, &r.SHA, &r.State, &r.ReasonCode, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return GitCommitIntentRecord{}, os.ErrNotExist
	}
	return r, err
}

func (s *Store) GetGitCommitIntentRecord(ctx context.Context, id string) (GitCommitIntentRecord, error) {
	return s.GitCommitIntentRecord(ctx, id)
}

func (s *Store) RecordGitCommit(ctx context.Context, id, sha string) error {
	if !gitIntentIDRE.MatchString(id) {
		return ErrInvalidGitCommitIntent
	}
	if !gitCommitSHARE.MatchString(sha) {
		return ErrInvalidGitCommitSHA
	}
	return s.WithTx(ctx, func(tx *Tx) error {
		var old string
		err := tx.tx.QueryRow(`SELECT sha FROM git_commit_intents WHERE id=?`, id).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			return os.ErrNotExist
		}
		if err != nil {
			return err
		}
		if old != "" && old != sha {
			return ErrGitCommitIntentConflict
		}
		_, err = tx.tx.Exec(`UPDATE git_commit_intents SET sha=?,state=?,reason_code='',updated_at=? WHERE id=?`, sha, CommitCreated, time.Now().UnixNano(), id)
		return err
	})
}

func (s *Store) RecordCommit(ctx context.Context, id, sha string) error {
	return s.RecordGitCommit(ctx, id, sha)
}

func (s *Store) RecordGitReconcile(ctx context.Context, id, reason string) error {
	if !gitIntentIDRE.MatchString(id) {
		return ErrInvalidGitCommitIntent
	}
	return s.WithTx(ctx, func(tx *Tx) error {
		var exists int
		if err := tx.tx.QueryRow(`SELECT 1 FROM git_commit_intents WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return os.ErrNotExist
		} else if err != nil {
			return err
		}
		_, err := tx.tx.Exec(`UPDATE git_commit_intents SET state=?,reason_code=?,updated_at=? WHERE id=?`, CommitIntentReconcile, stableReconcileCode(reason), time.Now().UnixNano(), id)
		return err
	})
}

func (s *Store) RecordReconcile(ctx context.Context, id, reason string) error {
	return s.RecordGitReconcile(ctx, id, reason)
}

func stableReconcileCode(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.HasPrefix(reason, "message file:"):
		return "MESSAGE_FILE_FAILED"
	case strings.HasPrefix(reason, "commit-tree outcome unknown"):
		return "COMMIT_OUTCOME_UNKNOWN"
	case strings.HasPrefix(reason, "invalid commit object identity"):
		return "INVALID_COMMIT_IDENTITY"
	case strings.HasPrefix(reason, "repository changed before ref update"):
		return "REPOSITORY_CHANGED"
	case strings.HasPrefix(reason, "inspect autogit ref:"):
		return "REF_INSPECTION_FAILED"
	case strings.HasPrefix(reason, "autogit ref already names"):
		return "REF_COLLISION"
	case strings.HasPrefix(reason, "ref update outcome unknown"):
		return "REF_UPDATE_UNKNOWN"
	case strings.HasPrefix(reason, "commit postcondition failed"):
		return "COMMIT_POSTCONDITION_FAILED"
	case strings.HasPrefix(reason, "commit evidence mismatch"):
		return "COMMIT_EVIDENCE_MISMATCH"
	case strings.HasPrefix(reason, "autogit ref is absent"):
		return "REF_ABSENT"
	}
	return "RECONCILE_REQUIRED"
}
func (t *Tx) PutPushJob(j PushJob) error {
	if !validPushJob(j) {
		return errors.New("invalid push job")
	}
	now := j.UpdatedAt
	if now == 0 {
		now = time.Now().UnixNano()
	}
	if j.CreatedAt == 0 {
		j.CreatedAt = now
	}
	var old PushJob
	var local int
	err := t.tx.QueryRow(`SELECT id,commit_job_id,remote_digest,owner,name,ref,commit_sha,state,local_only,created_at,updated_at FROM pushes WHERE id=?`, j.ID).Scan(&old.ID, &old.CommitJobID, &old.RemoteDigest, &old.Owner, &old.Name, &old.Ref, &old.CommitSHA, &old.State, &local, &old.CreatedAt, &old.UpdatedAt)
	if err == nil && (old.CommitJobID != j.CommitJobID || old.RemoteDigest != j.RemoteDigest || old.Owner != j.Owner || old.Name != j.Name || old.Ref != j.Ref || old.CommitSHA != j.CommitSHA || old.LocalOnly != (local != 0)) {
		return errors.New("push job identity conflict")
	}
	_, err = t.tx.Exec(`INSERT INTO pushes(id,commit_job_id,remote_digest,owner,name,ref,commit_sha,state,local_only,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,updated_at=excluded.updated_at`, j.ID, j.CommitJobID, j.RemoteDigest, j.Owner, j.Name, j.Ref, j.CommitSHA, j.State, j.LocalOnly, j.CreatedAt, now)
	return err
}

func validPushJob(j PushJob) bool {
	if !gitIntentIDRE.MatchString(j.ID) || !gitIntentIDRE.MatchString(j.Owner) || !gitIntentIDRE.MatchString(j.Name) || !gitRefRE.MatchString(j.Ref) || !gitCommitSHARE.MatchString(j.CommitSHA) {
		return false
	}
	if strings.Contains(j.Ref, "..") || strings.Contains(j.Ref, "@{") || strings.HasSuffix(strings.ToLower(j.Ref), ".lock") {
		return false
	}
	switch j.State {
	case PushRequested, PushRetryWait, PushSucceeded, PushBlocked, PushSkippedLocal:
		return true
	default:
		return false
	}
}
func (s *Store) PushJob(id string) (PushJob, error) {
	var j PushJob
	var local int
	err := s.db.QueryRow(`SELECT id,commit_job_id,remote_digest,owner,name,ref,commit_sha,state,local_only,created_at,updated_at FROM pushes WHERE id=?`, id).Scan(&j.ID, &j.CommitJobID, &j.RemoteDigest, &j.Owner, &j.Name, &j.Ref, &j.CommitSHA, &j.State, &local, &j.CreatedAt, &j.UpdatedAt)
	j.LocalOnly = local != 0
	return j, err
}

func (t *Tx) PutRemoteJob(j RemoteJob) error {
	if !validRemoteJob(j) {
		return errors.New("invalid remote job")
	}
	now := j.UpdatedAt
	if now == 0 {
		now = time.Now().UnixNano()
	}
	if j.CreatedAt == 0 {
		j.CreatedAt = now
	}
	var old RemoteJob
	err := t.tx.QueryRow(`SELECT id,repository_id,owner,name,alias,visibility,url,hosted_identity,state,created_at,updated_at FROM remote_jobs WHERE id=?`, j.ID).Scan(&old.ID, &old.RepositoryID, &old.Owner, &old.Name, &old.Alias, &old.Visibility, &old.URL, &old.HostedIdentity, &old.State, &old.CreatedAt, &old.UpdatedAt)
	if err == nil {
		if old.RepositoryID != j.RepositoryID || old.Owner != j.Owner || old.Name != j.Name || old.Alias != j.Alias || old.Visibility != j.Visibility || old.URL != j.URL {
			return errors.New("remote job identity conflict")
		}
		if j.HostedIdentity != "" && old.HostedIdentity != "" && j.HostedIdentity != old.HostedIdentity {
			return errors.New("remote hosted identity conflict")
		}
		_, err = t.tx.Exec(`UPDATE remote_jobs SET hosted_identity=CASE WHEN ?='' THEN hosted_identity ELSE ? END,state=?,updated_at=? WHERE id=?`, j.HostedIdentity, j.HostedIdentity, j.State, now, j.ID)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = t.tx.Exec(`INSERT INTO remote_jobs(id,repository_id,owner,name,alias,visibility,url,hosted_identity,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.RepositoryID, j.Owner, j.Name, j.Alias, j.Visibility, j.URL, j.HostedIdentity, j.State, j.CreatedAt, now)
	return err
}

func validRemoteJob(j RemoteJob) bool {
	if !gitIntentIDRE.MatchString(j.ID) || !repositoryDigestRE.MatchString(j.RepositoryID) || !gitIntentIDRE.MatchString(j.Owner) || !gitIntentIDRE.MatchString(j.Name) || !gitIntentIDRE.MatchString(j.Alias) || (j.Visibility != "private" && j.Visibility != "public") || j.URL != "https://github.com/"+j.Owner+"/"+j.Name+".git" || j.HostedIdentity != "" && j.HostedIdentity != j.Owner+"/"+j.Name {
		return false
	}
	switch j.State {
	case RemoteRequested, RemoteCreated, RemoteAttached, RemoteFailed:
		return true
	default:
		return false
	}
}

func (s *Store) RemoteJob(id string) (RemoteJob, error) {
	var j RemoteJob
	err := s.db.QueryRow(`SELECT id,repository_id,owner,name,alias,visibility,url,hosted_identity,state,created_at,updated_at FROM remote_jobs WHERE id=?`, id).Scan(&j.ID, &j.RepositoryID, &j.Owner, &j.Name, &j.Alias, &j.Visibility, &j.URL, &j.HostedIdentity, &j.State, &j.CreatedAt, &j.UpdatedAt)
	return j, err
}
func (t *Tx) EnqueueOutbox(o Outbox) error {
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().UnixNano()
	}
	_, err := t.tx.Exec(`INSERT INTO outbox(id,kind,aggregate_id,payload,created_at,published_at) VALUES(?,?,?,?,?,NULL) ON CONFLICT(id) DO NOTHING`, o.ID, o.Kind, o.AggregateID, o.Payload, o.CreatedAt)
	return err
}
func (s *Store) PendingOutbox(ctx context.Context, limit int) ([]Outbox, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,aggregate_id,payload,created_at,published_at FROM outbox WHERE published_at IS NULL ORDER BY created_at,id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbox
	for rows.Next() {
		var o Outbox
		if err := rows.Scan(&o.ID, &o.Kind, &o.AggregateID, &o.Payload, &o.CreatedAt, &o.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) MarkOutboxPublished(id string, at int64) error {
	_, err := s.db.Exec(`UPDATE outbox SET published_at=? WHERE id=? AND published_at IS NULL`, at, id)
	return err
}
func (s *Store) AcquireLease(ctx context.Context, l Lease, now int64) error {
	if l.Key == "" || l.Owner == "" || l.ExpiresAt <= now {
		return errors.New("invalid lease")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	var owner string
	var exp int64
	err = conn.QueryRowContext(ctx, `SELECT owner,expires_at FROM leases WHERE lease_key=?`, l.Key).Scan(&owner, &exp)
	if err == nil && exp > now && owner != l.Owner {
		return errors.New("lease held")
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO leases(lease_key,owner,expires_at) VALUES(?,?,?) ON CONFLICT(lease_key) DO UPDATE SET owner=excluded.owner,expires_at=excluded.expires_at`, l.Key, l.Owner, l.ExpiresAt)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
func (s *Store) ReleaseLease(key, owner string) error {
	_, err := s.db.Exec(`DELETE FROM leases WHERE lease_key=? AND owner=?`, key, owner)
	return err
}
func (s *Store) Audit(e AuditEvent) error {
	if e.At == 0 {
		e.At = time.Now().UnixNano()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev string
	_ = s.db.QueryRow(`SELECT digest FROM audit ORDER BY at DESC,id DESC LIMIT 1`).Scan(&prev)
	e.PrevDigest = prev
	h := sha256.Sum256([]byte(e.ID + "\x00" + e.RepositoryID + "\x00" + e.ReasonCode + "\x00" + e.Metadata + "\x00" + prev))
	e.Digest = "sha256:" + hex.EncodeToString(h[:])
	_, err := s.db.Exec(`INSERT INTO audit(id,repository_id,reason_code,metadata,prev_digest,digest,at) VALUES(?,?,?,?,?,?,?)`, e.ID, e.RepositoryID, e.ReasonCode, e.Metadata, e.PrevDigest, e.Digest, e.At)
	return err
}
func (s *Store) AuditEvents() ([]AuditEvent, error) {
	rows, err := s.db.Query(`SELECT id,repository_id,reason_code,metadata,prev_digest,digest,at FROM audit ORDER BY at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.RepositoryID, &e.ReasonCode, &e.Metadata, &e.PrevDigest, &e.Digest, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (t *Tx) PutPolicy(p Policy) error {
	_, err := t.tx.Exec(`INSERT INTO policies(id,repository_id,decision,visibility,workflow,local_only,public_consent,revision) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET decision=excluded.decision,visibility=excluded.visibility,workflow=excluded.workflow,local_only=excluded.local_only,public_consent=excluded.public_consent,revision=excluded.revision`, p.ID, p.RepositoryID, p.Decision, p.Visibility, p.Workflow, p.LocalOnly, p.PublicConsent, p.Revision)
	return err
}
func (s *Store) Policy(id string) (Policy, error) {
	var p Policy
	var l, pc int
	err := s.db.QueryRow(`SELECT id,repository_id,decision,visibility,workflow,local_only,public_consent,revision FROM policies WHERE id=?`, id).Scan(&p.ID, &p.RepositoryID, &p.Decision, &p.Visibility, &p.Workflow, &l, &pc, &p.Revision)
	p.LocalOnly = l != 0
	p.PublicConsent = pc != 0
	return p, err
}
func (t *Tx) PutSession(x Session) error {
	_, err := t.tx.Exec(`INSERT INTO sessions(id,repository_id,state,baseline_head,baseline_index,status_digest,baseline_paths_digest,client_id,revision) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,baseline_head=excluded.baseline_head,baseline_index=excluded.baseline_index,status_digest=excluded.status_digest,baseline_paths_digest=excluded.baseline_paths_digest,revision=excluded.revision`, x.ID, x.RepositoryID, x.State, x.BaselineHead, x.BaselineIndex, x.StatusDigest, x.BaselinePathsDigest, x.ClientID, x.Revision)
	return err
}

func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	var x Session
	err := s.db.QueryRowContext(ctx, `SELECT id,repository_id,state,baseline_head,baseline_index,status_digest,baseline_paths_digest,client_id,revision FROM sessions WHERE id=?`, id).Scan(&x.ID, &x.RepositoryID, &x.State, &x.BaselineHead, &x.BaselineIndex, &x.StatusDigest, &x.BaselinePathsDigest, &x.ClientID, &x.Revision)
	return x, err
}

// RecordSessionBaseline durably records only baseline identity and digests.
// The source bytes and raw changed paths remain in the caller's in-memory
// observation and are never written to the state database.
func (s *Store) RecordSessionBaseline(ctx context.Context, sessionID, repositoryID, clientID string, baseline repository.Baseline) error {
	if sessionID == "" || repositoryID == "" || clientID == "" || !validBaselineDigest(baseline.IndexDigest) || !validBaselineDigest(baseline.StatusDigest) || !validBaselineDigest(baseline.PathsDigest) || (baseline.Head != "" && !validObjectID(baseline.Head)) {
		return errors.New("invalid session baseline")
	}
	return s.WithTx(ctx, func(tx *Tx) error {
		var old Session
		err := tx.tx.QueryRow(`SELECT id,repository_id,state,baseline_head,baseline_index,status_digest,baseline_paths_digest,client_id,revision FROM sessions WHERE id=?`, sessionID).Scan(&old.ID, &old.RepositoryID, &old.State, &old.BaselineHead, &old.BaselineIndex, &old.StatusDigest, &old.BaselinePathsDigest, &old.ClientID, &old.Revision)
		if err == nil {
			if old.RepositoryID != repositoryID || old.ClientID != clientID || old.BaselineHead != baseline.Head || old.BaselineIndex != baseline.IndexDigest || old.StatusDigest != baseline.StatusDigest || old.BaselinePathsDigest != baseline.PathsDigest {
				return errors.New("session baseline identity conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return tx.PutSession(Session{ID: sessionID, RepositoryID: repositoryID, State: "ACTIVE", BaselineHead: baseline.Head, BaselineIndex: baseline.IndexDigest, StatusDigest: baseline.StatusDigest, BaselinePathsDigest: baseline.PathsDigest, ClientID: clientID, Revision: 1})
	})
}

func validBaselineDigest(value string) bool {
	return gitDigestRE.MatchString(value)
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
func (t *Tx) PutTask(x Task) error {
	_, err := t.tx.Exec(`INSERT INTO tasks(id,session_id,state,revision) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,revision=excluded.revision`, x.ID, x.SessionID, x.State, x.Revision)
	return err
}
func (t *Tx) PutPrompt(x Prompt) error {
	_, err := t.tx.Exec(`INSERT INTO prompts(id,task_id,kind,state,idempotency_key,blocking,revision) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,revision=excluded.revision`, x.ID, x.TaskID, x.Kind, x.State, x.IdempotencyKey, x.Blocking, x.Revision)
	return err
}
func (t *Tx) PutChangeSet(x ChangeSet) error {
	_, err := t.tx.Exec(`INSERT INTO changesets(id,task_id,base_sha,tree_digest,index_digest,state,revision) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,revision=excluded.revision`, x.ID, x.TaskID, x.BaseSHA, x.TreeDigest, x.IndexDigest, x.State, x.Revision)
	return err
}
func (t *Tx) PutVerification(x VerificationJob) error {
	_, err := t.tx.Exec(`INSERT INTO verifications(id,changeset_id,candidate_digest,policy_digest,verifier_digest,state,evidence_digest,revision) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,evidence_digest=excluded.evidence_digest,revision=excluded.revision`, x.ID, x.ChangeSetID, x.CandidateDigest, x.PolicyDigest, x.VerifierDigest, x.State, x.EvidenceDigest, x.Revision)
	return err
}
func JSON(v any) []byte { b, _ := json.Marshal(v); return b }
