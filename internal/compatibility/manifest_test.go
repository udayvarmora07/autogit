package compatibility

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateSupportWindowsIsDeterministicAndFlagsExpiry(t *testing.T) {
	windows := map[string]Window{}
	for _, name := range []string{"event_schema", "git", "github_rest", "go", "mcp", "result_schema", "sqlite"} {
		windows[name] = Window{Minimum: "1", ReviewBy: "2026-09-20"}
	}
	manifest := Manifest{SchemaVersion: "autogit.compatibility/1", SupportWindows: windows}
	report, err := Validate(manifest, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), 30*24*time.Hour)
	if err != nil || len(report.Windows) != len(windows) || !report.Due() || report.Expired() {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	windows["git"] = Window{Minimum: "1", ReviewBy: "2026-09-07"}
	report, err = Validate(Manifest{SchemaVersion: "autogit.compatibility/1", SupportWindows: windows}, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), 0)
	if err != nil || !report.Expired() {
		t.Fatalf("expired report=%+v err=%v", report, err)
	}
}

func TestLoadRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compatibility.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"autogit.compatibility/1"}{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}
