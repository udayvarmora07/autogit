package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONInstallBacksUpAtomicallyAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "client.json")
	original := []byte("{\n  \"theme\": \"dark\",\n  \"hooks\": []\n}\n")
	if err := os.WriteFile(p, original, 0640); err != nil {
		t.Fatal(err)
	}
	spec := ConfigSpec{Adapter: "codex", Path: p, Format: FormatJSON}
	plan, err := Plan(spec, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"theme":"dark"`) || !strings.Contains(string(after), OwnershipMarker) {
		t.Fatalf("unrelated config or ownership marker missing: %s", after)
	}
	if mode := fileMode(p); mode.Perm() != 0600 {
		t.Fatalf("mode=%o, want restrictive 0600", mode.Perm())
	}
	backups, _ := filepath.Glob(p + ".autogit-backup-*")
	if len(backups) != 1 {
		t.Fatalf("backup count=%d, want 1", len(backups))
	}
	first := string(after)
	plan, err = Plan(spec, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(p)
	if string(after) != first {
		t.Fatalf("second install changed bytes")
	}
}

func TestUninstallRemovesOnlyOwnedJSONEntry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "client.json")
	if err := os.WriteFile(p, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, _ := Plan(ConfigSpec{Adapter: "cursor", Path: p, Format: FormatJSON}, []string{dir})
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(ConfigSpec{Adapter: "cursor", Path: p, Format: FormatJSON}, []string{dir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), OwnershipMarker) || !strings.Contains(string(b), `"theme":"dark"`) {
		t.Fatalf("unrelated data was not preserved: %s", b)
	}
}

func TestLineInstallAndUninstallPreserveUnrelatedBytes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hooks.conf")
	original := "before\n# user comment\nautogit-user=keep\n"
	if err := os.WriteFile(p, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(ConfigSpec{Adapter: "gemini-cli", Path: p, Format: FormatLines}, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(ConfigSpec{Adapter: "gemini-cli", Path: p, Format: FormatLines}, []string{dir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != original {
		t.Fatalf("unrelated line bytes changed: %q", b)
	}
}

func TestInstallRejectsPathOutsideExplicitRootsAndSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	_, err := Plan(ConfigSpec{Adapter: "opencode", Path: filepath.Join(outside, "config"), Format: FormatLines}, []string{dir})
	if err == nil {
		t.Fatal("path outside supplied roots accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	_, err = Plan(ConfigSpec{Adapter: "commandcode", Path: filepath.Join(link, "config"), Format: FormatLines}, []string{dir})
	if err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestJSONPlanRejectsDuplicateKeysAndTrailingData(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "client.json")
	for _, content := range []string{`{"theme":"dark","theme":"light"}`, `{"theme":"dark"}{}`} {
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Plan(ConfigSpec{Adapter: "codex", Path: p, Format: FormatJSON}, []string{dir}); err == nil {
			t.Fatalf("invalid JSON accepted: %q", content)
		}
	}
}

func TestApplyRefusesToOverwriteAChangedConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "client.json")
	if err := os.WriteFile(p, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(ConfigSpec{Adapter: "codex", Path: p, Format: FormatJSON}, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"theme":"user-updated"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan); !errors.Is(err, ErrStale) {
		t.Fatalf("error=%v, want ErrStale", err)
	}
}

func fileMode(path string) os.FileMode {
	info, _ := os.Stat(path)
	return info.Mode()
}
