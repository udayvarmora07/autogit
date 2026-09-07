package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"autogit/internal/securefs"
)

// HealthReport is a redacted, read-only view of the SQLite durability
// contract. It contains no database paths or application payloads.
type HealthReport struct {
	SQLiteVersion     string `json:"sqlite_version"`
	JournalMode       string `json:"journal_mode"`
	Synchronous       int    `json:"synchronous"`
	ForeignKeys       bool   `json:"foreign_keys"`
	BusyTimeout       int    `json:"busy_timeout"`
	WALAutoCheckpoint int    `json:"wal_autocheckpoint"`
	SchemaVersion     int    `json:"schema_version"`
	IntegrityOK       bool   `json:"integrity_ok"`
	ForeignKeysOK     bool   `json:"foreign_keys_ok"`
}

type BackupReport struct {
	Bytes       int64 `json:"bytes"`
	IntegrityOK bool  `json:"integrity_ok"`
}

type RestoreReport struct {
	Bytes       int64 `json:"bytes"`
	IntegrityOK bool  `json:"integrity_ok"`
}

type RepairReport struct {
	IntegrityOK   bool `json:"integrity_ok"`
	ForeignKeysOK bool `json:"foreign_keys_ok"`
	Checkpointed  bool `json:"checkpointed"`
}

// RetentionPolicy controls explicit, operator-requested maintenance. A zero
// age/row limit leaves that class untouched. Pending events, pending outbox
// effects, and active leases are never pruned.
type RetentionPolicy struct {
	Now            time.Time
	MaxAuditAge    time.Duration
	MaxAuditRows   int
	ReceiptAge     time.Duration
	MaxReceiptRows int
	MaxOutboxAge   time.Duration
	Compact        bool
}

type RetentionReport struct {
	AuditEventsDeleted int64 `json:"audit_events_deleted"`
	AuditCompacted     int64 `json:"audit_compacted"`
	ReceiptsCompacted  int64 `json:"receipts_compacted"`
	OutboxDeleted      int64 `json:"outbox_deleted"`
	LeasesDeleted      int64 `json:"leases_deleted"`
	Vacuumed           bool  `json:"vacuumed"`
}

// Inspect opens an existing database read-only. It never creates files or
// upgrades schema, which makes it safe for doctor and support diagnostics.
func Inspect(ctx context.Context, path string) (HealthReport, error) {
	database, err := OpenReadOnly(ctx, path)
	if err != nil {
		return HealthReport{}, err
	}
	defer database.Close()
	return inspectDatabase(ctx, database)
}

// Export returns bounded structural diagnostics only. It deliberately omits
// payloads, metadata, repository identities, paths, and event arguments.
func Export(ctx context.Context, path string) ([]byte, error) {
	database, err := OpenReadOnly(ctx, path)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	health, err := inspectDatabase(ctx, database)
	if err != nil {
		return nil, err
	}
	tables := []string{"commits", "git_commit_intents", "pushes", "remote_jobs", "policies", "sessions", "tasks", "prompts", "changesets", "verifications", "leases", "outbox", "audit", "event_receipts", "event_receipt_tombstones", "pending_events", "audit_events", "lifecycle_projections"}
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			return nil, err
		}
		counts[table] = count
	}
	value := map[string]any{
		"schema_version":  health.SchemaVersion,
		"sqlite_version":  health.SQLiteVersion,
		"integrity_ok":    health.IntegrityOK,
		"foreign_keys_ok": health.ForeignKeysOK,
		"table_counts":    counts,
	}
	return json.Marshal(value)
}

