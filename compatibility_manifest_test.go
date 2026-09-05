package autogit

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"autogit/internal/adapters"
	"autogit/internal/state"
)

type releaseCompatibilityManifest struct {
	SchemaVersion      string                        `json:"schema_version"`
	EventSchemaMajors  []string                      `json:"event_schema_majors"`
	ResultSchemaMajors []string                      `json:"result_schema_majors"`
	StateSchema        releaseStateCompatibility     `json:"state_schema"`
	Adapters           []adapters.CapabilityManifest `json:"adapters"`
}

type releaseStateCompatibility struct {
	CurrentVersion int    `json:"current_version"`
	Upgrade        string `json:"upgrade"`
	FutureVersion  string `json:"future_version"`
}

func TestReleaseCompatibilityManifestMatchesSupportedContracts(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Dir(source)
	data, err := os.ReadFile(filepath.Join(root, "docs", "compatibility-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest releaseCompatibilityManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "autogit.compatibility/1" {
		t.Fatalf("schema version=%q", manifest.SchemaVersion)
	}
	if !reflect.DeepEqual(manifest.EventSchemaMajors, []string{"autogit.event/1"}) {
		t.Fatalf("event schema majors=%v", manifest.EventSchemaMajors)
	}
	if !reflect.DeepEqual(manifest.ResultSchemaMajors, []string{"autogit.result/1"}) {
		t.Fatalf("result schema majors=%v", manifest.ResultSchemaMajors)
	}
	if manifest.StateSchema.CurrentVersion != currentStateSchemaVersion(t) {
		t.Fatalf("state schema version=%d", manifest.StateSchema.CurrentVersion)
	}
	if manifest.StateSchema.Upgrade != "forward-only" || manifest.StateSchema.FutureVersion != "reject" {
		t.Fatalf("state schema policy=%+v", manifest.StateSchema)
	}
	names := adapters.SupportedNames()
	if len(manifest.Adapters) != len(names) {
		t.Fatalf("adapter count=%d, want %d", len(manifest.Adapters), len(names))
	}
	for i, name := range names {
		want, err := adapters.ManifestFor(name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(manifest.Adapters[i], want) {
			t.Fatalf("adapter manifest %q differs:\n got: %#v\nwant: %#v", name, manifest.Adapters[i], want)
		}
	}
}

func currentStateSchemaVersion(t *testing.T) int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := state.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow(`SELECT value FROM state_meta WHERE key='schema_version'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	return version
}
