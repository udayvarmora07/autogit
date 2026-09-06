package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenAppliesSQLiteSafetyContractAndCombinedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var version string
	if err := database.QueryRowContext(context.Background(), `SELECT sqlite_version()`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if !atLeastVersion(version, RequiredSQLiteVersion) {
		t.Fatalf("SQLite version = %s, want >= %s", version, RequiredSQLiteVersion)
	}

	checks := map[string]string{
		"journal_mode":       "wal",
		"foreign_keys":       "1",
		"busy_timeout":       "5000",
		"wal_autocheckpoint": "1000",
	}
	for pragma, want := range checks {
		var got string
		if err := database.QueryRowContext(context.Background(), `PRAGMA `+pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if !strings.EqualFold(got, want) {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}

	var synchronous int
	if err := database.QueryRowContext(context.Background(), `PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 && synchronous != 2 {
		t.Fatalf("PRAGMA synchronous = %d, want NORMAL(1) or FULL(2)", synchronous)
	}

	for _, table := range []string{"state_meta", "commits", "leases", "event_receipts", "pending_events", "lifecycle_projections"} {
		var count int
		if err := database.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %q count = %d, want 1", table, count)
		}
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("database permissions = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestOpenRejectsFinalSymlinkWithoutFollowingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	root := t.TempDir()
	realPath := filepath.Join(root, "real.db")
	linkPath := filepath.Join(root, "state.db")
	if err := os.WriteFile(realPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(linkPath); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Open(symlink) error = %v, want ErrUnsafePath", err)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was modified: info=%v err=%v", info, err)
	}
}

func TestOpenReadOnlyDoesNotCreateOrMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	if _, err := OpenReadOnly(context.Background(), path); err == nil {
		t.Fatal("OpenReadOnly unexpectedly created a missing database")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing database stat error = %v", err)
	}

	created := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(created)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	readOnly, err := OpenReadOnly(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err := readOnly.Exec(`CREATE TABLE should_not_exist(id INTEGER)`); err == nil {
		t.Fatal("read-only database accepted a write")
	}
}

func TestOpenSerializesInProcessConnections(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if got := database.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
}