// Repair performs only non-destructive maintenance. Corruption is reported,
// never silently rewritten; replacement from a known-good Backup is the
// supported recovery path.
func Repair(ctx context.Context, path string) (RepairReport, error) {
	database, err := OpenContext(ctx, path)
	if err != nil {
		return RepairReport{}, err
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE); PRAGMA optimize`); err != nil && !isBusy(err) {
		return RepairReport{}, err
	}
	health, err := inspectDatabase(ctx, database)
	if err != nil {
		return RepairReport{IntegrityOK: health.IntegrityOK, ForeignKeysOK: health.ForeignKeysOK}, err
	}
	return RepairReport{IntegrityOK: health.IntegrityOK, ForeignKeysOK: health.ForeignKeysOK, Checkpointed: true}, nil
}

func inspectDatabase(ctx context.Context, database *sql.DB) (HealthReport, error) {
	var report HealthReport
	if err := database.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&report.SQLiteVersion); err != nil {
		return report, err
	}
	if err := database.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&report.JournalMode); err != nil {
		return report, err
	}
	if err := database.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&report.Synchronous); err != nil {
		return report, err
	}
	var foreignKeys int
	if err := database.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		return report, err
	}
	report.ForeignKeys = foreignKeys == 1
	if err := database.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&report.BusyTimeout); err != nil {
		return report, err
	}
	if err := database.QueryRowContext(ctx, `PRAGMA wal_autocheckpoint`).Scan(&report.WALAutoCheckpoint); err != nil {
		return report, err
	}
	var rawVersion string
	if err := database.QueryRowContext(ctx, `SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&rawVersion); err != nil {
		return report, err
	}
	version, err := parseSchemaVersion(rawVersion)
	if err != nil {
		return report, err
	}
	report.SchemaVersion = version
	var integrity string
	if err := database.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return report, err
	}
	report.IntegrityOK = integrity == "ok"
	rows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	report.ForeignKeysOK = true
	for rows.Next() {
		report.ForeignKeysOK = false
		var table, rowID, parent string
		var foreignKeyID int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return report, err
		}
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	if !report.IntegrityOK || !report.ForeignKeysOK {
		return report, fmt.Errorf("SQLite integrity check failed")
	}
	return report, nil
}

func parseSchemaVersion(raw string) (int, error) {
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 || version > CurrentSchemaVersion {
		return 0, fmt.Errorf("unsupported state schema version %q", raw)
	}
	return version, nil
}

// Backup produces a standalone, checkpointed SQLite database using VACUUM
// INTO. The destination is created atomically and existing/symlinked targets
// are never overwritten.
func Backup(ctx context.Context, sourcePath, destinationPath string) (BackupReport, error) {
	source, err := OpenContext(ctx, sourcePath)
	if err != nil {
		return BackupReport{}, err
	}
	defer source.Close()
	destination, err := prepareNewMaintenancePath(destinationPath)
	if err != nil {
		return BackupReport{}, err
	}
	temporary, cleanup, err := allocateMaintenancePath(destination)
	if err != nil {
		return BackupReport{}, err
	}
	published := false
	defer func() {
		if !published {
			cleanup()
		}
	}()
	if _, err := source.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil && !isBusy(err) {
		return BackupReport{}, err
	}
	if _, err := source.ExecContext(ctx, vacuumIntoSQL(temporary)); err != nil {
		return BackupReport{}, err
	}
	if err := os.Chmod(temporary, 0600); err != nil && runtime.GOOS != "windows" {
		return BackupReport{}, err
	}
	report, err := Inspect(ctx, temporary)
	if err != nil {
		return BackupReport{}, fmt.Errorf("validate backup: %w", err)
	}
	if err := publishMaintenancePath(temporary, destination); err != nil {
		return BackupReport{}, err
	}
	published = true
	info, err := os.Stat(destination)
	if err != nil {
		return BackupReport{}, err
	}
	return BackupReport{Bytes: info.Size(), IntegrityOK: report.IntegrityOK && report.ForeignKeysOK}, nil
}

// Restore validates a standalone backup before replacing the destination via
// an atomic same-directory rename. It never opens or migrates the destination
// before the validated copy is ready.
func Restore(ctx context.Context, backupPath, destinationPath string) (RestoreReport, error) {
	if _, err := Inspect(ctx, backupPath); err != nil {
		return RestoreReport{}, fmt.Errorf("validate restore source: %w", err)
	}
	source, err := prepareExistingPath(backupPath)
	if err != nil {
		return RestoreReport{}, err
	}
	destination, err := prepareRestorePath(destinationPath)
	if err != nil {
		return RestoreReport{}, err
	}
	temporary, cleanup, err := allocateMaintenancePath(destination)
	if err != nil {
		return RestoreReport{}, err
	}
	published := false
	defer func() {
		if !published {
			cleanup()
		}
	}()
	bytes, err := copyMaintenanceFile(source, temporary)
	if err != nil {
		return RestoreReport{}, err
	}
	if _, err := Inspect(ctx, temporary); err != nil {
		return RestoreReport{}, fmt.Errorf("validate restored copy: %w", err)
	}
	if err := replaceMaintenancePath(temporary, destination); err != nil {
		return RestoreReport{}, err
	}
	published = true
	return RestoreReport{Bytes: bytes, IntegrityOK: true}, nil
}

