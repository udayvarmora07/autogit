package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"autogit/internal/adapters"
	"autogit/internal/app"
	"autogit/internal/events"
	"autogit/internal/install"
	"autogit/internal/lifecycle"
	"autogit/internal/policy"
	"autogit/internal/repository"
	"autogit/internal/security"
	"autogit/internal/verification"
)

type cliError struct{ Code, Message string }

func (e cliError) Error() string { return e.Code + ": " + e.Message }
func stateDir() (string, error) {
	if p := os.Getenv("AUTOGIT_STATE_DIR"); p != "" {
		return p, nil
	}
	p, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "autogit"), nil
}
func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		code := "E_INTERNAL"
		var ce cliError
		if errors.As(err, &ce) {
			code = ce.Code
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"error": map[string]string{"code": code, "message": safeMessage(err.Error())}})
		os.Exit(1)
	}
}
func safeMessage(s string) string {
	s = security.Redact(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}
func run(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, _ = io.WriteString(out, "autogit commands: install doctor enable disable status plan hook verify sync retry logs uninstall config explain\n")
		return nil
	}
	if args[0] == "hook" {
		return runHook(args[1:], in, out)
	}
	cmd := args[0]
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if cmd == "logs" {
		root := flag(args[1:], "--repo")
		if err := validateLogsArgs(args[1:]); err != nil {
			return err
		}
		// Resolve the repository before creating the state directory. This keeps
		// malformed or out-of-scope read-only requests side-effect free.
		key, _, keyErr := identityKeyForRead(dir)
		if keyErr != nil {
			return keyErr
		}
		if _, discoverErr := repository.DiscoverWithKey(root, key); discoverErr != nil {
			return cliError{"E_SCOPE", discoverErr.Error()}
		}
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	identityKey, err := loadIdentityKey(dir)
	if err != nil {
		return err
	}
	s, err := events.OpenStore(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	switch cmd {
	case "enable", "disable":
		root := flag(args[1:], "--repo")
		if root == "" {
			return cliError{"E_SCOPE", "--repo is required"}
		}
		info, err := repository.DiscoverWithKey(root, identityKey)
		if err != nil {
			return cliError{"E_SCOPE", err.Error()}
		}
		p := loadPolicy(dir, info.RepoID)
		if cmd == "disable" {
			p = policy.Policy{Tracking: "no", Version: p.Version + 1}
		} else {
			p.Tracking = "local"
			p.LocalOnly = true
			p.Visibility = "private"
			p.Workflow = "safe"
			p.Version++
		}
		if err = savePolicy(dir, info.RepoID, p); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "POLICY_UPDATED", "repo_id": info.RepoID})
	case "status", "plan", "config":
		if cmd == "config" && len(args) > 1 && args[1] != "explain" {
			return cliError{"E_USAGE", "config supports explain"}
		}
		verifierPath := flag(args[1:], "--verifiers")
		if verifierPath != "" && cmd != "config" {
			return cliError{"E_USAGE", "--verifiers is supported by config explain"}
		}
		root := flag(args[1:], "--repo")
		var repoID string
		if root != "" {
			info, discoverErr := repository.DiscoverWithKey(root, identityKey)
			if discoverErr != nil {
				return cliError{"E_SCOPE", discoverErr.Error()}
			}
			repoID = info.RepoID
		}
		if (cmd == "status" || cmd == "plan") && repoID == "" {
			return cliError{"E_SCOPE", "--repo is required for read-only inspection"}
		}
		p := loadPolicy(dir, repoID)
		result := map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "STATUS", "policy": p}
		if repoID != "" {
			result["repo_id"] = repoID
		}
		if cmd == "plan" {
			result["reason_code"] = "READ_ONLY_PLAN"
		}
		if cmd == "config" {
			result["reason_code"] = "CONFIG_EXPLAIN"
			if verifierPath != "" {
				registry, loadErr := verification.LoadRegistryFile(verifierPath, 1<<20)
				if loadErr != nil {
					return cliError{"E_VERIFIER_CONFIG", safeMessage(loadErr.Error())}
				}
				result["verifier_count"] = len(registry.Specs)
				result["verifier_set_digest"] = registry.VerifierSetDigest
				result["verifier_config_digest"] = registry.ConfigDigest
			}
		}
		if cmd == "status" {
			projection, projectionErr := lifecycleStatus(s, repoID)
			if projectionErr != nil {
				return projectionErr
			}
			result["lifecycle"] = projection
		}
		return json.NewEncoder(out).Encode(result)
	case "doctor":
		_, gitErr := exec.LookPath("git")
		result := map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "DOCTOR_OK", "git_available": gitErr == nil, "state_dir": "configured"}
		if gitErr != nil {
			result["reason_code"] = "GIT_UNAVAILABLE"
		}
		return json.NewEncoder(out).Encode(result)
	case "install", "uninstall":
		adapterName, configPath, root := flag(args[1:], "--adapter"), flag(args[1:], "--path"), flag(args[1:], "--root")
		if adapterName == "" || configPath == "" || root == "" {
			return cliError{"E_SCOPE", "--adapter, --path, and --root are required"}
		}
		entry, entryErr := install.ClientInstallationFor(adapterName)
		if entryErr != nil {
			return cliError{"E_UNSUPPORTED", entryErr.Error()}
		}
		if cmd == "install" {
			p, planErr := install.PlanClient(entry, configPath, []string{root}, root)
			if planErr != nil {
				if errors.Is(planErr, install.ErrUnsupported) {
					return cliError{"E_UNSUPPORTED", planErr.Error()}
				}
				return cliError{"E_SCOPE", planErr.Error()}
			}
			if applyErr := install.ApplyClient(p); applyErr != nil {
				if errors.Is(applyErr, install.ErrUnsupported) {
					return cliError{"E_UNSUPPORTED", applyErr.Error()}
				}
				return cliError{"E_INSTALL", applyErr.Error()}
			}
		} else if uninstallErr := install.UninstallClient(entry, configPath, []string{root}, root); uninstallErr != nil {
			if errors.Is(uninstallErr, install.ErrUnsupported) {
				return cliError{"E_UNSUPPORTED", uninstallErr.Error()}
			}
			return cliError{"E_INSTALL", uninstallErr.Error()}
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": strings.ToUpper(cmd) + "_APPLIED"})
	case "logs":
		root := flag(args[1:], "--repo")
		info, discoverErr := repository.DiscoverWithKey(root, identityKey)
		if discoverErr != nil {
			return cliError{"E_SCOPE", discoverErr.Error()}
		}
		limit := 50
		if raw, provided := flagValue(args[1:], "--limit"); provided {
			limit, _ = strconv.Atoi(raw)
		}
		logs, logsErr := s.Logs(context.Background(), info.RepoID, limit)
		if logsErr != nil {
			if code := events.CodeOf(logsErr); code != "" {
				return cliError{code, logsErr.Error()}
			}
			return logsErr
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "LOGS", "logs": logs})
	case "verify", "sync", "retry":
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "unsupported", "action": "none", "reason_code": "E_UNIMPLEMENTED", "operation": cmd})
	default:
		return cliError{"E_USAGE", "unknown command"}
	}
}

