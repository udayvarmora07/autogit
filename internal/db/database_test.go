package db

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	if version != EmbeddedSQLiteVersion {
		t.Fatalf("SQLite version = %s, want embedded version %s", version, EmbeddedSQLiteVersion)
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

func TestOpenContextDoesNotCreateStateAfterCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenContext() error = %v, want context canceled", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled open created state directory: %v", err)
	}
}

func TestMigrationCrashRollsBackAndReopens(t *testing.T) {
	if os.Getenv("AUTOGIT_DB_TEST_CHILD") == "migration" {
		migrationTestHook = func(stage string) {
			if stage != "before-schema-version-update" {
				return
			}
			if err := os.WriteFile(os.Getenv("AUTOGIT_DB_TEST_MARKER"), []byte("ready"), 0600); err != nil {
				os.Exit(125)
			}
			select {}
		}
		_, _ = Open(os.Getenv("AUTOGIT_DB_TEST_SOURCE"))
		os.Exit(126)
	}

	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE state_meta SET value='6' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(path), "migration-ready")
	command := exec.Command(os.Args[0], "-test.run=^TestMigrationCrashRollsBackAndReopens$")
	command.Env = append(os.Environ(),
		"AUTOGIT_DB_TEST_CHILD=migration",
		"AUTOGIT_DB_TEST_SOURCE="+path,
		"AUTOGIT_DB_TEST_MARKER="+marker,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	waitForDatabaseTestFile(t, marker)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("migration child survived forced termination")
	}

	database, err = Open(path)
	if err != nil {
		t.Fatalf("database did not recover after migration crash: %v", err)
	}
	defer database.Close()
	var version string
	if err := database.QueryRow(`SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != strconv.Itoa(CurrentSchemaVersion) {
		t.Fatalf("schema version=%q, want %d after recovery", version, CurrentSchemaVersion)
	}
	if _, err := Inspect(context.Background(), path); err != nil {
		t.Fatalf("recovered database failed integrity inspection: %v", err)
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

func TestOpenRejectsPreexistingWALSymlinkBeforeSQLiteOpens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	root := t.TempDir()
	path := filepath.Join(root, "state.db")
	outside := filepath.Join(t.TempDir(), "outside.wal")
	if err := os.WriteFile(outside, []byte("must remain unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path+"-wal"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Open(preexisting WAL symlink) error = %v, want ErrUnsafePath", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "must remain unchanged" {
		t.Fatalf("preexisting WAL target changed to %q", got)
	}
}

func TestOpenRejectsSymlinkedStateDirectoryAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	outside := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(link, "nested", "state.db")); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Open(symlinked state directory) error = %v, want ErrUnsafePath", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "nested", "state.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target was modified: %v", err)
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