// Retain prunes only explicitly selected, non-active records and then runs a
// bounded SQLite compaction. Receipt tombstones preserve idempotency identity
// after payload-free receipts leave the hot table.
func Retain(ctx context.Context, path string, policy RetentionPolicy) (RetentionReport, error) {
	database, err := OpenContext(ctx, path)
	if err != nil {
		return RetentionReport{}, err
	}
	defer database.Close()
	now := policy.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	report := RetentionReport{}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if policy.MaxAuditAge > 0 {
		cutoff := now.Add(-policy.MaxAuditAge).UTC().Format(time.RFC3339Nano)
		result, execErr := tx.ExecContext(ctx, `DELETE FROM audit_events WHERE created_at < ? AND revision NOT IN (SELECT revision FROM event_receipts WHERE disposition='pending')`, cutoff)
		if execErr != nil {
			return report, execErr
		}
		report.AuditEventsDeleted += rowsAffected(result)
	}
	if policy.MaxAuditRows > 0 {
		result, execErr := tx.ExecContext(ctx, `DELETE FROM audit_events WHERE revision NOT IN (SELECT revision FROM audit_events ORDER BY revision DESC LIMIT ?) AND revision NOT IN (SELECT revision FROM event_receipts WHERE disposition='pending')`, policy.MaxAuditRows)
		if execErr != nil {
			return report, execErr
		}
		report.AuditEventsDeleted += rowsAffected(result)
	}
	if policy.ReceiptAge > 0 || policy.MaxReceiptRows > 0 {
		where, args := receiptRetentionPredicate(now, policy)
		if _, execErr := tx.ExecContext(ctx, `INSERT OR IGNORE INTO event_receipt_tombstones(event_id,idempotency_key,payload_digest,disposition,revision,created_at) SELECT event_id,idempotency_key,payload_digest,disposition,revision,created_at FROM event_receipts WHERE `+where, args...); execErr != nil { // #nosec G202 -- where is assembled only from fixed predicates; all values are bound.
			return report, execErr
		}
		result, execErr := tx.ExecContext(ctx, `DELETE FROM event_receipts WHERE `+where, args...) // #nosec G202 -- where is assembled only from fixed predicates; all values are bound.
		if execErr != nil {
			return report, execErr
		}
		report.ReceiptsCompacted = rowsAffected(result)
	}
	if policy.MaxOutboxAge > 0 {
		cutoff := now.Add(-policy.MaxOutboxAge).UnixNano()
		result, execErr := tx.ExecContext(ctx, `DELETE FROM outbox WHERE published_at IS NOT NULL AND created_at < ?`, cutoff)
		if execErr != nil {
			return report, execErr
		}
		report.OutboxDeleted = rowsAffected(result)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM leases WHERE expires_at <= ?`, now.UnixNano())
	if err != nil {
		return report, err
	}
	report.LeasesDeleted = rowsAffected(result)
	if policy.Compact {
		report.AuditCompacted, err = compactAudit(ctx, tx, now, policy)
		if err != nil {
			return report, err
		}
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	if policy.Compact {
		if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE); PRAGMA optimize; VACUUM`); err != nil {
			return report, err
		}
		report.Vacuumed = true
	}
	return report, nil
}

func receiptRetentionPredicate(now time.Time, policy RetentionPolicy) (string, []any) {
	clauses := []string{"disposition <> 'pending'", "event_id NOT IN (SELECT event_id FROM pending_events)"}
	args := []any{}
	if policy.ReceiptAge > 0 {
		clauses = append(clauses, "created_at < ?")
		args = append(args, now.Add(-policy.ReceiptAge).UTC().Format(time.RFC3339Nano))
	}
	if policy.MaxReceiptRows > 0 {
		clauses = append(clauses, "event_id NOT IN (SELECT event_id FROM event_receipts ORDER BY revision DESC LIMIT ?)")
		args = append(args, policy.MaxReceiptRows)
	}
	return strings.Join(clauses, " AND "), args
}

