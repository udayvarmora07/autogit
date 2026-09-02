package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/events"
	"autogit/internal/repository"
)

func TestInvalidHookDoesNotCreateDurableState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", dir)
	var out bytes.Buffer
	if err := run([]string{"hook"}, strings.NewReader(`{"event":`), &out); err == nil {
		t.Fatal("invalid hook accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid hook created state: %v", entries)
	}
}

func TestSchemaInvalidHookDoesNotCreateDurableState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", dir)
	raw := strings.Replace(`{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"01J7N6X8P5K2V4W6FQ8M9ABCDF","event_type":"session.idle","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session"},"ordering":{"stream_id":"stream"},"idempotency":{"key":"k","attempt":1},"capabilities":{"queue_state":"bogus"},"payload":{}}`, `"attempt":1`, `"attempt":"bad"`, 1)
	var out bytes.Buffer
	if err := run([]string{"hook"}, strings.NewReader(raw), &out); err == nil {
		t.Fatal("schema-invalid hook accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("schema-invalid hook created state: %v", entries)
	}
}

func TestUnimplementedOperationIsExplicitlyUnsupported(t *testing.T) {
	t.Setenv("AUTOGIT_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"sync"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"disposition":"unsupported"`) || !strings.Contains(out.String(), `"reason_code":"E_UNIMPLEMENTED"`) {
		t.Fatalf("result=%s", out.String())
	}
}

func TestStatusWithoutEventsReturnsStableEmptyLifecycleSummary(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"status", "--repo", root}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	lifecycle := result["lifecycle"].(map[string]any)
	if lifecycle["exists"] != false || lifecycle["revision"] != float64(0) {
		t.Fatalf("empty lifecycle=%v", lifecycle)
	}
}

func TestLogsRequiresRepositoryAndRejectsInvalidLimitBeforeReading(t *testing.T) {
	t.Setenv("AUTOGIT_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"logs"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing repo error=%v", err)
	}
	if err := run([]string{"logs", "--repo", t.TempDir(), "--limit", "0"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_USAGE:") {
		t.Fatalf("invalid limit error=%v", err)
	}
}

func TestLogsInvalidRepositoryDoesNotCreateState(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	var out bytes.Buffer
	if err := run([]string{"logs", "--repo", filepath.Join(state, "missing-repository")}, strings.NewReader(""), &out); err == nil {
		t.Fatal("invalid repository accepted")
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid logs created state: %v", entries)
	}
}

func TestLogsUnknownArgumentDoesNotCreateState(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"logs", "--repo", root, "--unexpected"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("unknown logs argument accepted")
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid logs created state: %v", entries)
	}
}

