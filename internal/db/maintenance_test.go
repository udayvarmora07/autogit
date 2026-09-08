package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInspectReportsDurableSQLiteContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO audit(id,reason_code,metadata,at) VALUES('audit-1','TEST','{}',1)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if report.SQLiteVersion != EmbeddedSQLiteVersion || report.JournalMode != "wal" || (report.Synchronous != 1 && report.Synchronous != 2) || !report.ForeignKeys || report.BusyTimeout != busyTimeoutMS || report.WALAutoCheckpoint != 1000 {
		t.Fatalf("unexpected SQLite contract: %+v", report)
	}
	if !report.IntegrityOK || !report.ForeignKeysOK || report.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("unhealthy report: %+v", report)
	}
}

func TestBackupRestoreRoundTripPreservesDurableState(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "state.db")
	backup := filepath.Join(root, "backups", "state.db")
	restored := filepath.Join(root, "restored", "state.db")

	database, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO audit(id,repository_id,reason_code,metadata,at) VALUES('audit-1','repo','TEST','{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}',10)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), source, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), backup, restored); err != nil {
		t.Fatal(err)
	}
	if database, err := Open(restored); err != nil {
		t.Fatal(err)
	} else {
		if _, err := database.Exec(`INSERT INTO audit(id,reason_code,metadata,at) VALUES('audit-2','TEST','{}',2)`); err != nil {
			t.Fatal(err)
		}
		_ = database.Close()
	}
	if _, err := Restore(context.Background(), backup, restored); err != nil {
		t.Fatalf("restore over existing state: %v", err)
	}

	check, err := OpenReadOnly(context.Background(), restored)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var metadata string
	if err := check.QueryRow(`SELECT metadata FROM audit WHERE id='audit-1'`).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata, "sha256:") {
		t.Fatalf("restored metadata=%q", metadata)
	}
	if info, err := os.Stat(backup); err != nil {
		t.Fatalf("backup stat: info=%v err=%v", info, err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions: mode=%o, want 600", info.Mode().Perm())
	}
}

func TestBackupRejectsExistingDestinationAndSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "state.db")
	database, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	destination := filepath.Join(root, "backup.db")
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), source, destination); err == nil {
		t.Fatal("backup overwrote existing destination")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "existing" {
		t.Fatalf("existing destination changed: %q err=%v", got, err)
	}
	if runtimeSymlinkSupported() {
		target := filepath.Join(root, "outside.db")
		if err := os.WriteFile(target, []byte("outside"), 0600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link.db")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), source, link); err == nil {
			t.Fatal("backup accepted symlink destination")
		}
	}
}

func TestRetainCompactsOldAuditAndOutboxButPreservesPendingRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`
		INSERT INTO audit(id,repository_id,reason_code,metadata,prev_digest,digest,at) VALUES('audit-old','repo','OLD','{}','','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1);
		INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES('repo','accepted','OLD','{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}',?),
		('repo','pending','PENDING','{"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}',?);
		INSERT INTO event_receipts(event_id,idempotency_key,payload_digest,disposition,revision,created_at) VALUES('pending-event','pending-key','sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','pending',2,?);
		INSERT INTO pending_events(event_id,causation_id,payload) VALUES('pending-event','missing',X'7B7D');
		INSERT INTO outbox(id,kind,aggregate_id,payload,created_at,published_at) VALUES('published','test','aggregate',X'7B7D',1,1),('pending','test','aggregate',X'7B7D',1,NULL);`, old, time.Now().UTC().Format(time.RFC3339Nano), old); err != nil {
		t.Fatal(err)
	}

	report, err := Retain(context.Background(), path, RetentionPolicy{Now: time.Now(), MaxAuditAge: time.Hour, MaxOutboxAge: time.Hour, Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.AuditEventsDeleted != 1 || report.OutboxDeleted != 1 || report.AuditCompacted != 1 {
		t.Fatalf("unexpected retention report: %+v", report)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM pending_events WHERE event_id='pending-event'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pending recovery evidence removed: count=%d err=%v", count, err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM outbox WHERE id='pending'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pending outbox removed: count=%d err=%v", count, err)
	}
}

func TestRetainMovesOldReceiptToTombstoneAndReplayRemainsConflictSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, err := database.Exec(`INSERT INTO event_receipts(event_id,idempotency_key,payload_digest,disposition,revision,created_at) VALUES('old-event','old-key',?,'accepted',1,?)`, digest, old); err != nil {
		t.Fatal(err)
	}
	if _, err := Retain(context.Background(), path, RetentionPolicy{Now: time.Now(), ReceiptAge: time.Hour}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM event_receipts WHERE event_id='old-event'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("receipt was not compacted: count=%d err=%v", count, err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM event_receipt_tombstones WHERE event_id='old-event'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt tombstone missing: count=%d err=%v", count, err)
	}
}

func TestInspectRejectsCorruptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(context.Background(), path); err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("corrupt database was accepted: %v", err)
	}
}

func TestExportIsRedactedAndRepairOnlyRunsSafeMaintenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO audit(id,repository_id,reason_code,metadata,at) VALUES('audit-1','repo','TEST','{"secret":"must-not-export"}',1)`); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	export, err := Export(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(export), "must-not-export") || !strings.Contains(string(export), `"schema_version"`) {
		t.Fatalf("export leaked state payload: %s", export)
	}
	repair, err := Repair(context.Background(), path)
	if err != nil || !repair.IntegrityOK || !repair.ForeignKeysOK {
		t.Fatalf("repair report=%+v err=%v", repair, err)
	}
}

func runtimeSymlinkSupported() bool {
	return runtime.GOOS != "windows"
}