func compactAudit(ctx context.Context, tx *sql.Tx, now time.Time, policy RetentionPolicy) (int64, error) {
	if policy.MaxAuditAge <= 0 && policy.MaxAuditRows <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-policy.MaxAuditAge).UnixNano()
	var count int64
	var lastDigest string
	query := `SELECT count(*),COALESCE((SELECT digest FROM audit ORDER BY at DESC,id DESC LIMIT 1),'') FROM audit WHERE at < ?`
	if policy.MaxAuditAge <= 0 {
		query = `SELECT count(*),COALESCE((SELECT digest FROM audit ORDER BY at DESC,id DESC LIMIT 1),'') FROM audit WHERE id NOT IN (SELECT id FROM audit ORDER BY at DESC LIMIT ?)`
		if err := tx.QueryRowContext(ctx, query, policy.MaxAuditRows).Scan(&count, &lastDigest); err != nil {
			return 0, err
		}
	} else if err := tx.QueryRowContext(ctx, query, cutoff).Scan(&count, &lastDigest); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}
	metadata, _ := json.Marshal(map[string]any{"deleted": count, "last_digest": lastDigest})
	id := fmt.Sprintf("retention-%d", now.UnixNano())
	hash := sha256.Sum256([]byte(id + "\x00RETENTION_COMPACTED\x00" + string(metadata)))
	digest := "sha256:" + hex.EncodeToString(hash[:])
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO audit(id,repository_id,reason_code,metadata,prev_digest,digest,at) VALUES(?,?,?,?,?,?,?)`, id, "", "RETENTION_COMPACTED", string(metadata), "", digest, now.UnixNano()); err != nil {
		return 0, err
	}
	var result sql.Result
	var err error
	if policy.MaxAuditAge > 0 {
		result, err = tx.ExecContext(ctx, `DELETE FROM audit WHERE at < ? AND id <> ?`, cutoff, id)
	} else {
		result, err = tx.ExecContext(ctx, `DELETE FROM audit WHERE id NOT IN (SELECT id FROM audit ORDER BY at DESC LIMIT ?) AND id <> ?`, policy.MaxAuditRows, id)
	}
	if err != nil {
		return 0, err
	}
	return rowsAffected(result), nil
}

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	count, _ := result.RowsAffected()
	return count
}

func vacuumIntoSQL(path string) string {
	return `VACUUM INTO '` + strings.ReplaceAll(path, "'", "''") + `'`
}

func prepareNewMaintenancePath(path string) (string, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	if err := ensureMaintenanceParent(filepath.Dir(absolute)); err != nil {
		return "", err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return "", fmt.Errorf("maintenance destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return absolute, nil
}

func prepareRestorePath(path string) (string, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	if err := ensureMaintenanceParent(filepath.Dir(absolute)); err != nil {
		return "", err
	}
	if info, err := os.Lstat(absolute); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !sameOwnerAndMode(info) || !securefs.OwnedByCurrentUser(info) {
			return "", fmt.Errorf("%w: unsafe restore destination", ErrUnsafePath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return absolute, nil
}

func ensureMaintenanceParent(parent string) error {
	if err := securefs.EnsurePrivateRoot(parent); err != nil {
		return err
	}
	return checkLocalFilesystem(parent)
}

func allocateMaintenancePath(destination string) (string, func(), error) {
	parent := filepath.Dir(destination)
	for attempt := 0; attempt < 8; attempt++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", func() {}, err
		}
		path := filepath.Join(parent, ".autogit-maint-"+hex.EncodeToString(random[:]))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600) // #nosec G304 -- parent is a validated private maintenance directory and the filename is cryptographically random.
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", func() {}, err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return "", func() {}, err
		}
		if err := os.Remove(path); err != nil {
			return "", func() {}, err
		}
		return path, func() { _ = os.Remove(path) }, nil
	}
	return "", func() {}, errors.New("could not allocate maintenance path")
}

func publishMaintenancePath(temporary, destination string) error {
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: destination is a symlink", ErrUnsafePath)
		}
		return errors.New("maintenance destination appeared during operation")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	return restrictDatabaseArtifacts(destination)
}

func replaceMaintenancePath(temporary, destination string) error {
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !sameOwnerAndMode(info) || !securefs.OwnedByCurrentUser(info) {
			return fmt.Errorf("%w: unsafe restore destination", ErrUnsafePath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		artifact := destination + suffix
		info, err := os.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !sameOwnerAndMode(info) || !securefs.OwnedByCurrentUser(info) {
			return fmt.Errorf("%w: unsafe restore SQLite artifact", ErrUnsafePath)
		}
		if err := os.Remove(artifact); err != nil {
			return err
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	return restrictDatabaseArtifacts(destination)
}

func copyMaintenanceFile(source, destination string) (int64, error) {
	in, err := os.Open(source) // #nosec G304 -- source was validated as a regular owned backup by the caller.
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600) // #nosec G304 -- destination was validated as a private exclusive maintenance path by the caller.
	if err != nil {
		return 0, err
	}
	bytes, copyErr := io.Copy(out, in)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return 0, closeErr
	}
	return bytes, nil
}

func sameOwnerAndMode(info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || (info.Mode().Perm()&0077 == 0)
}