func runHook(args []string, in io.Reader, out io.Writer) error {
	b, err := io.ReadAll(io.LimitReader(in, (64<<10)+1))
	if err != nil {
		return cliError{"E_SCHEMA", "cannot read event"}
	}
	if len(b) > 64<<10 {
		return cliError{"E_SCHEMA", "event exceeds input limit"}
	}
	statePath, err := stateDir()
	if err != nil {
		return err
	}
	adapterName := flag(args, "--adapter")
	var pendingKey []byte
	var keyOnDisk bool
	if adapterName != "" {
		a, adapterErr := adapters.New(adapterName)
		if adapterErr != nil {
			return cliError{"E_ADAPTER", adapterErr.Error()}
		}
		root := flag(args, "--root")
		if root == "" {
			return cliError{"E_SCOPE", "--root is required with --adapter"}
		}
		pendingKey, keyOnDisk, err = identityKeyForRead(statePath)
		if err != nil {
			return err
		}
		trusted, resolveErr := repository.DiscoverWithKey(root, pendingKey)
		if resolveErr != nil {
			return cliError{"E_SCOPE", "cannot resolve approved repository"}
		}
		roots := []string{}
		roots = []string{root}
		ce, translateErr := a.Translate(b, adapters.TranslateOptions{ApprovedRoots: roots, ResolvedScope: map[string]string{"repo_id": trusted.RepoID, "worktree_id": trusted.WorktreeID}, InstallationID: flag(args, "--installation"), InstanceID: flag(args, "--instance"), EventHint: flag(args, "--event")})
		if translateErr != nil {
			return cliError{"E_SCHEMA", translateErr.Error()}
		}
		b, err = json.Marshal(ce)
		if err != nil {
			return cliError{"E_SCHEMA", "cannot encode canonical event"}
		}
	}
	e, err := events.Decode(b, 64<<10)
	if err != nil {
		return err
	}
	dir := statePath
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	key := pendingKey
	if len(key) == 0 {
		key, err = loadIdentityKey(dir)
	}
	if err != nil {
		return err
	}
	if !keyOnDisk && len(pendingKey) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "identity.key"), pendingKey, 0600); err != nil {
			return err
		}
	}
	if e.Project != nil {
		if root, ok := e.Project["candidate_root"].(string); ok {
			info, resolveErr := repository.DiscoverWithKey(root, key)
			if resolveErr != nil || e.Scope["repo_id"] != info.RepoID {
				return cliError{"E_SCOPE", "event project does not match trusted repository"}
			}
			if e.Scope["worktree_id"] != "" && e.Scope["worktree_id"] != info.WorktreeID {
				return cliError{"E_SCOPE", "event worktree does not match trusted repository"}
			}
		}
	}
	s, err := events.OpenStore(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	a := app.New(s, loadPolicy(dir, stringValue(e.Scope["repo_id"])), nil)
	a.Resolver = func(root string) (repository.Info, error) { return repository.DiscoverWithKey(root, key) }
	r, err := a.Hook(context.Background(), b)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(r)
}