func TestLogsReturnsNewestRedactedFactsForRepository(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	var statusOut bytes.Buffer
	if err := run([]string{"status", "--repo", root}, strings.NewReader(""), &statusOut); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(state, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	s, err := events.OpenStore(filepath.Join(state, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept(context.Background(), events.Event{SchemaVersion: "autogit.event/1", EventClass: "ingress", EventID: "01J7N6X8P5K2V4W6FQ8M9ABCDG", EventType: "session.idle", Scope: map[string]any{"repo_id": info.RepoID}, Ordering: map[string]any{"stream_id": "stream"}, Idempotency: map[string]any{"key": "log"}, Producer: map[string]any{"kind": "adapter"}, Payload: map[string]any{}, Digest: "sha256:" + strings.Repeat("a", 64)}); err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Close()
	var logsOut bytes.Buffer
	if err := run([]string{"logs", "--repo", root}, strings.NewReader(""), &logsOut); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Logs []map[string]any `json:"logs"`
	}
	if err := json.Unmarshal(logsOut.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != 1 {
		t.Fatalf("logs=%v", result.Logs)
	}
	for _, forbidden := range []string{"event_id", "metadata"} {
		if _, exists := result.Logs[0][forbidden]; exists || strings.Contains(logsOut.String(), forbidden) {
			t.Fatalf("logs leaked %q: %s", forbidden, logsOut.String())
		}
	}
	for _, required := range []string{"timestamp", "reason", "event_digest", "revision"} {
		if _, exists := result.Logs[0][required]; !exists {
			t.Fatalf("logs omitted %q: %s", required, logsOut.String())
		}
	}
}

func TestLogsScopesRepositoriesOrdersNewestAndHonorsLimitAcrossRestart(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	rootA, rootB := t.TempDir(), t.TempDir()
	for _, root := range []string{rootA, rootB} {
		if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"status", "--repo", rootA}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(state, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	infoA, err := repository.DiscoverWithKey(rootA, key)
	if err != nil {
		t.Fatal(err)
	}
	store, err := events.OpenStore(filepath.Join(state, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"01J7N6X8P5K2V4W6FQ8M9ABCDG", "01J7N6X8P5K2V4W6FQ8M9ABCDH", "01J7N6X8P5K2V4W6FQ8M9ABCDF"} {
		raw := strings.Replace(diagnosticEvent(id, "logs-"+string(rune('a'+i))), "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", infoA.RepoID, 1)
		e, decodeErr := events.Decode([]byte(raw), 64<<10)
		if decodeErr != nil {
			store.Close()
			t.Fatal(decodeErr)
		}
		if _, acceptErr := store.Accept(context.Background(), e); acceptErr != nil {
			store.Close()
			t.Fatal(acceptErr)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var limited bytes.Buffer
	if err := run([]string{"logs", "--repo", rootA, "--limit", "2"}, strings.NewReader(""), &limited); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Logs []events.AuditLog `json:"logs"`
	}
	if err := json.Unmarshal(limited.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != 2 || result.Logs[0].Revision <= result.Logs[1].Revision {
		t.Fatalf("limited logs=%v", result.Logs)
	}
	if strings.Contains(limited.String(), "event_id") {
		t.Fatalf("logs exposed event identity: %s", limited.String())
	}
	var other bytes.Buffer
	if err := run([]string{"logs", "--repo", rootB}, strings.NewReader(""), &other); err != nil {
		t.Fatal(err)
	}
	var otherResult struct {
		Logs []events.AuditLog `json:"logs"`
	}
	if err := json.Unmarshal(other.Bytes(), &otherResult); err != nil {
		t.Fatal(err)
	}
	if len(otherResult.Logs) != 0 {
		t.Fatalf("cross-repository logs=%v", otherResult.Logs)
	}
}

func diagnosticEvent(id, key string) string {
	return `{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"` + id + `","event_type":"session.idle","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session"},"ordering":{"stream_id":"stream"},"idempotency":{"key":"` + key + `"},"payload":{}}`
}

func TestInstallRoutesThroughClientRegistryAndRejectsUnsupported(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	var out bytes.Buffer
	err := run([]string{"install", "--adapter", "cursor", "--path", path, "--root", root}, strings.NewReader(""), &out)
	if err == nil || !strings.HasPrefix(err.Error(), "E_UNSUPPORTED:") {
		t.Fatalf("error=%v, want explicit unsupported result", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported install touched config: %v", statErr)
	}
}

func TestConfigExplainLoadsTrustedVerifierConfiguration(t *testing.T) {
	t.Setenv("AUTOGIT_STATE_DIR", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "verifiers.json")
	config, err := json.Marshal(map[string]any{"version": "1", "verifiers": []any{map[string]any{
		"name": "tests", "version": "1", "argv": []string{exe}, "applicable": true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"config", "explain", "--verifiers", path}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"verifier_set_digest":"sha256:`) || !strings.Contains(out.String(), `"verifier_config_digest":"sha256:`) {
		t.Fatalf("config explanation=%s", out.String())
	}
}

func TestInstallPassesCanonicalRootIntoCodexHook(t *testing.T) {
	t.Setenv("AUTOGIT_STATE_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "project's-root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "hooks.json")
	var out bytes.Buffer
	if err := run([]string{"install", "--adapter", "codex", "--path", path, "--root", root}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "session.ended") || !strings.Contains(string(b), "--root") {
		t.Fatalf("installed Codex hook omitted root/event: %s", b)
	}
}

func TestCodexSessionEndHookContractAcceptsOfficialPayload(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "hooks.json")
	var installOut bytes.Buffer
	if err := run([]string{"install", "--adapter", "codex", "--path", config, "--root", root}, strings.NewReader(""), &installOut); err != nil {
		t.Fatal(err)
	}
	var installed map[string]any
	b, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &installed); err != nil {
		t.Fatal(err)
	}
	groups := installed["hooks"].(map[string]any)["SessionEnd"].([]any)
	command := groups[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(command, "--adapter codex") || !strings.Contains(command, "--event session.ended") || !strings.Contains(command, "--root") {
		t.Fatalf("installed command=%q", command)
	}
	var hookOut bytes.Buffer
	payload, err := json.Marshal(map[string]string{
		"hook_event_name": "SessionEnd",
		"session_id":      "session-1",
		"cwd":             root,
		"operation_id":    "operation-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runHook([]string{"--adapter", "codex", "--root", root, "--event", "session.ended"}, strings.NewReader(string(payload)), &hookOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hookOut.String(), `"disposition":"accepted"`) {
		t.Fatalf("hook result=%s", hookOut.String())
	}
}

func TestAdapterHooksResolveTrustedTempRepositoryForEveryClient(t *testing.T) {
	for _, name := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "commandcode"} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AUTOGIT_STATE_DIR", t.TempDir())
			payload, err := json.Marshal(map[string]string{
				"event":      "idle",
				"session_id": "s-1",
				"cwd":        repo,
				"repo_id":    "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"id":         "01J7N6X8P5K2V4W6FQ8M9ABCDF",
			})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := run([]string{"hook", "--adapter", name, "--root", repo}, strings.NewReader(string(payload)), &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), `"disposition":"accepted"`) {
				t.Fatalf("result=%s", out.String())
			}
		})
	}
}
