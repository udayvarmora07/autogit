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
	"autogit/internal/coordinator"
	"autogit/internal/events"
	"autogit/internal/gittransaction"
	"autogit/internal/install"
	"autogit/internal/lifecycle"
	"autogit/internal/policy"
	"autogit/internal/provider"
	"autogit/internal/repository"
	"autogit/internal/security"
	"autogit/internal/session"
	"autogit/internal/state"
	"autogit/internal/verification"
	localworkflow "autogit/internal/workflow"
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
	if cmd == "retry" {
		if err := validateRetryArgs(args[1:]); err != nil {
			return err
		}
		return runRetry(args[1:], dir, out)
	}
	if cmd == "sync" {
		if err := validateSyncArgs(args[1:]); err != nil {
			return err
		}
		return runSync(args[1:], dir, out)
	}
	if cmd == "verify" {
		if err := validateVerifyArgs(args[1:]); err != nil {
			return err
		}
		return runVerify(args[1:], dir, out)
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
	case "verify", "sync":
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "unsupported", "action": "none", "reason_code": "E_UNIMPLEMENTED", "operation": cmd})
	default:
		return cliError{"E_USAGE", "unknown command"}
	}
}

type retryOptions struct {
	ID, Repo, Remote string
}

type syncOptions struct {
	ID, Repo, Session, Client, Message, Verifiers string
	Paths                                         []string
	Complete                                      bool
}

type verifyOptions struct {
	ID, Repo, Session, Client, Message, Verifiers string
	Paths                                         []string
}

func validateSyncArgs(args []string) error {
	_, err := parseSyncArgs(args)
	return err
}

func parseSyncArgs(args []string) (syncOptions, error) {
	var options syncOptions
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name == "--complete" {
			if seen[name] {
				return syncOptions{}, cliError{"E_USAGE", "--complete may be provided once"}
			}
			options.Complete = true
			seen[name] = true
			continue
		}
		if name != "--id" && name != "--repo" && name != "--session" && name != "--client" && name != "--message" && name != "--verifiers" && name != "--path" {
			return syncOptions{}, cliError{"E_USAGE", "sync supports baseline fields plus --complete, --id, --message, and --verifiers"}
		}
		if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return syncOptions{}, cliError{"E_USAGE", name + " requires a value"}
		}
		if name != "--path" && seen[name] {
			return syncOptions{}, cliError{"E_USAGE", name + " may be provided once"}
		}
		switch name {
		case "--id":
			options.ID = args[i+1]
		case "--repo":
			options.Repo = args[i+1]
		case "--session":
			options.Session = args[i+1]
		case "--client":
			options.Client = args[i+1]
		case "--message":
			options.Message = args[i+1]
		case "--verifiers":
			options.Verifiers = args[i+1]
		case "--path":
			options.Paths = append(options.Paths, args[i+1])
		}
		seen[name] = true
		i++
	}
	if options.Repo == "" || options.Session == "" || options.Client == "" || len(options.Paths) == 0 {
		return syncOptions{}, cliError{"E_SCOPE", "--repo, --session, --client, and at least one --path are required for sync"}
	}
	if !options.Complete && (options.ID != "" || options.Message != "" || options.Verifiers != "") {
		return syncOptions{}, cliError{"E_USAGE", "--id, --message, and --verifiers require --complete"}
	}
	if options.Complete && (options.ID == "" || options.Message == "" || options.Verifiers == "") {
		return syncOptions{}, cliError{"E_SCOPE", "--id, --message, and --verifiers are required with --complete"}
	}
	return options, nil
}

