package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"autogit/internal/events"
	"autogit/internal/gittransaction"
	"autogit/internal/lifecycle"
	"autogit/internal/policy"
	"autogit/internal/provider"
	"autogit/internal/publication"
	"autogit/internal/repository"
	"autogit/internal/state"
	"autogit/internal/verification"
	localworkflow "autogit/internal/workflow"
)

func testVerifierConfig(t *testing.T) []byte {
	t.Helper()
	argv := []string{"/usr/bin/true"}
	if runtime.GOOS == "windows" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		argv = []string{executable, "-test.run=^$"}
	}
	b, err := json.Marshal(map[string]any{
		"version":   "1",
		"verifiers": []any{map[string]any{"name": "true", "version": "1", "argv": argv}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

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

func TestDoctorReportsOperationalDependencySurface(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)
	var out bytes.Buffer
	if err := run([]string{"doctor"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["state_database"] != "not_initialized" || result["lock_store"] != "not_initialized" {
		t.Fatalf("doctor state diagnostics=%v", result)
	}
	if count, ok := result["adapter_count"].(float64); !ok || count != 6 {
		t.Fatalf("doctor adapter count=%v", result["adapter_count"])
	}
	if _, ok := result["gh_available"].(bool); !ok {
		t.Fatalf("doctor gh diagnostic=%v", result["gh_available"])
	}
	if _, ok := result["provider_auth"].(string); !ok {
		t.Fatalf("doctor provider auth diagnostic=%v", result["provider_auth"])
	}
	if entries, err := os.ReadDir(stateRoot); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("read-only doctor created state: %v", entries)
	}
}

func TestDoctorReportsCorruptStateAsUnavailableWithoutRepairingIt(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(stateRoot, "state.db")
	original := []byte("not a sqlite database")
	if err := os.WriteFile(databasePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)

	var out bytes.Buffer
	if err := run([]string{"doctor"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["state_database"] != "unavailable" || result["lock_store"] != "unavailable" {
		t.Fatalf("doctor reported corrupt state as usable: %v", result)
	}
	if result["reason_code"] != "STATE_UNAVAILABLE" {
		t.Fatalf("doctor reason code=%v, want STATE_UNAVAILABLE", result["reason_code"])
	}
	got, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("read-only doctor changed corrupt state database: %q", got)
	}
}

func TestDoctorReportsHealthyInitializedStateAndLeaseStore(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(stateRoot, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)

	var out bytes.Buffer
	if err := run([]string{"doctor"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["state_database"] != "available" || result["lock_store"] != "available" || result["reason_code"] != "DOCTOR_OK" {
		t.Fatalf("doctor healthy-state diagnostics=%v", result)
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

func TestHookSessionStartedRecordsRepositoryBaselineBeforeReturning(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("user work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("k", 32)
	if err := os.WriteFile(filepath.Join(stateDir, "identity.key"), []byte(key), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	input := hookEvent("01J7N6X8P5K2V4W6NQ8M9ABCDF", "session.started", "started", `,"session_id":"session"`, "", "")
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		t.Fatal(err)
	}
	raw["scope"].(map[string]any)["repo_id"] = info.RepoID
	raw["scope"].(map[string]any)["worktree_id"] = info.WorktreeID
	raw["project"] = map[string]any{"candidate_root": root}
	inputBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"hook"}, bytes.NewReader(inputBytes), &out); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Session(context.Background(), "session")
	if err != nil || got.RepositoryID != info.RepoID || got.ClientID != "codex" || got.BaselinePathsDigest == "" || got.BaselineEvidence == "" {
		t.Fatalf("session=%+v err=%v output=%s", got, err, out.String())
	}
	if strings.Contains(out.String(), "existing.txt") || strings.Contains(out.String(), "user work") {
		t.Fatalf("hook output leaked baseline content: %s", out.String())
	}
}

func hookEvent(id, typ, key, scopeExtra, taskExtra, orderingExtra string) string {
	return `{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"` + id + `","event_type":"` + typ + `","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"` + scopeExtra + taskExtra + `},"ordering":{"stream_id":"stream"` + orderingExtra + `},"idempotency":{"key":"` + key + `"},"payload":{}}`
}

func hookEventWithCapabilities(id, typ, key, scopeExtra, capabilities string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(hookEvent(id, typ, key, scopeExtra, "", "")), &raw); err != nil {
		panic(err)
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(capabilities), &caps); err != nil {
		panic(err)
	}
	raw["capabilities"] = caps
	encoded, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestVerifyRequiresExplicitSessionEvidence(t *testing.T) {
	t.Setenv("AUTOGIT_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"verify"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing verify arguments error=%v output=%s", err, out.String())
	}
}

func TestVerifyAllOwnedAcceptsSourceFreeSessionEvidence(t *testing.T) {
	options, err := parseVerifyArgs([]string{
		"--id", "verify-1", "--repo", "/tmp/project", "--session", "session",
		"--client", "codex", "--message", "feat: verify candidate", "--verifiers", "verifiers.json",
		"--all-owned",
	})
	if err != nil {
		t.Fatalf("verify all-owned args: %v", err)
	}
	if !options.AllOwned || len(options.Paths) != 0 {
		t.Fatalf("verify options=%+v", options)
	}
}

func TestVerifyAndSyncCanUseTrustedPolicyProfileWithTaskIntent(t *testing.T) {
	verify, err := parseVerifyArgs([]string{"--id", "verify-intent", "--repo", "/tmp/project", "--session", "session", "--client", "codex", "--intent", "fix parser accepts paths", "--all-owned"})
	if err != nil || verify.Intent == "" || verify.Message != "" || verify.Verifiers != "" {
		t.Fatalf("verify profile options=%+v err=%v", verify, err)
	}
	sync, err := parseSyncArgs([]string{"--complete", "--all-owned", "--id", "sync-intent", "--repo", "/tmp/project", "--session", "session", "--client", "codex", "--intent", "fix parser accepts paths"})
	if err != nil || sync.Intent == "" || sync.Message != "" || sync.Verifiers != "" {
		t.Fatalf("sync profile options=%+v err=%v", sync, err)
	}
}

func TestSyncAllOwnedRequiresCompletionAndNoExplicitPaths(t *testing.T) {
	base := []string{"--repo", "/tmp/project", "--session", "session", "--client", "codex"}
	if _, err := parseSyncArgs(append(append([]string{}, base...), "--all-owned")); err == nil || !strings.Contains(err.Error(), "requires --complete") {
		t.Fatalf("all-owned without completion error=%v", err)
	}
	if _, err := parseSyncArgs(append(append([]string{}, base...), "--complete", "--all-owned", "--id", "commit", "--message", "feat: work", "--verifiers", "verifiers.json", "--path", "file.txt")); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("all-owned with path error=%v", err)
	}
}

func TestPlanReportsReadOnlyRepositorySummary(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", "--initial-branch", "main", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: baseline")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "candidate.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"enable", "--repo", root, "--local"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	headBefore := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
	indexBefore := strings.TrimSpace(gitOutput(t, root, "rev-parse", "--git-path", "index"))
	indexBytesBefore, err := os.ReadFile(filepath.Join(root, indexBefore))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"plan", "--repo", root}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("plan: %v output=%s", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("plan JSON: %v output=%s", err, out.String())
	}
	if got["reason_code"] != "READ_ONLY_PLAN" {
		t.Fatalf("plan=%v", got)
	}
	repositorySummary, ok := got["repository"].(map[string]any)
	if !ok || repositorySummary["head"] != headBefore || repositorySummary["changed_path_count"] != float64(1) || repositorySummary["paths_digest"] == "" {
		t.Fatalf("repository summary=%v", got["repository"])
	}
	if got["checks"].(map[string]any)["tracking_consent"] != true {
		t.Fatalf("checks=%v", got["checks"])
	}
	if strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD")) != headBefore {
		t.Fatal("plan changed HEAD")
	}
	indexBytesAfter, err := os.ReadFile(filepath.Join(root, indexBefore))
	if err != nil || !bytes.Equal(indexBytesBefore, indexBytesAfter) {
		t.Fatal("plan changed the shared index")
	}
}

func TestPlanDoesNotCreateStateBeforeInitialization(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "candidate.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"plan", "--repo", root}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("plan: %v output=%s", err, out.String())
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("read-only plan created state: %v", entries)
	}
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func TestParseInitRequiresExplicitTrackingChoice(t *testing.T) {
	if _, err := parseInitArgs([]string{"--repo", "/tmp/project"}); err == nil || !strings.HasPrefix(err.Error(), "E_CONSENT:") {
		t.Fatalf("missing tracking choice error=%v", err)
	}
	got, err := parseInitArgs([]string{"--repo", "/tmp/project", "--local", "--branch", "trunk"})
	if err != nil || !got.Local || got.Branch != "trunk" {
		t.Fatalf("local init=%+v err=%v", got, err)
	}
	if _, err := parseInitArgs([]string{"--repo", "/tmp/project", "--provider", "github", "--owner", "owner", "--name", "repo", "--visibility", "public"}); err == nil || !strings.HasPrefix(err.Error(), "E_CONSENT:") {
		t.Fatalf("public init without consent error=%v", err)
	}
	got, err = parseInitArgs([]string{"--repo", "/tmp/project", "--local", "--dry-run"})
	if err != nil || !got.DryRun {
		t.Fatalf("dry-run init=%+v err=%v", got, err)
	}
}

func TestInitCreatesGitAndRecordsConsentBeforeMutation(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"init", "--repo", root, "--local", "--branch", "trunk"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("git metadata missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatalf("generated hygiene missing: %v", err)
	}
	branchOutput, err := exec.Command("git", "-C", root, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(branchOutput)) != "trunk" {
		t.Fatalf("initial branch=%q err=%v", branchOutput, err)
	}
	key, err := os.ReadFile(filepath.Join(stateRoot, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	if got := loadPolicy(stateRoot, info.RepoID); !got.TrackingEnabled() || !got.LocalOnly {
		t.Fatalf("policy=%+v output=%s", got, out.String())
	}
	if !strings.Contains(out.String(), "REPOSITORY_INITIALIZED") || !strings.Contains(out.String(), "trunk") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestInitDryRunDoesNotCreateStateOrGitMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"init", "--repo", root, "--local", "--dry-run"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created git metadata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created hygiene: %v", err)
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || !strings.Contains(out.String(), "REPOSITORY_INIT_PLAN") {
		t.Fatalf("dry-run state=%v output=%s", entries, out.String())
	}
}

func TestVerifyReconstructsCleanSessionWithoutCreatingCommitIntent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: baseline")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	for _, config := range [][]string{{"user.name", "AutoGit"}, {"user.email", "autogit@example.test"}} {
		if output, err := exec.Command("git", "-C", root, "config", config[0], config[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, output)
		}
	}
	var setup bytes.Buffer
	if err := run([]string{"enable", "--repo", root}, strings.NewReader(""), &setup); err != nil {
		t.Fatal(err)
	}
	var syncOut bytes.Buffer
	if err := run([]string{"sync", "--repo", root, "--session", "session-verify", "--client", "codex", "--path", "tracked.txt", "--path", "new.txt"}, strings.NewReader(""), &syncOut); err != nil {
		t.Fatalf("sync: %v output=%s", err, syncOut.String())
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verifierConfig := filepath.Join(stateDir, "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	var verifyOut bytes.Buffer
	if err := run([]string{"verify", "--id", "verify-1", "--repo", root, "--session", "session-verify", "--client", "codex", "--message", "feat: verify candidate", "--verifiers", verifierConfig, "--path", "tracked.txt", "--path", "new.txt"}, strings.NewReader(""), &verifyOut); err != nil {
		t.Fatalf("verify: %v output=%s", err, verifyOut.String())
	}
	if !strings.Contains(verifyOut.String(), `"reason_code":"VERIFICATION_PASSED"`) || strings.Contains(verifyOut.String(), "candidate") {
		t.Fatalf("verify output=%s", verifyOut.String())
	}
	db, err := state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.GitCommitIntent(context.Background(), "verify-1"); err == nil {
		t.Fatal("verify created durable commit intent")
	}
	if refs, err := exec.Command("git", "-C", root, "show-ref", "--verify", "refs/autogit/commits/verify-1").CombinedOutput(); err == nil {
		t.Fatalf("verify created AutoGit ref: output=%s", refs)
	}
}

func TestSyncCompleteCreatesVerifiedAutoGitCommitFromCleanSession(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: baseline")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	for _, config := range [][]string{{"user.name", "AutoGit"}, {"user.email", "autogit@example.test"}} {
		if output, err := exec.Command("git", "-C", root, "config", config[0], config[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, output)
		}
	}
	var setup bytes.Buffer
	if err := run([]string{"enable", "--repo", root}, strings.NewReader(""), &setup); err != nil {
		t.Fatal(err)
	}
	var syncOut bytes.Buffer
	if err := run([]string{"sync", "--repo", root, "--session", "session-complete", "--client", "codex", "--path", "new.txt"}, strings.NewReader(""), &syncOut); err != nil {
		t.Fatalf("sync: %v output=%s", err, syncOut.String())
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verifierConfig := filepath.Join(stateDir, "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	var completeOut bytes.Buffer
	if err := run([]string{"sync", "--complete", "--all-owned", "--id", "commit-1", "--repo", root, "--session", "session-complete", "--client", "codex", "--message", "feat: capture candidate", "--verifiers", verifierConfig}, strings.NewReader(""), &completeOut); err != nil {
		t.Fatalf("sync complete: %v output=%s", err, completeOut.String())
	}
	if !strings.Contains(completeOut.String(), `"reason_code":"SYNC_COMMITTED"`) || strings.Contains(completeOut.String(), "candidate") {
		t.Fatalf("sync complete output=%s", completeOut.String())
	}
	if refs, err := exec.Command("git", "-C", root, "show-ref", "--verify", "refs/autogit/commits/commit-1").CombinedOutput(); err != nil || len(refs) == 0 {
		t.Fatalf("missing AutoGit commit ref: err=%v output=%s", err, refs)
	}
	var statusOut bytes.Buffer
	if err := run([]string{"status", "--repo", root}, strings.NewReader(""), &statusOut); err != nil {
		t.Fatalf("status after sync complete: %v output=%s", err, statusOut.String())
	}
	if !strings.Contains(statusOut.String(), `"commits":{"count":1`) || !strings.Contains(statusOut.String(), `"verifications":{"count":1`) {
		t.Fatalf("sync complete did not project durable domain facts: %s", statusOut.String())
	}
}

func TestSyncCompleteResolvesTrustedPolicyProfileForGeneratedIntent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "chore: baseline").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	verifierConfig := filepath.Join(t.TempDir(), "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"enable", "--repo", root, "--local", "--auto-complete", "--verifiers", verifierConfig}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"sync", "--repo", root, "--session", "profile-sync", "--client", "codex", "--path", "new.txt"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"sync", "--complete", "--all-owned", "--id", "profile-commit", "--repo", root, "--session", "profile-sync", "--client", "codex", "--intent", "fix parser accepts paths"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("sync complete: %v output=%s", err, out.String())
	}
	if subject := strings.TrimSpace(gitOutput(t, root, "show", "-s", "--format=%s", "refs/autogit/commits/profile-commit")); subject != "fix: fix parser accepts paths" {
		t.Fatalf("generated subject=%q output=%s", subject, out.String())
	}
}

func TestSyncAllOwnedResumesHookBaselineAndExcludesPreexistingWork(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("committed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: baseline")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	for _, config := range [][]string{{"user.name", "AutoGit"}, {"user.email", "autogit@example.test"}} {
		if output, err := exec.Command("git", "-C", root, "config", config[0], config[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("pre-existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"enable", "--repo", root, "--local"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(stateDir, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(hookEvent("01J7N6X8P5K2V4W6NQ8M9ABCDJ", "session.started", "start-hook", `,"session_id":"hook-session"`, "", "")), &raw); err != nil {
		t.Fatal(err)
	}
	raw["scope"].(map[string]any)["repo_id"] = info.RepoID
	raw["scope"].(map[string]any)["worktree_id"] = info.WorktreeID
	raw["project"] = map[string]any{"candidate_root": root}
	hookInput, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"hook"}, bytes.NewReader(hookInput), &bytes.Buffer{}); err != nil {
		t.Fatalf("session hook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned.txt"), []byte("session-owned\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verifierConfig := filepath.Join(stateDir, "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"sync", "--complete", "--all-owned", "--id", "hook-commit", "--repo", root, "--session", "hook-session", "--client", "codex", "--message", "feat: capture hook work", "--verifiers", verifierConfig}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("sync all-owned: %v output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"reason_code":"SYNC_COMMITTED"`) {
		t.Fatalf("sync output=%s", out.String())
	}
	ref := "refs/autogit/commits/hook-commit"
	if output, err := exec.Command("git", "-C", root, "show", ref+":tracked.txt").CombinedOutput(); err != nil || string(output) != "committed\n" {
		t.Fatalf("pre-existing file entered candidate: err=%v content=%q", err, output)
	}
	if output, err := exec.Command("git", "-C", root, "show", ref+":owned.txt").CombinedOutput(); err != nil || string(output) != "session-owned\n" {
		t.Fatalf("owned file missing from candidate: err=%v content=%q", err, output)
	}
}

func TestVerifyAllOwnedResumesHookBaselineWithoutCreatingCommitIntent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("committed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	commit := exec.Command("git", "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: baseline")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	for _, config := range [][]string{{"user.name", "AutoGit"}, {"user.email", "autogit@example.test"}} {
		if output, err := exec.Command("git", "-C", root, "config", config[0], config[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("pre-existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"enable", "--repo", root, "--local"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(stateDir, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(hookEvent("01J7N6X8P5K2V4W6NQ8M9ABCEK", "session.started", "verify-hook", `,"session_id":"verify-hook-session"`, "", "")), &raw); err != nil {
		t.Fatal(err)
	}
	raw["scope"].(map[string]any)["repo_id"] = info.RepoID
	raw["scope"].(map[string]any)["worktree_id"] = info.WorktreeID
	raw["project"] = map[string]any{"candidate_root": root}
	hookInput, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"hook"}, bytes.NewReader(hookInput), &bytes.Buffer{}); err != nil {
		t.Fatalf("session hook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned.txt"), []byte("session-owned\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verifierConfig := filepath.Join(stateDir, "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"verify", "--id", "verify-hook-1", "--repo", root, "--session", "verify-hook-session", "--client", "codex", "--message", "feat: verify hook work", "--verifiers", verifierConfig, "--all-owned"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("verify all-owned: %v output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"reason_code":"VERIFICATION_PASSED"`) || strings.Contains(out.String(), "session-owned") {
		t.Fatalf("verify output=%s", out.String())
	}
	db, err := state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.GitCommitIntent(context.Background(), "verify-hook-1"); err == nil {
		t.Fatal("verify created durable commit intent")
	}
	if refs, err := exec.Command("git", "-C", root, "show-ref", "--verify", "refs/autogit/commits/verify-hook-1").CombinedOutput(); err == nil {
		t.Fatalf("verify created AutoGit ref: output=%s", refs)
	}
}

func TestDomainFactProjectionTracksPublishPostcondition(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.db")
	info := repository.Info{RepoID: "sha256:" + strings.Repeat("a", 64), WorktreeID: "sha256:" + strings.Repeat("b", 64)}
	options := syncOptions{ID: "commit-facts", Session: "session-facts"}
	result := localworkflow.Result{
		Commit:       gittransaction.Commit{SHA: strings.Repeat("c", 40), ParentSHA: strings.Repeat("d", 40), CandidateDigest: "sha256:" + strings.Repeat("e", 64), MessageDigest: "sha256:" + strings.Repeat("f", 64)},
		PolicyDigest: "sha256:" + strings.Repeat("1", 64), GuardDigest: "sha256:" + strings.Repeat("2", 64), OwnershipDigest: "sha256:" + strings.Repeat("3", 64),
		Verification: verification.VerificationResult{VerifierSetDigest: "sha256:" + strings.Repeat("4", 64), EvidenceDigest: "sha256:" + strings.Repeat("5", 64)},
	}
	intent := state.GitCommitIntentRecord{CreatedAt: 1}
	p := policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe"}
	if err := emitSyncDomainFacts(context.Background(), statePath, p, info, options, result, intent); err != nil {
		t.Fatalf("emit commit facts: %v", err)
	}
	job := state.PushJob{ID: "commit-facts", Owner: "owner", Name: "repo", Ref: "main", CommitSHA: result.Commit.SHA, RemoteDigest: "sha256:" + strings.Repeat("6", 64), State: state.PushSucceeded, CreatedAt: 2}
	if err := emitPublishDomainFacts(context.Background(), statePath, p, info, job, nil); err != nil {
		t.Fatalf("emit push facts: %v", err)
	}
	store, err := events.OpenStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	data, _, err := store.LifecycleProjection(info.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	var projected lifecycle.State
	if err := json.Unmarshal(data, &projected); err != nil {
		t.Fatal(err)
	}
	push, ok := projected.Pushes[job.ID]
	if !ok || push.State != lifecycle.PushSucceededStatus || push.CommitSHA != job.CommitSHA || push.RemoteDigest != job.RemoteDigest {
		t.Fatalf("projected push=%+v all=%+v", push, projected.Pushes)
	}
}

func TestStoredPushDomainFactsPropagatesJobReadFailure(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	info := repository.Info{RepoID: "sha256:" + strings.Repeat("a", 64), WorktreeID: "sha256:" + strings.Repeat("b", 64)}
	err = emitStoredPushDomainFacts(context.Background(), db, filepath.Join(t.TempDir(), "state.db"), policy.Policy{}, info, "missing", nil)
	if err == nil {
		t.Fatal("closed push-job store was treated as a successful fact projection")
	}
}

func TestRetryRequiresExplicitJobRepositoryAndRemote(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	var out bytes.Buffer
	if err := run([]string{"retry"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing retry arguments error=%v output=%s", err, out.String())
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid retry created state: %v", entries)
	}
	if err := run([]string{"retry", "--id", "job"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing retry repository/remote error=%v", err)
	}
	if err := run([]string{"retry", "--id", "job", "--repo", "/tmp/repo"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing retry remote error=%v", err)
	}
}

func TestPublishRequiresExplicitDestinationBeforeProviderDiscovery(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	var out bytes.Buffer
	err := run([]string{"publish", "--id", "commit-1", "--repo", t.TempDir()}, strings.NewReader(""), &out)
	if err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("publish accepted incomplete destination: %v output=%s", err, out.String())
	}
	entries, readErr := os.ReadDir(stateDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid publish created state before validation: %v", entries)
	}
}

func TestRemoteCreateRequiresExplicitIdentityAndConsent(t *testing.T) {
	got, err := parseRemoteArgs([]string{"create", "--id", "remote-1", "--repo", "/tmp/repo", "--alias", "origin", "--owner", "owner", "--name", "repo"})
	if err != nil || got.Visibility != "private" || got.Alias != "origin" {
		t.Fatalf("options=%+v err=%v", got, err)
	}
	if _, err := parseRemoteArgs([]string{"create", "--id", "remote-1", "--repo", "/tmp/repo", "--alias", "origin", "--owner", "owner", "--name", "repo", "--visibility", "public"}); err == nil || !strings.HasPrefix(err.Error(), "E_CONSENT:") {
		t.Fatalf("public remote creation without consent error=%v", err)
	}
	if _, err := parseRemoteArgs([]string{"create", "--id", "remote-1", "--repo", "/tmp/repo", "--alias", "origin", "--owner", "owner", "--name", "repo", "--unknown", "x"}); err == nil {
		t.Fatal("unknown remote creation option accepted")
	}
}

func TestRemoteCreateHonorsLocalOnlyPolicyBeforeProviderDiscovery(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"enable", "--repo", root, "--local"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"remote", "create", "--id", "remote-local", "--repo", root, "--alias", "origin", "--owner", "owner", "--name", "repo"}, strings.NewReader(""), &out)
	if err == nil || !strings.HasPrefix(err.Error(), "E_LOCAL_ONLY:") {
		t.Fatalf("local-only remote creation error=%v output=%s", err, out.String())
	}
}

func TestEnableLocalFlagRemainsLocalWithFollowingRepositoryFlag(t *testing.T) {
	p := enabledPolicy([]string{"--local", "--repo", "/tmp/project"}, 3)
	if p.Tracking != "local" || !p.LocalOnly || p.Provider != "" {
		t.Fatalf("local policy was changed by following value flag: %+v", p)
	}
}

func TestValidateEnableAutomaticCompletionRequiresTrustedVerifierConfig(t *testing.T) {
	if err := validateEnableArgs([]string{"--repo", "/tmp/project", "--auto-complete"}, true); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing verifier config accepted: %v", err)
	}
	if err := validateEnableArgs([]string{"--repo", "/tmp/project", "--auto-complete", "--verifiers", "/tmp/verifiers.json"}, true); err != nil {
		t.Fatalf("valid automatic completion options rejected: %v", err)
	}
}

func TestPublishPrivateUsesExactCommitAndRecordsDurablePush(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gitPath, "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Example\n\n## Usage\nrun it\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gitPath, "-C", root, "add", "--", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command(gitPath, "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: baseline").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	if output, err := exec.Command(gitPath, "-C", root, "remote", "add", "origin", "https://github.com/owner/repo").CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, output)
	}
	for _, config := range [][]string{{"user.name", "AutoGit"}, {"user.email", "autogit@example.test"}} {
		if output, err := exec.Command(gitPath, "-C", root, "config", config[0], config[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, output)
		}
	}
	if err := run([]string{"enable", "--repo", root, "--provider", "github", "--owner", "owner", "--destination", "owner/repo", "--visibility", "private"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(stateDir, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(mustCommand(t, gitPath, "-C", root, "rev-parse", "HEAD")))
	tree := strings.TrimSpace(string(mustCommand(t, gitPath, "-C", root, "rev-parse", "HEAD^{tree}")))
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	db, err := state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutGitCommitIntent(context.Background(), state.GitCommitIntent{ID: "commit-publish", RepoDir: info.Root, Ref: "refs/autogit/commits/commit-publish", TreeOID: tree, ParentSHA: sha, Message: "feat: publish candidate\n", CandidateDigest: digest('a'), MessageDigest: digest('b'), SnapshotDigest: digest('c'), PolicyDigest: digest('d'), VerifierDigest: digest('e'), GuardDigest: digest('f')}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.RecordGitCommit(context.Background(), "commit-publish", sha); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if output, err := exec.Command(gitPath, "-C", root, "update-ref", "refs/autogit/commits/commit-publish", sha).CombinedOutput(); err != nil {
		t.Fatalf("git autogit ref: %v: %s", err, output)
	}

	fakeBin := t.TempDir()
	ghState := filepath.Join(fakeBin, "gh-state")
	if runtime.GOOS == "windows" {
		installWindowsPublishFixtures(t, fakeBin, gitPath, ghState, sha)
	} else {
		ghScript := "#!/bin/sh\nif [ -f '" + ghState + "' ]; then printf '%s\\n' '" + sha + "'; else : > '" + ghState + "'; printf '%s\\n' 'Not Found' >&2; exit 1; fi\n"
		gitScript := "#!/bin/sh\ncase \"$*\" in *'remote get-url --push -- origin'*) printf '%s\\n' 'https://github.com/owner/repo';; *' push -- origin '*) exit 0;; *) exec '" + gitPath + "' \"$@\";; esac\n"
		if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(ghScript), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fakeBin, "git"), []byte(gitScript), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out bytes.Buffer
	if err := run([]string{"publish", "--id", "commit-publish", "--repo", root, "--remote", "origin", "--owner", "owner", "--name", "repo", "--ref", "main", "--visibility", "private"}, strings.NewReader(""), &out); err != nil {
		if runtime.GOOS == "windows" {
			if log, readErr := os.ReadFile(filepath.Join(fakeBin, "git-log")); readErr == nil {
				t.Fatalf("publish: %v output=%s git-log=%s", err, out.String(), log)
			}
		}
		t.Fatalf("publish: %v output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"reason_code":"PUBLISH_SUCCEEDED"`) || !strings.Contains(out.String(), `"commit_sha":"`+sha+`"`) {
		t.Fatalf("publish output=%s", out.String())
	}
	db, err = state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := db.PushJob("commit-publish")
	db.Close()
	if err != nil || job.State != state.PushSucceeded || job.CommitSHA != sha || job.RemoteDigest == "" {
		t.Fatalf("push job=%+v err=%v", job, err)
	}
	if err := run([]string{"enable", "--repo", root, "--provider", "github", "--owner", "owner", "--destination", "owner/repo", "--visibility", "public", "--public-consent"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var publicOut bytes.Buffer
	if err := run([]string{"publish", "--id", "commit-publish", "--repo", root, "--remote", "origin", "--owner", "owner", "--name", "repo", "--ref", "main", "--visibility", "public", "--mode", "public", "--public-consent"}, strings.NewReader(""), &publicOut); err != nil {
		t.Fatalf("public preflight: %v output=%s", err, publicOut.String())
	}
	if !strings.Contains(publicOut.String(), `"reason_code":"PUBLIC_PREFLIGHT_REQUIRED"`) || !strings.Contains(publicOut.String(), `"preflight"`) || !strings.Contains(publicOut.String(), `"repository":"repo"`) {
		t.Fatalf("public preflight output=%s", publicOut.String())
	}
}

func installWindowsPublishFixtures(t *testing.T, dir, realGit, ghState, sha string) {
	t.Helper()
	source := filepath.Join(dir, "publish-fixture.go")
	program := `package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	if logPath := os.Getenv("AUTOGIT_TEST_GIT_LOG"); name == "git" && logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err == nil {
			defer f.Close()
			fmt.Fprintln(f, strings.Join(os.Args[1:], "\x00"))
		}
	}
	if name == "gh" {
		state := os.Getenv("AUTOGIT_TEST_GH_STATE")
		if _, err := os.Stat(state); err == nil {
			fmt.Fprintln(os.Stdout, os.Getenv("AUTOGIT_TEST_GH_SHA"))
			return
		}
		_ = os.WriteFile(state, []byte{}, 0600)
		fmt.Fprintln(os.Stderr, "Not Found")
		os.Exit(1)
	}
	args := strings.Join(os.Args[1:], " ")
	if strings.Contains(args, "remote get-url --push -- origin") {
		fmt.Fprintln(os.Stdout, "https://github.com/owner/repo")
		return
	}
	if strings.Contains(args, "push -- origin") {
		return
	}
	command := exec.Command(os.Getenv("AUTOGIT_TEST_REAL_GIT"), os.Args[1:]...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "publish-fixture.exe")
	build := exec.Command("go", "build", "-o", fixture, source)
	build.Env = append(os.Environ(), "GO111MODULE=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Windows publish fixture: %v: %s", err, output)
	}
	binary, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gh.exe", "git.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), binary, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AUTOGIT_TEST_GH_STATE", ghState)
	t.Setenv("AUTOGIT_TEST_GH_SHA", sha)
	t.Setenv("AUTOGIT_TEST_REAL_GIT", realGit)
	t.Setenv("AUTOGIT_TEST_GIT_LOG", filepath.Join(dir, "git-log"))
}

func TestParsePublishPublicEvidenceOptions(t *testing.T) {
	got, err := parsePublishArgs([]string{
		"--id", "commit-1", "--repo", "/repo", "--remote", "origin",
		"--owner", "owner", "--name", "repo", "--ref", "feature/public",
		"--visibility", "public", "--mode", "public", "--public-consent",
		"--confirm-destination", "--feature-branch-approved", "--tests", "passed",
		"--ci", "present", "--verifiers", "/trusted/verifiers.json",
		"--license", "LICENSE", "--protected-branch", "--status-checks-required",
		"--status-checks-passed",
	})
	if err != nil {
		t.Fatalf("parse public publish evidence: %v", err)
	}
	if !got.PublicConsent || !got.ConfirmDestination || !got.FeatureBranchApproved || !got.ProtectedBranch || !got.StatusChecksRequired || !got.StatusChecksPassed {
		t.Fatalf("boolean evidence was not retained: %+v", got)
	}
	if got.Tests != publication.StatusPassed || got.CI != publication.StatusPresent || got.Verifiers != "/trusted/verifiers.json" || got.License != "LICENSE" {
		t.Fatalf("evidence values were not retained: %+v", got)
	}
}

func TestPublicPreflightCanAuthorizeOnlyWithExactTreeEvidence(t *testing.T) {
	stateRoot := t.TempDir()
	root := t.TempDir()
	if err := os.Chmod(stateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gitPath, "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	for path, content := range map[string]string{
		"README.md": "# Example\n\n## Usage\nrun it\n",
		"LICENSE":   "Copyright AutoGit\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command(gitPath, "-C", root, "add", "--", "README.md", "LICENSE").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command(gitPath, "-C", root, "-c", "user.name=AutoGit", "-c", "user.email=autogit@example.test", "commit", "-qm", "feat: publishable candidate").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	sha := strings.TrimSpace(string(mustCommand(t, gitPath, "-C", root, "rev-parse", "HEAD")))
	tree := strings.TrimSpace(string(mustCommand(t, gitPath, "-C", root, "rev-parse", "HEAD^{tree}")))
	verifierPath := filepath.Join(stateRoot, "verifiers.json")
	if err := os.WriteFile(verifierPath, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	registry, err := verification.LoadTrustedRegistryFile(verifierPath, stateRoot, 0)
	if err != nil {
		t.Fatal(err)
	}
	intent := state.GitCommitIntentRecord{Intent: state.GitCommitIntent{RepoDir: root, TreeOID: tree, ParentSHA: sha, CandidateDigest: "sha256:" + strings.Repeat("a", 64), PolicyDigest: "sha256:" + strings.Repeat("b", 64), VerifierDigest: registry.VerifierSetDigest, GuardDigest: "sha256:" + strings.Repeat("c", 64)}, SHA: sha, State: state.CommitCreated}
	report := buildPublicPreflight(context.Background(), stateRoot, root, intent, publishOptions{Owner: "owner", Name: "repo", Ref: "feature/public", Visibility: publication.VisibilityPublic, Mode: publication.ModePublic, License: "LICENSE", Verifiers: verifierPath, Tests: publication.StatusPassed, CI: publication.StatusPresent, PublicConsent: true, ConfirmDestination: true, FeatureBranchApproved: true}, policy.Policy{Workflow: publication.WorkflowSafe})
	if !report.CanPublishPublic() {
		t.Fatalf("complete evidence was blocked: %+v", report)
	}
	if report.Verification.Required != 1 || report.Verification.PassedCount != 1 || !report.CandidateScan.Passed || !report.HistoryScan.Passed {
		t.Fatalf("evidence was not reflected in report: %+v", report)
	}
}

func TestInstallListDiscoversAllAdapterCapabilitiesWithoutStateMutation(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	var out bytes.Buffer
	if err := run([]string{"install", "--list"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("install --list: %v output=%s", err, out.String())
	}
	for _, name := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "commandcode"} {
		if !strings.Contains(out.String(), `"adapter":"`+name+`"`) {
			t.Fatalf("adapter %q missing from discovery output=%s", name, out.String())
		}
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("install --list mutated state: %v", entries)
	}
}

func TestTrustedExecutableRejectsFinalSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink executable policy is covered by native Windows process tests")
	}
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real-tool")
	if err := os.WriteFile(realPath, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, filepath.Join(dir, "tool")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, err := trustedExecutable("tool"); err == nil {
		t.Fatal("trusted executable accepted a final-component symlink")
	}
}

func TestTrustedExecutableResolvesTrustedGitSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink executable policy is covered by native Windows process tests")
	}
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real-git")
	if err := os.WriteFile(realPath, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(realPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	resolved, err := trustedExecutable("git")
	if err != nil {
		t.Fatalf("trusted Git symlink rejected: %v", err)
	}
	if resolved != wantPath {
		t.Fatalf("resolved Git path=%q, want %q", resolved, wantPath)
	}
}

func mustCommand(t *testing.T, executable string, args ...string) []byte {
	t.Helper()
	output, err := exec.Command(executable, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v: %v: %s", executable, args, err, output)
	}
	return output
}

func TestRetryRejectsTerminalJobBeforeProviderDiscovery(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	jobID := "blocked-job"
	sha := strings.Repeat("a", 40)
	if err := db.WithTx(context.Background(), func(tx *state.Tx) error {
		return tx.PutPushJob(state.PushJob{ID: jobID, Owner: "owner", Name: "repo", Ref: "main", CommitSHA: sha, State: state.PushBlocked})
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = run([]string{"retry", "--id", jobID, "--repo", root, "--remote", "origin"}, strings.NewReader(""), &out)
	if err == nil || !strings.HasPrefix(err.Error(), "E_STATE:") {
		t.Fatalf("terminal retry error=%v output=%s", err, out.String())
	}
}

func TestRetryCoordinatorUsesDurableWriterLease(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	coord := retryCoordinator(db, &provider.LocalOnlyProvider{}, "retry-owner")
	if coord.Lease == nil {
		t.Fatal("retry coordinator has no durable writer lease")
	}
	if coord.Owner != "retry-owner" {
		t.Fatalf("owner=%q", coord.Owner)
	}
}

func TestSyncRequiresExplicitSessionClientRepositoryAndPaths(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	var out bytes.Buffer
	if err := run([]string{"sync"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing sync arguments error=%v output=%s", err, out.String())
	}
	if err := run([]string{"sync", "--repo", t.TempDir(), "--session", "s", "--client", "codex"}, strings.NewReader(""), &out); err == nil || !strings.HasPrefix(err.Error(), "E_SCOPE:") {
		t.Fatalf("missing sync path error=%v", err)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid sync created state: %v", entries)
	}
}

func TestSyncCapturesRedactedBaselineAndPersistsSession(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"sync", "--repo", root, "--session", "session-1", "--client", "codex", "--path", "owned.txt"}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "owned.txt") || strings.Contains(out.String(), "candidate") || !strings.Contains(out.String(), `"reason_code":"SYNC_BASELINE_CAPTURED"`) {
		t.Fatalf("sync output leaked or lacked result: %s", out.String())
	}
	key, err := os.ReadFile(filepath.Join(stateDir, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.Session(context.Background(), "session-1")
	if err != nil || session.RepositoryID != info.RepoID || session.ClientID != "codex" || session.BaselinePathsDigest == "" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
}

func TestStatusWithoutEventsReturnsStableEmptyLifecycleSummary(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", state)
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
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
	repositorySummary, ok := result["repository"].(map[string]any)
	if !ok || repositorySummary["status_digest"] == "" || repositorySummary["paths_digest"] == "" || repositorySummary["changed_path_count"] != float64(0) {
		t.Fatalf("repository summary=%v", result["repository"])
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
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
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
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
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
		if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
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
	stateDir := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateDir)
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
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("read-only config explain created state: %v", entries)
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

func TestHookCompletionProfileCommitsDurableSessionCandidate(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := run([]string{"enable", "--repo", root, "--local"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(stateRoot, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	verifierConfig := filepath.Join(stateRoot, "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	withScope := func(input string, task bool) []byte {
		var raw map[string]any
		if err := json.Unmarshal([]byte(input), &raw); err != nil {
			t.Fatal(err)
		}
		scope := raw["scope"].(map[string]any)
		scope["repo_id"] = info.RepoID
		scope["worktree_id"] = info.WorktreeID
		if task {
			scope["task_id"] = "task"
		}
		raw["project"] = map[string]any{"candidate_root": root}
		encoded, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	started := withScope(hookEventWithCapabilities("01J7N6X8P5K2V4W6NQ8M9ABCDJ", "session.started", "profile-start", `,"session_id":"profile-session"`, `{"queue_state":"none","task_boundaries":"native"}`), false)
	if err := runHook(nil, bytes.NewReader(started), &bytes.Buffer{}); err != nil {
		t.Fatalf("session.started: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "candidate.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	taskStarted := withScope(hookEventWithCapabilities("01J7N6X8P5K2V4W6NQ8M9ABCDK", "task.started", "profile-task-start", `,"session_id":"profile-session","task_id":"task"`, `{"queue_state":"none","task_boundaries":"native"}`), true)
	if err := runHook(nil, bytes.NewReader(taskStarted), &bytes.Buffer{}); err != nil {
		t.Fatalf("task.started: %v", err)
	}
	completed := withScope(hookEventWithCapabilities("01J7N6X8P5K2V4W6NQ8M9ABCDM", "task.completed", "profile-task-complete", `,"session_id":"profile-session","task_id":"task"`, `{"queue_state":"none","task_boundaries":"native"}`), true)
	var out bytes.Buffer
	if err := runHook([]string{"--message", "feat: commit lifecycle candidate", "--verifiers", verifierConfig}, bytes.NewReader(completed), &out); err != nil {
		t.Fatalf("task.completed: %v output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"action":"commit"`) || !strings.Contains(out.String(), `"reason_code":"SESSION_COMMITTED"`) {
		t.Fatalf("completion output=%s", out.String())
	}
	refs := strings.TrimSpace(gitOutput(t, root, "for-each-ref", "--format=%(refname)", "refs/autogit/commits"))
	if refs == "" || strings.Count(refs, "refs/autogit/commits/") != 1 {
		t.Fatalf("autogit refs=%q", refs)
	}
}

func TestHookAutomaticCompletionUsesTrustedPolicyAndTaskIntent(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	verifierConfig := filepath.Join(t.TempDir(), "verifiers.json")
	if err := os.WriteFile(verifierConfig, testVerifierConfig(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"enable", "--repo", root, "--local", "--auto-complete", "--verifiers", verifierConfig}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(stateRoot, "identity.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		t.Fatal(err)
	}
	withScope := func(input string, task bool) []byte {
		var raw map[string]any
		if err := json.Unmarshal([]byte(input), &raw); err != nil {
			t.Fatal(err)
		}
		scope := raw["scope"].(map[string]any)
		scope["repo_id"], scope["worktree_id"] = info.RepoID, info.WorktreeID
		if task {
			scope["task_id"] = "auto-task"
		}
		raw["project"] = map[string]any{"candidate_root": root}
		encoded, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	started := withScope(hookEventWithCapabilities("01J7N6X8P5K2V4W6NQ8M9ABCDP", "session.started", "auto-start", `,"session_id":"auto-session"`, `{"queue_state":"none","task_boundaries":"native"}`), false)
	if err := runHook(nil, bytes.NewReader(started), &bytes.Buffer{}); err != nil {
		t.Fatalf("session.started: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "candidate.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	taskStarted := withScope(hookEventWithCapabilities("01J7N6X8P5K2V4W6NQ8M9ABCDQ", "task.started", "auto-task-start", `,"session_id":"auto-session","task_id":"auto-task"`, `{"queue_state":"none","task_boundaries":"native"}`), true)
	if err := runHook(nil, bytes.NewReader(taskStarted), &bytes.Buffer{}); err != nil {
		t.Fatalf("task.started: %v", err)
	}
	completed := withScope(hookEventWithCapabilities("01J7N6X8P5K2V4W6NQ8M9ABCDR", "task.completed", "auto-task-complete", `,"session_id":"auto-session","task_id":"auto-task"`, `{"queue_state":"none","task_boundaries":"native"}`), true)
	var completion map[string]any
	if err := json.Unmarshal(completed, &completion); err != nil {
		t.Fatal(err)
	}
	completion["payload"] = map[string]any{"reason": "fix parser accepts paths"}
	completed, err = json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runHook(nil, bytes.NewReader(completed), &out); err != nil {
		t.Fatalf("task.completed: %v output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"action":"commit"`) || !strings.Contains(out.String(), `"reason_code":"SESSION_COMMITTED"`) {
		t.Fatalf("completion output=%s", out.String())
	}
	ref := strings.TrimSpace(gitOutput(t, root, "for-each-ref", "--format=%(refname)", "refs/autogit/commits"))
	if ref == "" {
		t.Fatal("automatic completion did not create an AutoGit ref")
	}
	message := strings.TrimSpace(gitOutput(t, root, "show", "-s", "--format=%s", ref))
	if message != "fix: fix parser accepts paths" {
		t.Fatalf("generated commit subject=%q", message)
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
