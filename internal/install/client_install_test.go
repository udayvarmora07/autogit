package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientRegistryReportsOnlyDocumentedHookFormatsAsSupported(t *testing.T) {
	byName := map[string]ClientInstallation{}
	for _, entry := range ClientInstallations() {
		byName[entry.Adapter] = entry
	}
	for _, name := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "commandcode"} {
		entry, ok := byName[name]
		if !ok {
			t.Fatalf("missing client registry entry %q", name)
		}
		if entry.Supported && entry.ConfigFormat != FormatJSON {
			t.Fatalf("%s supported with invalid format %q", name, entry.ConfigFormat)
		}
		if !entry.Supported && entry.UnsupportedReason == "" {
			t.Fatalf("%s unsupported without an honest reason", name)
		}
	}
	if !byName["claude-code"].Supported || !byName["gemini-cli"].Supported {
		t.Fatal("documented JSON hook clients not supported")
	}
}

func TestClaudeInstallUsesOwnedStopHookSchemaAndPreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-check"}]}]}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	entry, _ := ClientInstallationFor("claude-code")
	plan, err := PlanClient(entry, path, []string{dir}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyClient(plan); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	b, _ := os.ReadFile(path)
	if json.Unmarshal(b, &got) != nil {
		t.Fatalf("invalid JSON: %s", b)
	}
	hooks := got["hooks"].(map[string]any)[entry.HookEvent].([]any)
	if len(hooks) != 2 {
		t.Fatalf("hook count=%d", len(hooks))
	}
	if hooks[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "user-check" {
		t.Fatal("unrelated hook changed")
	}
	owned := hooks[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if owned["name"] != "autogit" || owned["type"] != "command" {
		t.Fatalf("not an owned Claude entry: %#v", owned)
	}
	if err := UninstallClient(entry, path, []string{dir}, dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	var restored map[string]any
	if json.Unmarshal(after, &restored) != nil {
		t.Fatal("uninstall produced invalid JSON")
	}
	if len(restored["hooks"].(map[string]any)[entry.HookEvent].([]any)) != 1 {
		t.Fatal("uninstall removed unrelated hook")
	}
}

func TestGeminiInstallUsesSettingsHooksAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"general":{"vimMode":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	entry, _ := ClientInstallationFor("gemini-cli")
	p, err := PlanClient(entry, path, []string{dir}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyClient(p); err != nil {
		t.Fatal(err)
	}
	one, _ := os.ReadFile(path)
	p, err = PlanClient(entry, path, []string{dir}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyClient(p); err != nil {
		t.Fatal(err)
	}
	two, _ := os.ReadFile(path)
	if string(one) != string(two) {
		t.Fatal("second Gemini install changed bytes")
	}
}

func TestUnsupportedClientInstallFailsClosedWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	entry, _ := ClientInstallationFor("cursor")
	if _, err := PlanClient(entry, path, []string{dir}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error=%v, want ErrUnsupported", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported install created a file")
	}
}

func TestCodexInstallUsesSessionEndAndShellQuotesCanonicalRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project's-root")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hooks.json")
	entry, err := ClientInstallationFor("codex")
	if err != nil || !entry.Supported {
		t.Fatalf("codex registry entry = %#v, err=%v", entry, err)
	}
	plan, err := PlanClient(entry, path, []string{filepath.Dir(path)}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(plan.Plan.Desired, &obj); err != nil {
		t.Fatal(err)
	}
	hooks := obj["hooks"].(map[string]any)["SessionEnd"].([]any)
	command := hooks[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(command, "--event session.ended") || !strings.Contains(command, "--root") || !strings.Contains(command, "project") || !strings.Contains(command, "s-root") {
		t.Fatalf("command = %q", command)
	}
	if !strings.Contains(command, "\\") {
		t.Fatalf("root is not shell-quoted: %q", command)
	}
}