type stateCounts struct {
	Count  int            `json:"count"`
	States map[string]int `json:"states"`
}

type sessionSummary struct {
	Count int    `json:"count"`
	State string `json:"state,omitempty"`
}

type lifecycleSummary struct {
	Exists        bool           `json:"exists"`
	Revision      int64          `json:"revision"`
	Session       sessionSummary `json:"session"`
	Tasks         stateCounts    `json:"tasks"`
	Candidates    stateCounts    `json:"candidates"`
	Verifications stateCounts    `json:"verifications"`
	Commits       stateCounts    `json:"commits"`
	Pushes        stateCounts    `json:"pushes"`
}

func lifecycleStatus(s *events.Store, repositoryID string) (lifecycleSummary, error) {
	data, revision, err := s.LifecycleProjection(repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return lifecycleSummary{Exists: false, Revision: 0, Tasks: stateCounts{States: map[string]int{}}, Candidates: stateCounts{States: map[string]int{}}, Verifications: stateCounts{States: map[string]int{}}, Commits: stateCounts{States: map[string]int{}}, Pushes: stateCounts{States: map[string]int{}}}, nil
	}
	if err != nil {
		return lifecycleSummary{}, cliError{"E_STATE", "cannot read lifecycle projection"}
	}
	var state lifecycle.State
	if err := json.Unmarshal(data, &state); err != nil || state.RepositoryID != repositoryID {
		return lifecycleSummary{}, cliError{"E_STATE", "invalid lifecycle projection"}
	}
	return lifecycleSummary{
		Exists:        true,
		Revision:      revision,
		Session:       sessionSummary{Count: boolCount(state.Session.ID != ""), State: string(state.Session.State)},
		Tasks:         stateCountsFromTasks(state.Tasks),
		Candidates:    stateCountsFromCandidates(state.Candidates),
		Verifications: stateCountsFromVerifications(state.Verifications),
		Commits:       stateCountsFromCommits(state.Commits),
		Pushes:        stateCountsFromPushes(state.Pushes),
	}, nil
}

func boolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}
func countStates[T any](values map[string]T, state func(T) string) stateCounts {
	out := stateCounts{Count: len(values), States: map[string]int{}}
	for _, value := range values {
		out.States[state(value)]++
	}
	return out
}
func stateCountsFromTasks(values map[string]lifecycle.Task) stateCounts {
	return countStates(values, func(value lifecycle.Task) string { return string(value.State) })
}
func stateCountsFromCandidates(values map[string]lifecycle.Candidate) stateCounts {
	return countStates(values, func(value lifecycle.Candidate) string { return string(value.State) })
}
func stateCountsFromVerifications(values map[string]lifecycle.Verification) stateCounts {
	return countStates(values, func(value lifecycle.Verification) string { return string(value.State) })
}
func stateCountsFromCommits(values map[string]lifecycle.Commit) stateCounts {
	return countStates(values, func(value lifecycle.Commit) string { return string(value.State) })
}
func stateCountsFromPushes(values map[string]lifecycle.Push) stateCounts {
	return countStates(values, func(value lifecycle.Push) string { return string(value.State) })
}

func flag(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}
func flagValue(args []string, name string) (string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			if i+1 >= len(args) {
				return "", true
			}
			return args[i+1], true
		}
	}
	return "", false
}

func validateLogsArgs(args []string) error {
	repoSeen := false
	limitSeen := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if repoSeen || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
				return cliError{"E_SCOPE", "--repo is required"}
			}
			repoSeen = true
			i++
		case "--limit":
			if limitSeen || i+1 >= len(args) {
				return cliError{"E_USAGE", "--limit must be between 1 and 200"}
			}
			limit, parseErr := strconv.Atoi(args[i+1])
			if parseErr != nil || limit < 1 || limit > 200 {
				return cliError{"E_USAGE", "--limit must be between 1 and 200"}
			}
			limitSeen = true
			i++
		default:
			return cliError{"E_USAGE", "unknown logs argument"}
		}
	}
	if !repoSeen {
		return cliError{"E_SCOPE", "--repo is required"}
	}
	return nil
}
func policyPath(dir, id string) string {
	return filepath.Join(dir, "policy-"+strings.NewReplacer(":", "_", "/", "_", `\`, "_").Replace(id)+".json")
}
func loadPolicy(dir, id string) policy.Policy {
	if id == "" {
		return policy.Policy{}
	}
	b, err := os.ReadFile(policyPath(dir, id))
	if err != nil {
		return policy.Policy{}
	}
	var p policy.Policy
	_ = json.Unmarshal(b, &p)
	return p
}
func stringValue(v any) string { s, _ := v.(string); return s }
func savePolicy(dir, id string, p policy.Policy) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := policyPath(dir, id) + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, policyPath(dir, id)); err != nil {
		return fmt.Errorf("save policy: %w", err)
	}
	return nil
}

func loadIdentityKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "identity.key")
	if key, err := os.ReadFile(path); err == nil {
		if len(key) >= 32 {
			_ = os.Chmod(path, 0600)
			return key, nil
		}
		return nil, cliError{"E_STATE", "identity key is invalid"}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func identityKeyForRead(dir string) ([]byte, bool, error) {
	path := filepath.Join(dir, "identity.key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) < 32 {
			return nil, false, cliError{"E_STATE", "identity key is invalid"}
		}
		return key, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, err
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, false, err
	}
	return key, false, nil
}
