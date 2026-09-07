package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvenancePreviewAndRollbackStayOwned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"user":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(ConfigSpec{Adapter: "codex", Path: path, Format: FormatJSON}, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if Preview(plan).DesiredDigest == "" {
		t.Fatal("preview omitted desired digest")
	}
	if err := ApplyWithProvenance(plan); err != nil {
		t.Fatal(err)
	}
	prov, err := ReadProvenance(path)
	if err != nil || !prov.Installed || prov.Adapter != "codex" {
		t.Fatalf("provenance=%+v err=%v", prov, err)
	}
	if err := Rollback(path, "codex"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"user":true}` {
		t.Fatalf("rollback=%s", got)
	}
}
