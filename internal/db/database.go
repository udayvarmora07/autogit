// Package db owns AutoGit's SQLite connection, migration, and trust-boundary
// policy. State and event repositories must use this package instead of
// opening the shared database independently.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	RequiredSQLiteVersion = "3.51.3"
	CurrentSchemaVersion  = 7
	openTimeout           = 15 * time.Second
	busyTimeoutMS         = 5000
)

var ErrUnsafePath = errors.New("unsafe database path")

// Open opens the shared state database, applies the connection contract, and
// runs all durable schema migrations. It is the only production database
// opener used by both state and event repositories.
func Open(path string) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	return OpenContext(ctx, path)
}

func OpenContext(ctx context.Context, path string) (*sql.DB, error) {
	if ctx == nil {
		return nil, errors.New("database context is required")
	}
	absolute, err := prepareWritablePath(path)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", writableDSN(absolute))
	if err != nil {
		return nil, err
	}
	configurePool(database)
	if err := configureSQLite(ctx, database); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := migrate(ctx, database); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := restrictDatabaseArtifacts(absolute); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

// OpenReadOnly opens an existing database without creating files or applying
// migrations. It is intended for diagnostics and read-only command paths.
func OpenReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	if ctx == nil {
		return nil, errors.New("database context is required")
	}
	absolute, err := prepareExistingPath(path)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", readOnlyDSN(absolute))
	if err != nil {
		return nil, err
	}
	configurePool(database)
	if _, err := database.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func configurePool(database *sql.DB) {
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(0)
}

func configureSQLite(ctx context.Context, database *sql.DB) error {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		var journalMode string
		lastErr = func() error {
			if _, err := database.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA wal_autocheckpoint=1000;`); err != nil {
				return err
			}
			if err := database.QueryRowContext(ctx, `PRAGMA journal_mode=WAL`).Scan(&journalMode); err != nil {
				return err
			}
			if !strings.EqualFold(journalMode, "wal") {
				return fmt.Errorf("SQLite journal mode is %q, want WAL", journalMode)
			}
			return nil
		}()
		if lastErr == nil {
			break
		}
		if !isBusy(lastErr) {
			return lastErr
		}
		if err := waitBusy(ctx, attempt); err != nil {
			return err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	var version string
	if err := database.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&version); err != nil {
		return err
	}
	if !atLeastVersion(version, RequiredSQLiteVersion) {
		return fmt.Errorf("SQLite version %s is older than required %s", version, RequiredSQLiteVersion)
	}
	return nil
}

func migrate(ctx context.Context, database *sql.DB) error {
	for attempt := 0; attempt < 8; attempt++ {
		err := migrateOnce(ctx, database)
		if err == nil || !isBusy(err) {
			return err
		}
		if err := waitBusy(ctx, attempt); err != nil {
			return err
		}
	}
	return errors.New("SQLite migration remained busy after retries")
}

func waitBusy(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(25*(1<<attempt)) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func migrateOnce(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS state_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL);`); err != nil {
		return err
	}
	version, err := schemaVersion(ctx, tx)
	if err != nil {
		return err
	}
	if version > CurrentSchemaVersion {
		return fmt.Errorf("unsupported state schema version %d", version)
	}
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	if err := ensureColumns(ctx, tx); err != nil {
		return err
	}
	if version != 0 && version < 1 {
		return fmt.Errorf("unsupported state schema version %d", version)
	}
	if version < CurrentSchemaVersion {
		if _, err := tx.ExecContext(ctx, `INSERT INTO state_meta(key,value) VALUES('schema_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(CurrentSchemaVersion)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func schemaVersion(ctx context.Context, tx *sql.Tx) (int, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("unsupported state schema version %q", raw)
	}
	return version, nil
}

func ensureColumns(ctx context.Context, tx *sql.Tx) error {
	if err := ensureTableColumns(ctx, tx, "commits", map[string]string{
		"policy_digest":   "TEXT NOT NULL DEFAULT ''",
		"verifier_digest": "TEXT NOT NULL DEFAULT ''",
		"guard_digest":    "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := ensureTableColumns(ctx, tx, "sessions", map[string]string{
		"status_digest":         "TEXT NOT NULL DEFAULT ''",
		"baseline_paths_digest": "TEXT NOT NULL DEFAULT ''",
		"baseline_evidence":     "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := ensureTableColumns(ctx, tx, "audit_events", map[string]string{
		"repository_id": "TEXT NOT NULL DEFAULT ''",
		"disposition":   "TEXT NOT NULL DEFAULT ''",
		"reason_code":   "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	return ensureTableColumns(ctx, tx, "remote_jobs", map[string]string{
		"repository_id": "TEXT NOT NULL DEFAULT ''",
	})
}

func ensureTableColumns(ctx context.Context, tx *sql.Tx, table string, wanted map[string]string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
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
	for name, definition := range wanted {
		if present[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition); err != nil {
			return fmt.Errorf("add %s.%s: %w", table, name, err)
		}
	}
	return nil
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "sqlite_busy")
}

func prepareWritablePath(path string) (string, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", fmt.Errorf("state directory: %w", err)
	}
	if info, err := os.Lstat(parent); err != nil {
		return "", err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("%w: state directory is not a real directory", ErrUnsafePath)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		// A caller-created, non-sticky state root can be tightened in place.
		// Never chmod a shared sticky directory such as /tmp.
		if info.Mode()&01000 != 0 || os.Chmod(parent, 0700) != nil {
			return "", fmt.Errorf("%w: state directory permissions are too broad (%o)", ErrUnsafePath, info.Mode().Perm())
		}
	}
	if err := verifyDirectory(parent); err != nil {
		return "", err
	}
	if info, err := os.Lstat(absolute); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: database file is not a regular file", ErrUnsafePath)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return "", fmt.Errorf("%w: database permissions are too broad", ErrUnsafePath)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	} else {
		file, createErr := os.OpenFile(absolute, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil && !errors.Is(createErr, fs.ErrExist) {
			return "", createErr
		}
		if createErr == nil {
			if closeErr := file.Close(); closeErr != nil {
				return "", closeErr
			}
		}
		info, statErr := os.Lstat(absolute)
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: database file is not a regular file", ErrUnsafePath)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return "", fmt.Errorf("%w: database permissions are too broad", ErrUnsafePath)
		}
	}
	return absolute, nil
}

func prepareExistingPath(path string) (string, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	if err := verifyDirectory(filepath.Dir(absolute)); err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: database file is not a regular file", ErrUnsafePath)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("%w: database permissions are too broad", ErrUnsafePath)
	}
	return absolute, nil
}

func absolutePath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("%w: database path is empty or contains NUL", ErrUnsafePath)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func verifyDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: state directory is not a real directory", ErrUnsafePath)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: state directory permissions are too broad (%o)", ErrUnsafePath, info.Mode().Perm())
	}
	return nil
}

func restrictDatabaseArtifacts(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		artifact := path + suffix
		info, err := os.Lstat(artifact)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: database artifact is not a regular file", ErrUnsafePath)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(artifact, 0600); err != nil {
				return err
			}
		}
	}
	return nil
}

func writableDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
}

func readOnlyDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro&_query_only=true&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
}

func atLeastVersion(got, want string) bool {
	var gMajor, gMinor, gPatch int
	var wMajor, wMinor, wPatch int
	if _, err := fmt.Sscanf(got, "%d.%d.%d", &gMajor, &gMinor, &gPatch); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(want, "%d.%d.%d", &wMajor, &wMinor, &wPatch); err != nil {
		return false
	}
	if gMajor != wMajor {
		return gMajor > wMajor
	}
	if gMinor != wMinor {
		return gMinor > wMinor
	}
	return gPatch >= wPatch
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS commits (id TEXT PRIMARY KEY,candidate_digest TEXT NOT NULL,base_sha TEXT NOT NULL,message_digest TEXT NOT NULL,policy_digest TEXT NOT NULL DEFAULT '',verifier_digest TEXT NOT NULL DEFAULT '',guard_digest TEXT NOT NULL DEFAULT '',commit_sha TEXT,state TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS git_commit_intents (id TEXT PRIMARY KEY,repo_dir TEXT NOT NULL,ref TEXT NOT NULL,parent_sha TEXT NOT NULL,tree_oid TEXT NOT NULL,message TEXT NOT NULL,candidate_digest TEXT NOT NULL,message_digest TEXT NOT NULL,snapshot_digest TEXT NOT NULL,policy_digest TEXT NOT NULL,verifier_digest TEXT NOT NULL,guard_digest TEXT NOT NULL,sha TEXT NOT NULL DEFAULT '',state TEXT NOT NULL,reason_code TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS pushes (id TEXT PRIMARY KEY,commit_job_id TEXT NOT NULL,remote_digest TEXT,owner TEXT,name TEXT,ref TEXT,commit_sha TEXT NOT NULL,state TEXT NOT NULL,local_only INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS remote_jobs (id TEXT PRIMARY KEY,repository_id TEXT NOT NULL,owner TEXT NOT NULL,name TEXT NOT NULL,alias TEXT NOT NULL,visibility TEXT NOT NULL,url TEXT NOT NULL,hosted_identity TEXT NOT NULL DEFAULT '',state TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS policies (id TEXT PRIMARY KEY,repository_id TEXT NOT NULL,decision TEXT,visibility TEXT,workflow TEXT,local_only INTEGER NOT NULL,public_consent INTEGER NOT NULL,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY,repository_id TEXT NOT NULL,state TEXT,baseline_head TEXT,baseline_index TEXT,status_digest TEXT,baseline_paths_digest TEXT,baseline_evidence TEXT NOT NULL DEFAULT '',client_id TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY,session_id TEXT NOT NULL,state TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS prompts (id TEXT PRIMARY KEY,task_id TEXT NOT NULL,kind TEXT,state TEXT,idempotency_key TEXT UNIQUE,blocking INTEGER NOT NULL,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS changesets (id TEXT PRIMARY KEY,task_id TEXT NOT NULL,base_sha TEXT,tree_digest TEXT,index_digest TEXT,state TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS verifications (id TEXT PRIMARY KEY,changeset_id TEXT NOT NULL,candidate_digest TEXT,policy_digest TEXT,verifier_digest TEXT,state TEXT,evidence_digest TEXT,revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS leases (lease_key TEXT PRIMARY KEY,owner TEXT NOT NULL,expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS outbox (id TEXT PRIMARY KEY,kind TEXT NOT NULL,aggregate_id TEXT NOT NULL,payload BLOB NOT NULL,created_at INTEGER NOT NULL,published_at INTEGER);
CREATE TABLE IF NOT EXISTS audit (id TEXT PRIMARY KEY,repository_id TEXT,reason_code TEXT,metadata TEXT,prev_digest TEXT,digest TEXT,at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS outbox_pending ON outbox(published_at,created_at);
CREATE TABLE IF NOT EXISTS event_receipts (event_id TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE, payload_digest TEXT NOT NULL, disposition TEXT NOT NULL, revision INTEGER NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS event_revision_sequence (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL);
INSERT OR IGNORE INTO event_revision_sequence(id,revision) VALUES(1,COALESCE((SELECT MAX(revision) FROM event_receipts),0));
CREATE TABLE IF NOT EXISTS pending_events (event_id TEXT PRIMARY KEY, causation_id TEXT NOT NULL, payload BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS audit_events (revision INTEGER PRIMARY KEY AUTOINCREMENT, repository_id TEXT NOT NULL DEFAULT '', disposition TEXT NOT NULL DEFAULT '', reason_code TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS lifecycle_projections (repository_id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state BLOB NOT NULL, updated_at TEXT NOT NULL);
INSERT OR IGNORE INTO state_meta(key,value) VALUES('schema_version','7');
`
