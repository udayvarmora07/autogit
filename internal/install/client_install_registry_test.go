package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"autogit/internal/adapters"
)

func TestCursorInstallerUsesOfficialFlatHooksSchema(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"hooks":{"afterFileEdit":[{"command":"user-hook"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := ClientInstallationFor("cursor")
	if err != nil || !entry.Supported {
		t.Fatalf("cursor entry=%+v err=%v", entry, err)
	}
	plan, err := PlanClient(entry, path, []string{dir}, root)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(plan.Plan.Desired, &got); err != nil {
		t.Fatal(err)
	}
	hooks := got["hooks"].(map[string]any)
	items := hooks["sessionEnd"].([]any)
	if len(items) != 1 {
		t.Fatalf("sessionEnd hooks=%v", items)
	}
	item := items[0].(map[string]any)
	if item["command"] == nil || item["type"] != "command" {
		t.Fatalf("not an official Cursor command hook: %#v", item)
	}
	if _, nested := item["hooks"]; nested {
		t.Fatalf("Cursor hook incorrectly used grouped schema: %#v", item)
	}
	if err := ApplyClient(plan); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClient(entry, path, []string{dir}, root); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restored map[string]any
	if err := json.Unmarshal(final, &restored); err != nil {
		t.Fatal(err)
	}
	if _, exists := restored["hooks"].(map[string]any)["sessionEnd"]; exists {
		t.Fatalf("owned Cursor hook was not removed: %s", final)
	}
	if len(restored["hooks"].(map[string]any)["afterFileEdit"].([]any)) != 1 {
		t.Fatalf("unrelated Cursor hook was removed: %s", final)
	}
}

func TestClientInstallationsAreDerivedFromAdapterRegistry(t *testing.T) {
	for _, entry := range ClientInstallations() {
		contract, err := adapters.RegistryEntryFor(entry.Adapter)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Supported != contract.InstallSupported || entry.HookEvent != contract.Config.HookEvent {
			t.Fatalf("install entry=%+v registry=%+v", entry, contract)
		}
	}
}

func TestCursorUninstallRejectsMalformedHookEventArray(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"hooks":{"sessionEnd":{}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := ClientInstallationFor("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if err := UninstallClient(entry, path, []string{dir}, root); !errors.Is(err, ErrFormat) {
		t.Fatalf("error=%v, want ErrFormat", err)
	}
}