func runSync(args []string, dir string, out io.Writer) error {
	options, err := parseSyncArgs(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	key, err := loadIdentityKey(dir)
	if err != nil {
		return err
	}
	info, err := repository.DiscoverWithKey(options.Repo, key)
	if err != nil {
		return cliError{"E_SCOPE", err.Error()}
	}
	db, err := state.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if options.Complete {
		return runSyncComplete(context.Background(), options, dir, info, db, out)
	}
	baseline, err := session.New(db).CaptureAndRecord(context.Background(), session.Request{SessionID: options.Session, RepositoryID: info.RepoID, ClientID: options.Client, Root: info.Root, Paths: options.Paths})
	if err != nil {
		return cliError{"E_REPOSITORY", safeMessage(err.Error())}
	}
	result := baseline.EventPayload()
	result["schema_version"] = "autogit.result/1"
	result["disposition"] = "accepted"
	result["action"] = "checkpoint"
	result["reason_code"] = "SYNC_BASELINE_CAPTURED"
	result["repo_id"] = info.RepoID
	result["session_id"] = options.Session
	return json.NewEncoder(out).Encode(result)
}

func runSyncComplete(ctx context.Context, options syncOptions, dir string, info repository.Info, db *state.Store, out io.Writer) error {
	durable, err := db.Session(ctx, options.Session)
	if errors.Is(err, sql.ErrNoRows) {
		return cliError{"E_NOT_FOUND", "sync session was not found"}
	}
	if err != nil {
		return cliError{"E_STATE", "cannot read sync session"}
	}
	if durable.RepositoryID != info.RepoID || durable.ClientID != options.Client {
		return cliError{"E_SCOPE", "sync session does not match repository or client"}
	}
	registry, err := verification.LoadTrustedRegistryFile(options.Verifiers, dir, 0)
	if err != nil {
		return cliError{"E_VERIFIER_CONFIG", safeMessage(err.Error())}
	}
	service := session.New(db)
	started, err := service.ResumeFromDurable(ctx, session.Request{SessionID: options.Session, RepositoryID: info.RepoID, ClientID: options.Client, Root: info.Root, Paths: options.Paths}, session.DurableBaseline{
		Head: durable.BaselineHead, IndexDigest: durable.BaselineIndex, StatusDigest: durable.StatusDigest, PathsDigest: durable.BaselinePathsDigest,
	})
	if err != nil {
		return cliError{"E_REPOSITORY", safeMessage(err.Error())}
	}
	plan, err := service.BuildOwnedPlanAtCurrent(ctx, started.Request, started.Baseline)
	if err != nil {
		return cliError{"E_SCOPE", safeMessage(err.Error())}
	}
	workflowService := localworkflow.Service{Git: gittransaction.SystemRunner{}, Intents: gittransaction.NewStateIntentPort(db), VerifierRunner: verification.ExecRunner{}, Lease: coordinator.StateLease{DB: db}}
	result, err := workflowService.RunPlan(ctx, localworkflow.Request{ID: options.ID, RepositoryDir: info.Root, Message: options.Message, Policy: loadPolicy(dir, info.RepoID), Verifiers: registry}, plan)
	if err != nil {
		return cliError{"E_COMMIT", safeMessage(err.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"schema_version": "autogit.result/1", "disposition": "accepted", "action": "commit", "reason_code": "SYNC_COMMITTED",
		"session_id": options.Session, "commit_sha": result.Commit.SHA, "ref": result.Commit.Ref, "ownership_digest": result.OwnershipDigest,
		"verification": map[string]any{"verifier_set_digest": result.Verification.VerifierSetDigest, "evidence_digest": result.Verification.EvidenceDigest},
	})
}

func validateVerifyArgs(args []string) error {
	_, err := parseVerifyArgs(args)
	return err
}

func parseVerifyArgs(args []string) (verifyOptions, error) {
	var options verifyOptions
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name != "--id" && name != "--repo" && name != "--session" && name != "--client" && name != "--message" && name != "--verifiers" && name != "--path" {
			return verifyOptions{}, cliError{"E_USAGE", "verify supports --id, --repo, --session, --client, --message, --verifiers, and repeated --path"}
		}
		if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return verifyOptions{}, cliError{"E_USAGE", name + " requires a value"}
		}
		if name != "--path" && seen[name] {
			return verifyOptions{}, cliError{"E_USAGE", name + " may be provided once"}
		}
		switch name {
		case "--id":
			options.ID = args[i+1]
		case "--repo":
			options.Repo = args[i+1]
		case "--session":
			options.Session = args[i+1]
		case "--client":
			options.Client = args[i+1]
		case "--message":
			options.Message = args[i+1]
		case "--verifiers":
			options.Verifiers = args[i+1]
		case "--path":
			options.Paths = append(options.Paths, args[i+1])
		}
		seen[name] = true
		i++
	}
	if options.ID == "" || options.Repo == "" || options.Session == "" || options.Client == "" || options.Message == "" || options.Verifiers == "" || len(options.Paths) == 0 {
		return verifyOptions{}, cliError{"E_SCOPE", "--id, --repo, --session, --client, --message, --verifiers, and at least one --path are required for verify"}
	}
	return options, nil
}

func runVerify(args []string, dir string, out io.Writer) error {
	options, err := parseVerifyArgs(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	key, err := loadIdentityKey(dir)
	if err != nil {
		return err
	}
	info, err := repository.DiscoverWithKey(options.Repo, key)
	if err != nil {
		return cliError{"E_SCOPE", err.Error()}
	}
	db, err := state.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	durable, err := db.Session(context.Background(), options.Session)
	if errors.Is(err, sql.ErrNoRows) {
		return cliError{"E_NOT_FOUND", "verify session was not found"}
	}
	if err != nil {
		return cliError{"E_STATE", "cannot read verify session"}
	}
	if durable.RepositoryID != info.RepoID || durable.ClientID != options.Client {
		return cliError{"E_SCOPE", "verify session does not match repository or client"}
	}
	registry, err := verification.LoadTrustedRegistryFile(options.Verifiers, dir, 0)
	if err != nil {
		return cliError{"E_VERIFIER_CONFIG", safeMessage(err.Error())}
	}
	service := session.New(db)
	started, err := service.ResumeFromDurable(context.Background(), session.Request{SessionID: options.Session, RepositoryID: info.RepoID, ClientID: options.Client, Root: info.Root, Paths: options.Paths}, session.DurableBaseline{
		Head: durable.BaselineHead, IndexDigest: durable.BaselineIndex, StatusDigest: durable.StatusDigest, PathsDigest: durable.BaselinePathsDigest,
	})
	if err != nil {
		return cliError{"E_REPOSITORY", safeMessage(err.Error())}
	}
	plan, err := service.BuildOwnedPlanAtCurrent(context.Background(), started.Request, started.Baseline)
	if err != nil {
		return cliError{"E_SCOPE", safeMessage(err.Error())}
	}
	workflowService := localworkflow.Service{Git: gittransaction.SystemRunner{}, Intents: gittransaction.NewStateIntentPort(db), VerifierRunner: verification.ExecRunner{}}
	result, err := workflowService.VerifyPlan(context.Background(), localworkflow.Request{ID: options.ID, RepositoryDir: info.Root, Message: options.Message, Policy: loadPolicy(dir, info.RepoID), Verifiers: registry}, plan)
	if err != nil {
		return cliError{"E_VERIFY", safeMessage(err.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"schema_version": "autogit.result/1", "disposition": "accepted", "action": "verify", "reason_code": "VERIFICATION_PASSED",
		"session_id": options.Session, "ownership_digest": result.OwnershipDigest,
		"verification": map[string]any{"decision": result.Verification.Decision, "reason": result.Verification.Reason, "verifier_set_digest": result.Verification.VerifierSetDigest, "evidence_digest": result.Verification.EvidenceDigest},
	})
}

func validateRetryArgs(args []string) error {
	_, err := parseRetryArgs(args)
	return err
}

func parseRetryArgs(args []string) (retryOptions, error) {
	var options retryOptions
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name != "--id" && name != "--repo" && name != "--remote" {
			return retryOptions{}, cliError{"E_USAGE", "retry supports --id, --repo, and --remote"}
		}
		if seen[name] || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return retryOptions{}, cliError{"E_USAGE", name + " requires one value and may be provided once"}
		}
		seen[name] = true
		switch name {
		case "--id":
			options.ID = args[i+1]
		case "--repo":
			options.Repo = args[i+1]
		case "--remote":
			options.Remote = args[i+1]
		}
		i++
	}
	if options.ID == "" {
		return retryOptions{}, cliError{"E_SCOPE", "--id is required for retry"}
	}
	if options.Repo == "" || options.Remote == "" {
		return retryOptions{}, cliError{"E_SCOPE", "--repo and --remote are required for retry"}
	}
	return options, nil
}

func runRetry(args []string, dir string, out io.Writer) error {
	options, err := parseRetryArgs(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	key, err := loadIdentityKey(dir)
	if err != nil {
		return err
	}
	info, err := repository.DiscoverWithKey(options.Repo, key)
	if err != nil {
		return cliError{"E_SCOPE", err.Error()}
	}
	db, err := state.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	job, err := db.PushJob(options.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return cliError{"E_NOT_FOUND", "retry job was not found"}
	}
	if err != nil {
		return cliError{"E_STATE", "cannot read retry job"}
	}
	if job.State != state.PushRetryWait {
		return cliError{"E_STATE", "retry job is not waiting for retry"}
	}
	ghPath, err := trustedExecutable("gh")
	if err != nil {
		return cliError{"E_PROVIDER", "gh is unavailable"}
	}
	gitPath, err := trustedExecutable("git")
	if err != nil {
		return cliError{"E_PROVIDER", "git is unavailable"}
	}
	ghRunner := provider.SystemRunner{Executable: ghPath, WorkingDir: info.Root}
	gitRunner := provider.SystemRunner{Executable: gitPath, WorkingDir: info.Root}
	pusher := provider.GitPusher{Runner: gitRunner, Dir: info.Root, AllowedRemotes: map[string]string{job.Owner + "/" + job.Name: options.Remote}}
	publication := provider.GH{Runner: ghRunner, Pusher: pusher}
	coord := retryCoordinator(db, publication, options.ID)
	if err := coord.RetryPush(context.Background(), options.ID); err != nil {
		status, _, statusErr := coord.Store.PushStatus(context.Background(), options.ID)
		if statusErr == nil && status == state.PushRetryWait {
			return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "pending", "action": "retry", "reason_code": "PUSH_RETRY_WAIT", "retryable": true, "job_id": options.ID})
		}
		return cliError{"E_PUSH", safeMessage(err.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "PUSH_RETRIED", "job_id": options.ID})
}

func retryCoordinator(db *state.Store, publication provider.PublicationProvider, owner string) coordinator.Coordinator {
	return coordinator.Coordinator{
		Store:    coordinator.NewStateStore(db),
		Provider: coordinator.PublicationProviderAdapter{Provider: publication},
		Lease:    coordinator.StateLease{DB: db},
		Owner:    owner,
	}
}

func trustedExecutable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
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
			if worktreeID, ok := e.Scope["worktree_id"].(string); ok && worktreeID != "" && worktreeID != info.WorktreeID {
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
	// Event receipts/projections and repository session evidence have separate
	// package-owned ports, even though they share the same private SQLite file.
	// The baseline service never writes raw source bytes to this database.
	baselineStore, err := state.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer baselineStore.Close()
	a.Baselines = &session.Service{Runner: repository.SystemRunner{}, Store: baselineStore}
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
