package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"autogit/internal/adapters"
	"autogit/internal/app"
	"autogit/internal/coordinator"
	"autogit/internal/events"
	"autogit/internal/gitport"
	"autogit/internal/gittransaction"
	"autogit/internal/historyscan"
	"autogit/internal/install"
	"autogit/internal/lifecycle"
	"autogit/internal/policy"
	"autogit/internal/provider"
	"autogit/internal/publication"
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
		_, _ = io.WriteString(out, "autogit commands: install doctor enable disable init status plan hook verify sync publish remote retry logs uninstall config explain\n")
		return nil
	}
	if args[0] == "hook" {
		return runHook(args[1:], in, out)
	}
	cmd := args[0]
	if cmd == "install" && hasFlag(args[1:], "--list") {
		if len(args) != 2 || args[1] != "--list" {
			return cliError{"E_USAGE", "install --list cannot be combined with other arguments"}
		}
		return runInstallList(out)
	}
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
	if cmd == "init" {
		options, parseErr := parseInitArgs(args[1:])
		if parseErr != nil {
			return parseErr
		}
		return runInit(options, dir, out)
	}
	if cmd == "plan" {
		return runPlan(args[1:], dir, out)
	}
	if cmd == "config" {
		return runConfig(args[1:], dir, out)
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
	if cmd == "publish" {
		return runPublish(args[1:], dir, out)
	}
	if cmd == "remote" {
		return runRemote(args[1:], dir, out)
	}
	if cmd == "doctor" {
		if len(args) != 1 {
			return cliError{"E_USAGE", "doctor does not accept arguments"}
		}
		return runDoctor(dir, out)
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
		if err := validateEnableArgs(args[1:], cmd == "enable"); err != nil {
			return err
		}
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
			p = enabledPolicy(args[1:], p.Version+1)
		}
		if err = savePolicy(dir, info.RepoID, p); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "POLICY_UPDATED", "repo_id": info.RepoID})
	case "status":
		verifierPath := flag(args[1:], "--verifiers")
		if verifierPath != "" {
			return cliError{"E_USAGE", "--verifiers is supported by config explain"}
		}
		root := flag(args[1:], "--repo")
		var repoID string
		var info repository.Info
		if root != "" {
			var discoverErr error
			info, discoverErr = repository.DiscoverWithKey(root, identityKey)
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
		if cmd == "status" {
			projection, projectionErr := lifecycleStatus(s, repoID)
			if projectionErr != nil {
				return projectionErr
			}
			result["lifecycle"] = projection
			summary, summaryErr := captureRepositorySummary(context.Background(), info.Root)
			if summaryErr != nil {
				return cliError{"E_REPOSITORY", safeMessage(summaryErr.Error())}
			}
			result["repository"] = summary
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

func runPlan(args []string, dir string, out io.Writer) error {
	root := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--repo" {
			return cliError{"E_USAGE", "plan supports only --repo"}
		}
		if root != "" || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return cliError{"E_USAGE", "--repo requires one value"}
		}
		root = args[i+1]
		i++
	}
	if root == "" {
		return cliError{"E_SCOPE", "--repo is required for read-only inspection"}
	}
	key, _, err := identityKeyForRead(dir)
	if err != nil {
		return err
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil {
		return cliError{"E_SCOPE", err.Error()}
	}
	p := loadPolicy(dir, info.RepoID)
	summary, err := captureRepositorySummary(context.Background(), info.Root)
	if err != nil {
		return cliError{"E_REPOSITORY", safeMessage(err.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "READ_ONLY_PLAN", "repo_id": info.RepoID,
		"repository": summary,
		"policy":     p,
		"checks": map[string]any{
			"tracking_consent": p.TrackingEnabled(), "local_only": p.LocalOnly,
			"provider_allowed": p.ProviderAllowed(), "public_consent": p.PublicConsent,
		},
	})
}

func captureRepositorySummary(ctx context.Context, root string) (map[string]any, error) {
	baseline, err := repository.CaptureBaseline(ctx, repository.SystemRunner{}, root)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"head": baseline.Head, "index_digest": baseline.IndexDigest, "status_digest": baseline.StatusDigest,
		"paths_digest": baseline.PathsDigest, "changed_path_count": len(baseline.Paths),
	}, nil
}

func runConfig(args []string, _ string, out io.Writer) error {
	if len(args) == 0 || args[0] != "explain" {
		return cliError{"E_USAGE", "config supports explain"}
	}
	verifierPath := ""
	for i := 1; i < len(args); i++ {
		if args[i] != "--verifiers" || verifierPath != "" || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return cliError{"E_USAGE", "config explain supports one --verifiers path"}
		}
		verifierPath = args[i+1]
		i++
	}
	result := map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "CONFIG_EXPLAIN", "policy": policy.Policy{}}
	if verifierPath != "" {
		registry, err := verification.LoadRegistryFile(verifierPath, 1<<20)
		if err != nil {
			return cliError{"E_VERIFIER_CONFIG", safeMessage(err.Error())}
		}
		result["verifier_count"] = len(registry.Specs)
		result["verifier_set_digest"] = registry.VerifierSetDigest
		result["verifier_config_digest"] = registry.ConfigDigest
	}
	return json.NewEncoder(out).Encode(result)
}

func runDoctor(dir string, out io.Writer) error {
	_, gitErr := trustedExecutable("git")
	_, ghErr := trustedExecutable("gh")
	installations := install.ClientInstallations()
	installable := 0
	for _, entry := range installations {
		if entry.Supported {
			installable++
		}
	}
	stateDatabase, lockStore := inspectDoctorState(dir)
	result := map[string]any{
		"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "DOCTOR_OK",
		"git_available": gitErr == nil, "gh_available": ghErr == nil, "provider_auth": "not_checked",
		"state_dir": "configured", "state_database": stateDatabase, "lock_store": lockStore,
		"adapter_count": len(installations), "installable_adapter_count": installable,
	}
	if gitErr != nil {
		result["reason_code"] = "GIT_UNAVAILABLE"
	}
	return json.NewEncoder(out).Encode(result)
}

func inspectDoctorState(dir string) (string, string) {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "not_initialized", "not_initialized"
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return "unavailable", "unavailable"
	}
	dbInfo, err := os.Lstat(filepath.Join(dir, "state.db"))
	if errors.Is(err, os.ErrNotExist) {
		return "not_initialized", "not_initialized"
	}
	if err != nil || dbInfo.Mode()&os.ModeSymlink != 0 || !dbInfo.Mode().IsRegular() || (runtime.GOOS != "windows" && dbInfo.Mode().Perm()&0077 != 0) {
		return "unavailable", "unavailable"
	}
	return "available", "available"
}

func runInstallList(out io.Writer) error {
	type discovered struct {
		Adapter      string                      `json:"adapter"`
		Installable  bool                        `json:"installable"`
		Reason       string                      `json:"reason,omitempty"`
		Capabilities adapters.CapabilityManifest `json:"capabilities"`
	}
	entries := install.ClientInstallations()
	result := make([]discovered, 0, len(entries))
	for _, entry := range entries {
		manifest, err := adapters.ManifestFor(entry.Adapter)
		if err != nil {
			return cliError{"E_ADAPTER", safeMessage(err.Error())}
		}
		result = append(result, discovered{Adapter: entry.Adapter, Installable: entry.Supported, Reason: entry.UnsupportedReason, Capabilities: manifest})
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "ADAPTER_DISCOVERY", "adapters": result})
}

type retryOptions struct {
	ID, Repo, Remote string
}

type syncOptions struct {
	ID, Repo, Session, Client, Message, Verifiers string
	Paths                                         []string
	Complete, AllOwned                            bool
}

type verifyOptions struct {
	ID, Repo, Session, Client, Message, Verifiers string
	Paths                                         []string
	AllOwned                                      bool
}

type publishOptions struct {
	ID, Repo, Remote, Owner, Name, Ref, Visibility, Mode, License string
	Verifiers, Tests, CI                                          string
	PublicConsent, ConfirmDestination, FeatureBranchApproved      bool
	ProtectedBranch, StatusChecksRequired, StatusChecksPassed     bool
}

type remoteOptions struct {
	ID, Repo, Alias, Owner, Name, Visibility string
	PublicConsent                            bool
}

type initOptions struct {
	Repo, Branch, Provider, Owner, Name, Visibility string
	Local, PublicConsent, DryRun                    bool
}

func validateEnableArgs(args []string, enabling bool) error {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name == "--local" || name == "--public-consent" {
			if !enabling {
				return cliError{"E_USAGE", name + " is not valid with disable"}
			}
			if seen[name] {
				return cliError{"E_USAGE", name + " may be provided once"}
			}
			seen[name] = true
			continue
		}
		switch name {
		case "--repo", "--provider", "--owner", "--destination", "--visibility":
		default:
			return cliError{"E_USAGE", "unknown enable argument"}
		}
		if seen[name] || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return cliError{"E_USAGE", name + " requires one value and may be provided once"}
		}
		seen[name] = true
		i++
	}
	if flag(args, "--repo") == "" {
		return cliError{"E_SCOPE", "--repo is required"}
	}
	if !enabling {
		for _, name := range []string{"--provider", "--owner", "--destination", "--visibility"} {
			if seen[name] {
				return cliError{"E_USAGE", name + " is not valid with disable"}
			}
		}
		return nil
	}
	remote := flag(args, "--provider") != "" || flag(args, "--owner") != "" || flag(args, "--destination") != "" || flag(args, "--visibility") != ""
	if seen["--local"] && remote {
		return cliError{"E_USAGE", "--local cannot be combined with remote policy fields"}
	}
	if remote && (flag(args, "--provider") == "" || flag(args, "--owner") == "" || flag(args, "--destination") == "") {
		return cliError{"E_SCOPE", "--provider, --owner, and --destination are required for remote tracking"}
	}
	if visibility := flag(args, "--visibility"); visibility != "" && visibility != "private" && visibility != "public" {
		return cliError{"E_USAGE", "--visibility must be private or public"}
	}
	if flag(args, "--visibility") == "public" && !seen["--public-consent"] {
		return cliError{"E_CONSENT", "--public-consent is required for public tracking"}
	}
	if flag(args, "--provider") != "" && flag(args, "--provider") != "github" {
		return cliError{"E_PROVIDER", "only github is supported"}
	}
	return nil
}

func enabledPolicy(args []string, version int) policy.Policy {
	remote := flag(args, "--provider") != "" || flag(args, "--owner") != "" || flag(args, "--destination") != "" || flag(args, "--visibility") != ""
	if !remote || hasFlag(args, "--local") {
		return policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe", Version: version}
	}
	visibility := flag(args, "--visibility")
	if visibility == "" {
		visibility = "private"
	}
	return policy.Policy{
		Tracking: "yes", Visibility: visibility, Provider: flag(args, "--provider"),
		Owner: flag(args, "--owner"), Destination: flag(args, "--destination"),
		Workflow: "safe", PublicConsent: hasFlag(args, "--public-consent"), Version: version,
	}
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func validatePublishArgs(args []string) error {
	_, err := parsePublishArgs(args)
	return err
}

func parsePublishArgs(args []string) (publishOptions, error) {
	var options publishOptions
	options.Mode = "private"
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name == "--public-consent" || name == "--confirm-destination" || name == "--feature-branch-approved" || name == "--protected-branch" || name == "--status-checks-required" || name == "--status-checks-passed" {
			if seen[name] {
				return publishOptions{}, cliError{"E_USAGE", name + " may be provided once"}
			}
			seen[name] = true
			switch name {
			case "--public-consent":
				options.PublicConsent = true
			case "--confirm-destination":
				options.ConfirmDestination = true
			case "--feature-branch-approved":
				options.FeatureBranchApproved = true
			case "--protected-branch":
				options.ProtectedBranch = true
			case "--status-checks-required":
				options.StatusChecksRequired = true
			case "--status-checks-passed":
				options.StatusChecksPassed = true
			}
			continue
		}
		switch name {
		case "--id", "--repo", "--remote", "--owner", "--name", "--ref", "--visibility", "--mode", "--license", "--verifiers", "--tests", "--ci":
		default:
			return publishOptions{}, cliError{"E_USAGE", "publish supports explicit destination, --license, --verifiers, readiness evidence, and consent flags"}
		}
		if seen[name] || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return publishOptions{}, cliError{"E_USAGE", name + " requires one value and may be provided once"}
		}
		seen[name] = true
		switch name {
		case "--id":
			options.ID = args[i+1]
		case "--repo":
			options.Repo = args[i+1]
		case "--remote":
			options.Remote = args[i+1]
		case "--owner":
			options.Owner = args[i+1]
		case "--name":
			options.Name = args[i+1]
		case "--ref":
			options.Ref = args[i+1]
		case "--visibility":
			options.Visibility = args[i+1]
		case "--mode":
			options.Mode = args[i+1]
		case "--license":
			options.License = args[i+1]
		case "--verifiers":
			options.Verifiers = args[i+1]
		case "--tests":
			options.Tests = args[i+1]
		case "--ci":
			options.CI = args[i+1]
		}
		i++
	}
	if options.ID == "" || options.Repo == "" || options.Remote == "" || options.Owner == "" || options.Name == "" || options.Ref == "" {
		return publishOptions{}, cliError{"E_SCOPE", "--id, --repo, --remote, --owner, --name, and --ref are required for publish"}
	}
	if options.Visibility == "" {
		options.Visibility = "private"
	}
	if options.Visibility != "private" && options.Visibility != "public" {
		return publishOptions{}, cliError{"E_USAGE", "--visibility must be private or public"}
	}
	if options.Mode != "private" && options.Mode != "public" {
		return publishOptions{}, cliError{"E_USAGE", "--mode must be private or public"}
	}
	if options.Mode != options.Visibility {
		return publishOptions{}, cliError{"E_SCOPE", "--mode and --visibility must match"}
	}
	if options.Mode == "public" && !options.PublicConsent {
		return publishOptions{}, cliError{"E_CONSENT", "--public-consent is required for public publish"}
	}
	if options.Tests != "" && options.Tests != publication.StatusAbsent && options.Tests != publication.StatusPassed && options.Tests != publication.StatusFailed && options.Tests != publication.StatusUnknown {
		return publishOptions{}, cliError{"E_USAGE", "--tests must be absent, passed, failed, or unknown"}
	}
	if options.CI != "" && options.CI != publication.StatusAbsent && options.CI != publication.StatusPresent && options.CI != publication.StatusPassed && options.CI != publication.StatusFailed && options.CI != publication.StatusUnknown {
		return publishOptions{}, cliError{"E_USAGE", "--ci must be absent, present, passed, failed, or unknown"}
	}
	return options, nil
}

func parseRemoteArgs(args []string) (remoteOptions, error) {
	var options remoteOptions
	if len(args) == 0 || args[0] != "create" {
		return remoteOptions{}, cliError{"E_USAGE", "remote supports only remote create"}
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		name := args[i]
		if name == "--public-consent" {
			if seen[name] {
				return remoteOptions{}, cliError{"E_USAGE", name + " may be provided once"}
			}
			seen[name] = true
			options.PublicConsent = true
			continue
		}
		switch name {
		case "--id", "--repo", "--alias", "--owner", "--name", "--visibility":
		default:
			return remoteOptions{}, cliError{"E_USAGE", "remote create supports --id, --repo, --alias, --owner, --name, --visibility, and --public-consent"}
		}
		if seen[name] || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return remoteOptions{}, cliError{"E_USAGE", name + " requires one value and may be provided once"}
		}
		seen[name] = true
		switch name {
		case "--id":
			options.ID = args[i+1]
		case "--repo":
			options.Repo = args[i+1]
		case "--alias":
			options.Alias = args[i+1]
		case "--owner":
			options.Owner = args[i+1]
		case "--name":
			options.Name = args[i+1]
		case "--visibility":
			options.Visibility = args[i+1]
		}
		i++
	}
	if options.ID == "" || options.Repo == "" || options.Alias == "" || options.Owner == "" || options.Name == "" {
		return remoteOptions{}, cliError{"E_SCOPE", "remote create requires --id, --repo, --alias, --owner, and --name"}
	}
	if options.Visibility == "" {
		options.Visibility = "private"
	}
	if options.Visibility != "private" && options.Visibility != "public" {
		return remoteOptions{}, cliError{"E_USAGE", "--visibility must be private or public"}
	}
	if options.Visibility == "public" && !options.PublicConsent {
		return remoteOptions{}, cliError{"E_CONSENT", "--public-consent is required for public remote creation"}
	}
	return options, nil
}

func parseInitArgs(args []string) (initOptions, error) {
	options := initOptions{Branch: "main", Visibility: "private"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name == "--local" || name == "--public-consent" || name == "--dry-run" {
			if seen[name] {
				return initOptions{}, cliError{"E_USAGE", name + " may be provided once"}
			}
			seen[name] = true
			if name == "--local" {
				options.Local = true
			} else if name == "--dry-run" {
				options.DryRun = true
			} else {
				options.PublicConsent = true
			}
			continue
		}
		switch name {
		case "--repo", "--branch", "--provider", "--owner", "--name", "--visibility":
		default:
			return initOptions{}, cliError{"E_USAGE", "init supports --repo, --branch, --local, --provider, --owner, --name, --visibility, --public-consent, and --dry-run"}
		}
		if seen[name] || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return initOptions{}, cliError{"E_USAGE", name + " requires one value and may be provided once"}
		}
		seen[name] = true
		switch name {
		case "--repo":
			options.Repo = args[i+1]
		case "--branch":
			options.Branch = args[i+1]
		case "--provider":
			options.Provider = args[i+1]
		case "--owner":
			options.Owner = args[i+1]
		case "--name":
			options.Name = args[i+1]
		case "--visibility":
			options.Visibility = args[i+1]
		}
		i++
	}
	if options.Repo == "" {
		return initOptions{}, cliError{"E_SCOPE", "--repo is required"}
	}
	remote := options.Provider != "" || options.Owner != "" || options.Name != ""
	if options.Local && remote {
		return initOptions{}, cliError{"E_USAGE", "--local cannot be combined with remote tracking fields"}
	}
	if !options.Local && (!remote || options.Provider == "" || options.Owner == "" || options.Name == "") {
		return initOptions{}, cliError{"E_CONSENT", "init requires --local or complete remote tracking consent"}
	}
	if options.Provider != "" && options.Provider != "github" {
		return initOptions{}, cliError{"E_PROVIDER", "only github is supported"}
	}
	if options.Visibility != "private" && options.Visibility != "public" {
		return initOptions{}, cliError{"E_USAGE", "--visibility must be private or public"}
	}
	if options.Visibility == "public" && !options.PublicConsent {
		return initOptions{}, cliError{"E_CONSENT", "--public-consent is required for public initialization"}
	}
	if err := repository.ValidateInitialBranch(options.Branch); err != nil {
		return initOptions{}, cliError{"E_USAGE", err.Error()}
	}
	if !options.Local {
		if err := provider.ValidateRemoteRequest(provider.RemoteRequest{Owner: options.Owner, Name: options.Name, Visibility: options.Visibility}); err != nil {
			return initOptions{}, cliError{"E_USAGE", err.Error()}
		}
	}
	return options, nil
}

func runInit(options initOptions, dir string, out io.Writer) error {
	preview, err := repository.PlanInitialization(options.Repo, options.Branch)
	if err != nil {
		return cliError{"E_SCOPE", err.Error()}
	}
	if options.DryRun {
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": "REPOSITORY_INIT_PLAN", "root": preview.Root, "branch": preview.Branch, "tracking": initTrackingMode(options), "visibility": options.Visibility, "hygiene": preview.Hygiene})
	}
	root := preview.Root
	gitPath, err := trustedExecutable("git")
	if err != nil {
		return cliError{"E_PROVIDER", "git is unavailable"}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	key, err := loadIdentityKey(dir)
	if err != nil {
		return err
	}
	repoID, err := repository.FutureRepositoryID(root, key)
	if err != nil {
		return cliError{"E_SCOPE", err.Error()}
	}
	p := initPolicy(options)
	// Persist consent before Git initialization so a crash cannot leave a new
	// repository without the decision that authorized the mutation.
	if err := savePolicy(dir, repoID, p); err != nil {
		return err
	}
	if _, err := repository.Initialize(context.Background(), gitport.Runner{Executable: gitPath}, root, options.Branch); err != nil {
		return cliError{"E_GIT", safeMessage(err.Error())}
	}
	info, err := repository.DiscoverWithKey(root, key)
	if err != nil || info.RepoID != repoID {
		return cliError{"E_STATE", "initialized repository identity could not be confirmed"}
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "notify", "reason_code": "REPOSITORY_INITIALIZED", "repo_id": info.RepoID, "branch": options.Branch, "tracking": p.Tracking, "visibility": p.Visibility})
}

func initTrackingMode(options initOptions) string {
	if options.Local {
		return "local"
	}
	return "yes"
}

func initPolicy(options initOptions) policy.Policy {
	if options.Local {
		return policy.Policy{Tracking: "local", LocalOnly: true, Visibility: "private", Workflow: "safe", Version: 1}
	}
	return policy.Policy{Tracking: "yes", Visibility: options.Visibility, Provider: options.Provider, Owner: options.Owner, Destination: options.Owner + "/" + options.Name, Workflow: "safe", PublicConsent: options.PublicConsent, Version: 1}
}

func runRemote(args []string, dir string, out io.Writer) error {
	options, err := parseRemoteArgs(args)
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
	p := loadPolicy(dir, info.RepoID)
	if !p.TrackingEnabled() {
		return cliError{"E_CONSENT", "tracking consent is required before repository creation"}
	}
	if p.LocalOnly {
		return cliError{"E_LOCAL_ONLY", "repository creation is disabled by local-only policy"}
	}
	if p.Provider != "github" {
		return cliError{"E_PROVIDER", "unsupported repository provider"}
	}
	if p.Owner != "" && p.Owner != options.Owner || p.Destination != "" && p.Destination != options.Owner+"/"+options.Name || p.Visibility != "" && p.Visibility != options.Visibility {
		return cliError{"E_SCOPE", "remote identity does not match approved policy"}
	}
	if options.Visibility == "public" && (!p.CanPublishPublic() || !options.PublicConsent) {
		return cliError{"E_CONSENT", "public repository creation requires separate policy and command consent"}
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
	tx := provider.RepositoryTransaction{
		State:  db,
		Hosted: provider.GH{Runner: ghRunner, VerifyOwner: true},
		Git:    provider.GitPusher{Runner: gitRunner, Dir: info.Root},
	}
	identity, err := tx.Create(context.Background(), provider.RemoteCreateRequest{ID: options.ID, RepositoryID: info.RepoID, Alias: options.Alias, Owner: options.Owner, Name: options.Name, Visibility: options.Visibility})
	if err != nil {
		return cliError{"E_PROVIDER", safeMessage(err.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "notify", "reason_code": "REMOTE_ATTACHED", "remote": identity, "alias": options.Alias, "visibility": options.Visibility})
}

func runPublish(args []string, dir string, out io.Writer) error {
	options, err := parsePublishArgs(args)
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
	intent, err := db.GitCommitIntentRecord(context.Background(), options.ID)
	if errors.Is(err, os.ErrNotExist) {
		return cliError{"E_NOT_FOUND", "commit intent was not found"}
	}
	if err != nil {
		return cliError{"E_STATE", "cannot read commit intent"}
	}
	if intent.Intent.RepoDir != info.Root || intent.State != state.CommitCreated || intent.SHA == "" {
		return cliError{"E_STATE", "commit intent is not a completed local commit for this repository"}
	}
	if err := verifyStoredCommit(context.Background(), info.Root, intent); err != nil {
		return cliError{"E_STATE", safeMessage(err.Error())}
	}
	p := loadPolicy(dir, info.RepoID)
	if !p.TrackingEnabled() {
		return cliError{"E_CONSENT", "tracking consent is required before publication"}
	}
	if p.LocalOnly {
		return cliError{"E_LOCAL_ONLY", "publication is disabled by local-only policy"}
	}
	if p.Provider != "" && p.Provider != "github" {
		return cliError{"E_PROVIDER", "unsupported publication provider"}
	}
	if p.Visibility != "" && p.Visibility != options.Visibility {
		return cliError{"E_SCOPE", "publish visibility does not match approved policy"}
	}
	if p.Owner != "" && p.Owner != options.Owner {
		return cliError{"E_SCOPE", "publish owner does not match approved policy"}
	}
	if p.Destination != "" && p.Destination != options.Owner+"/"+options.Name {
		return cliError{"E_SCOPE", "publish destination does not match approved policy"}
	}
	if options.Mode == "public" && (!p.CanPublishPublic() || !options.PublicConsent) {
		return cliError{"E_CONSENT", "public publication requires separate policy and command consent"}
	}
	if options.Mode == "public" {
		report := buildPublicPreflight(context.Background(), dir, info.Root, intent, options, p)
		if !report.CanPublishPublic() {
			return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "rejected", "action": "blocked", "reason_code": "PUBLIC_PREFLIGHT_REQUIRED", "job_id": options.ID, "preflight": report})
		}
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
	publication := provider.GH{Runner: ghRunner, Pusher: provider.GitPusher{Runner: gitRunner, Dir: info.Root, AllowedRemotes: map[string]string{options.Owner + "/" + options.Name: options.Remote}}}
	if options.Mode == "public" {
		if err := publication.ConfirmRepository(context.Background(), provider.RemoteRequest{Owner: options.Owner, Name: options.Name, Visibility: options.Visibility}); err != nil {
			return cliError{"E_PROVIDER", safeMessage(err.Error())}
		}
	}
	coord := retryCoordinator(db, publication, options.ID)
	remoteDigest := digestDestination(options.Owner, options.Name, options.Visibility, options.Ref)
	publishErr := coord.Push(context.Background(), coordinator.PushRequest{ID: options.ID, Owner: options.Owner, Name: options.Name, Ref: options.Ref, CommitSHA: intent.SHA, RemoteDigest: remoteDigest})
	if job, jobErr := db.PushJob(options.ID); jobErr == nil {
		if factErr := emitPublishDomainFacts(context.Background(), filepath.Join(dir, "state.db"), p, info, job, publishErr); factErr != nil {
			return cliError{"E_STATE", safeMessage(factErr.Error())}
		}
	}
	if publishErr != nil {
		status, _, statusErr := coord.Store.PushStatus(context.Background(), options.ID)
		if statusErr == nil && status == state.PushRetryWait {
			return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "pending", "action": "retry", "reason_code": "PUSH_RETRY_WAIT", "retryable": true, "job_id": options.ID, "commit_sha": intent.SHA})
		}
		return cliError{"E_PUSH", safeMessage(publishErr.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "notify", "reason_code": "PUBLISH_SUCCEEDED", "job_id": options.ID, "commit_sha": intent.SHA, "owner": options.Owner, "name": options.Name, "ref": options.Ref, "visibility": options.Visibility})
}

func verifyStoredCommit(ctx context.Context, root string, intent state.GitCommitIntentRecord) error {
	runner := gittransaction.SystemRunner{}
	refResult, err := runner.Run(ctx, root, nil, "rev-parse", "--verify", "--quiet", intent.Intent.Ref)
	if err != nil || strings.TrimSpace(refResult.Output) != intent.SHA {
		return errors.New("AutoGit ref does not name the recorded commit")
	}
	treeResult, err := runner.Run(ctx, root, nil, "show", "-s", "--format=%T", "--no-patch", intent.SHA)
	if err != nil || strings.TrimSpace(treeResult.Output) != intent.Intent.TreeOID {
		return errors.New("recorded commit tree does not match its immutable intent")
	}
	return nil
}

func digestDestination(owner, name, visibility, ref string) string {
	h := sha256.Sum256([]byte(owner + "\x00" + name + "\x00" + visibility + "\x00" + ref))
	return "sha256:" + hex.EncodeToString(h[:])
}

func digestText(value string) string {
	h := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(h[:])
}

func buildPublicPreflight(ctx context.Context, trustedDir, root string, intent state.GitCommitIntentRecord, options publishOptions, p policy.Policy) publication.Report {
	ref := "refs/heads/" + options.Ref
	request := publication.Request{
		Mode:                  publication.ModePublic,
		FirstPublication:      true,
		PublicConsent:         options.PublicConsent,
		CandidateDigest:       intent.Intent.CandidateDigest,
		BaseDigest:            digestText(intent.Intent.ParentSHA),
		PolicyDigest:          intent.Intent.PolicyDigest,
		GuardDigest:           intent.Intent.GuardDigest,
		VerifierSetDigest:     intent.Intent.VerifierDigest,
		Destination:           publication.Destination{Provider: "github", Host: "github.com", Owner: options.Owner, Repository: options.Name, Visibility: publication.VisibilityPublic, Ref: ref},
		ObservedDestination:   publication.Destination{Provider: "github", Host: "github.com", Owner: options.Owner, Repository: options.Name, Visibility: publication.VisibilityPublic, Ref: ref},
		DestinationConfirmed:  options.ConfirmDestination,
		Readiness:             publication.Readiness{Tests: publication.StatusUnknown, CI: publication.StatusUnknown},
		WorkflowSafe:          p.Workflow == "safe",
		WorkflowSolo:          p.Workflow == "solo",
		FeatureBranchApproved: options.FeatureBranchApproved,
		ProtectedBranch:       options.ProtectedBranch,
		StatusChecksRequired:  options.StatusChecksRequired,
		StatusChecksPassed:    options.StatusChecksPassed,
	}
	if options.Tests != "" {
		request.Readiness.Tests = options.Tests
	}
	if options.CI != "" {
		request.Readiness.CI = options.CI
	}
	entries, err := gittransaction.SnapshotAtCommit(ctx, gittransaction.SystemRunner{}, root, intent.SHA, 64<<20)
	if err != nil {
		return publication.Evaluate(request)
	}
	files := make([]publication.FileMetadata, 0, len(entries))
	var readme []byte
	readmePath := ""
	licensePresent := false
	licensePath := ""
	for _, entry := range entries {
		files = append(files, publication.FileMetadata{Path: entry.Path, Bytes: int64(len(entry.Content)), Mode: uint32(entry.Mode.Perm())})
		if strings.EqualFold(entry.Path, "README.md") || strings.EqualFold(entry.Path, "README") {
			if readme == nil {
				readme = append([]byte(nil), entry.Content...)
				readmePath = entry.Path
			}
		}
		if options.License != "" && strings.EqualFold(entry.Path, options.License) {
			licensePresent = true
			licensePath = entry.Path
		}
	}
	request.Files = files
	request.README = publication.READMEInput{Path: readmePath, Content: readme}
	request.License = publication.LicenseEvidence{Selected: options.License, FilePath: licensePath, Present: licensePresent}
	scan := security.Scanner{}.Scan(ctx, security.CandidateSnapshot{Files: treeSecurityFiles(entries)})
	request.CandidateScan = publication.ScanEvidence{Scope: publication.ScanCandidate, CandidateDigest: intent.Intent.CandidateDigest, PolicyDigest: intent.Intent.PolicyDigest, Passed: scan.Safe(), Findings: len(scan.Findings), ReasonCodes: append([]string(nil), scan.ReasonCodes...), Digest: digestValue(scan)}
	history, historyErr := historyscan.ScanHistory(ctx, gittransaction.SystemRunner{}, historyscan.Request{RepoRoot: root, CandidateSHA: intent.SHA, PolicyDigest: intent.Intent.PolicyDigest})
	if historyErr == nil {
		request.HistoryScan = publication.ScanEvidence{Scope: publication.ScanHistory, CandidateDigest: intent.Intent.CandidateDigest, PolicyDigest: intent.Intent.PolicyDigest, Passed: history.Safe(), Findings: len(history.Findings), ReasonCodes: append([]string(nil), history.ReasonCodes...), Digest: digestValue(history)}
	}
	if options.Verifiers != "" {
		if registry, loadErr := verification.LoadTrustedRegistryFile(options.Verifiers, trustedDir, 0); loadErr == nil {
			if verifyRoot, rootErr := writePreflightSnapshot(entries); rootErr == nil {
				defer os.RemoveAll(verifyRoot)
				verificationPolicy := verification.VerificationPolicy{Visibility: publication.VisibilityPublic}
				verificationRequest := verification.TrustedRequest{CandidateDigest: intent.Intent.CandidateDigest, BaseDigest: digestText(intent.Intent.ParentSHA), PolicyDigest: intent.Intent.PolicyDigest, GuardDigest: intent.Intent.GuardDigest, Dir: verifyRoot}
				result, verifyErr := registry.Verify(ctx, verificationPolicy, verificationRequest, verification.ExecRunner{})
				if verifyErr == nil {
					passedCount := 0
					for _, evidence := range result.Evidence {
						if evidence.ValidForTrusted(intent.Intent.CandidateDigest, digestText(intent.Intent.ParentSHA), intent.Intent.PolicyDigest, intent.Intent.GuardDigest, registry.VerifierSetDigest) {
							passedCount++
						}
					}
					request.Verification = publication.VerificationEvidence{CandidateDigest: intent.Intent.CandidateDigest, BaseDigest: digestText(intent.Intent.ParentSHA), PolicyDigest: intent.Intent.PolicyDigest, GuardDigest: intent.Intent.GuardDigest, VerifierSetDigest: registry.VerifierSetDigest, Passed: result.ValidFor(verificationRequest, verificationPolicy, registry), Required: len(result.Evidence), PassedCount: passedCount, Digest: result.EvidenceDigest}
				}
			}
		}
	}
	return publication.Evaluate(request)
}

func writePreflightSnapshot(entries []gittransaction.SnapshotEntry) (string, error) {
	root, err := os.MkdirTemp("", "autogit-public-preflight-")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	for _, entry := range entries {
		if entry.Mode&os.FileMode(0120000) == os.FileMode(0120000) || entry.Delete {
			return "", errors.New("public verification snapshot contains unsupported entry")
		}
		path := filepath.Join(root, filepath.FromSlash(entry.Path))
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", errors.New("public verification snapshot path escapes root")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, entry.Content, 0600); err != nil {
			return "", err
		}
		if err := os.Chmod(path, entry.Mode.Perm()); err != nil {
			return "", err
		}
	}
	cleanup = false
	return root, nil
}

func treeSecurityFiles(entries []gittransaction.SnapshotEntry) []security.CandidateFile {
	files := make([]security.CandidateFile, 0, len(entries))
	for _, entry := range entries {
		files = append(files, security.CandidateFile{Path: entry.Path, Content: append([]byte(nil), entry.Content...), Mode: uint32(entry.Mode), Symlink: entry.Mode&os.FileMode(0120000) == os.FileMode(0120000)})
	}
	return files
}

func digestValue(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
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
		if name == "--all-owned" {
			if seen[name] {
				return syncOptions{}, cliError{"E_USAGE", "--all-owned may be provided once"}
			}
			options.AllOwned = true
			seen[name] = true
			continue
		}
		if name != "--id" && name != "--repo" && name != "--session" && name != "--client" && name != "--message" && name != "--verifiers" && name != "--path" {
			return syncOptions{}, cliError{"E_USAGE", "sync supports baseline fields plus --complete, --all-owned, --id, --message, and --verifiers"}
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
	if options.Repo == "" || options.Session == "" || options.Client == "" || (!options.AllOwned && len(options.Paths) == 0) {
		return syncOptions{}, cliError{"E_SCOPE", "--repo, --session, --client, and at least one --path are required for sync unless --all-owned is used"}
	}
	if options.AllOwned && len(options.Paths) > 0 {
		return syncOptions{}, cliError{"E_USAGE", "--all-owned cannot be combined with --path"}
	}
	if options.AllOwned && !options.Complete {
		return syncOptions{}, cliError{"E_USAGE", "--all-owned requires --complete"}
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
		return runSyncComplete(context.Background(), options, dir, info, key, db, out)
	}
	baseline, err := session.New(db).CaptureAndRecord(context.Background(), session.Request{SessionID: options.Session, RepositoryID: info.RepoID, ClientID: options.Client, Root: info.Root, Paths: options.Paths, IdentityKey: key})
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

func runSyncComplete(ctx context.Context, options syncOptions, dir string, info repository.Info, identityKey []byte, db *state.Store, out io.Writer) error {
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
	started, err := service.ResumeFromDurable(ctx, session.Request{SessionID: options.Session, RepositoryID: info.RepoID, ClientID: options.Client, Root: info.Root, Paths: options.Paths, IdentityKey: identityKey}, session.DurableBaseline{
		Head: durable.BaselineHead, IndexDigest: durable.BaselineIndex, StatusDigest: durable.StatusDigest, PathsDigest: durable.BaselinePathsDigest, Evidence: durable.BaselineEvidence,
	})
	if err != nil {
		return cliError{"E_REPOSITORY", safeMessage(err.Error())}
	}
	plan, err := service.BuildOwnedPlanAtCurrent(ctx, started.Request, started.Baseline)
	if err != nil {
		return cliError{"E_SCOPE", safeMessage(err.Error())}
	}
	workflowService := localworkflow.Service{Git: gittransaction.SystemRunner{}, Intents: gittransaction.NewStateIntentPort(db), VerifierRunner: verification.ExecRunner{}, Lease: coordinator.StateLease{DB: db}, TrustedVerifierDir: dir, IdentityKey: identityKey}
	result, err := workflowService.RunPlan(ctx, localworkflow.Request{ID: options.ID, RepositoryDir: info.Root, Message: options.Message, Policy: loadPolicy(dir, info.RepoID), Verifiers: registry}, plan)
	if err != nil {
		return cliError{"E_COMMIT", safeMessage(err.Error())}
	}
	intent, err := db.GitCommitIntentRecord(ctx, options.ID)
	if err != nil {
		return cliError{"E_STATE", "cannot read committed intent facts"}
	}
	if err := emitSyncDomainFacts(ctx, filepath.Join(dir, "state.db"), loadPolicy(dir, info.RepoID), info, options, result, intent); err != nil {
		return cliError{"E_STATE", safeMessage(err.Error())}
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"schema_version": "autogit.result/1", "disposition": "accepted", "action": "commit", "reason_code": "SYNC_COMMITTED",
		"session_id": options.Session, "commit_sha": result.Commit.SHA, "ref": result.Commit.Ref, "ownership_digest": result.OwnershipDigest,
		"verification": map[string]any{"verifier_set_digest": result.Verification.VerifierSetDigest, "evidence_digest": result.Verification.EvidenceDigest},
	})
}

func emitSyncDomainFacts(ctx context.Context, statePath string, p policy.Policy, info repository.Info, options syncOptions, result localworkflow.Result, intent state.GitCommitIntentRecord) error {
	store, err := events.OpenStore(statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	base := digestText(result.Commit.ParentSHA)
	changeID := "change/" + options.ID
	taskID := "task/" + options.Session
	correlation := "sync/" + options.ID
	occurredAt := time.Unix(0, intent.CreatedAt).UTC().Format(time.RFC3339Nano)
	if intent.CreatedAt <= 0 {
		occurredAt = "1970-01-01T00:00:00Z"
	}
	evidence := map[string]any{
		"candidate_digest": result.Commit.CandidateDigest,
		"base_digest":      base,
		"tree_digest":      result.Commit.CandidateDigest,
		"index_digest":     result.OwnershipDigest,
		"policy_digest":    result.PolicyDigest,
		"verifier_digest":  result.Verification.VerifierSetDigest,
		"guard_digest":     result.GuardDigest,
		"message_digest":   result.Commit.MessageDigest,
	}
	for _, eventType := range []string{"change.detected", "change.staged"} {
		if err := emitDomainFact(ctx, store, p, events.DomainEventRequest{EventType: eventType, OccurredAt: occurredAt, RepoID: info.RepoID, WorktreeID: info.WorktreeID, SessionID: options.Session, TaskID: taskID, ChangeID: changeID, CorrelationID: correlation, IdempotencyKey: correlation + "/" + eventType, Payload: evidence}); err != nil {
			return err
		}
	}
	verificationPayload := cloneFactPayload(evidence)
	verificationPayload["verification_id"] = "verification/" + options.ID
	verificationPayload["evidence_digest"] = result.Verification.EvidenceDigest
	if err := emitDomainFact(ctx, store, p, events.DomainEventRequest{EventType: "verification.passed", OccurredAt: occurredAt, RepoID: info.RepoID, WorktreeID: info.WorktreeID, SessionID: options.Session, TaskID: taskID, ChangeID: changeID, CorrelationID: correlation, IdempotencyKey: correlation + "/verification.passed", Payload: verificationPayload}); err != nil {
		return err
	}
	commitPayload := cloneFactPayload(evidence)
	commitPayload["commit_job_id"] = options.ID
	if err := emitDomainFact(ctx, store, p, events.DomainEventRequest{EventType: "commit.requested", OccurredAt: occurredAt, RepoID: info.RepoID, WorktreeID: info.WorktreeID, SessionID: options.Session, TaskID: taskID, ChangeID: changeID, CorrelationID: correlation, IdempotencyKey: correlation + "/commit.requested", Payload: commitPayload}); err != nil {
		return err
	}
	commitPayload["commit_sha"] = result.Commit.SHA
	return emitDomainFact(ctx, store, p, events.DomainEventRequest{EventType: "commit.created", OccurredAt: occurredAt, RepoID: info.RepoID, WorktreeID: info.WorktreeID, SessionID: options.Session, TaskID: taskID, ChangeID: changeID, CorrelationID: correlation, IdempotencyKey: correlation + "/commit.created", Payload: commitPayload})
}

func emitDomainFact(ctx context.Context, store *events.Store, p policy.Policy, request events.DomainEventRequest) error {
	b, err := events.NewDomainEvent(request)
	if err != nil {
		return err
	}
	result, err := app.New(store, p, nil).ApplyDomain(ctx, b)
	if err != nil {
		return err
	}
	if result.Disposition == "rejected" || result.Disposition == "pending" {
		return fmt.Errorf("domain fact %s was %s", request.EventType, result.Disposition)
	}
	return nil
}

func cloneFactPayload(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func emitPublishDomainFacts(ctx context.Context, statePath string, p policy.Policy, info repository.Info, job state.PushJob, operationErr error) error {
	store, err := events.OpenStore(statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	sessionID, taskID, changeID, ok := lifecycleScopeForCommit(store, info.RepoID, job.CommitSHA)
	if !ok {
		// Older/manual commit intents may predate lifecycle projection. The
		// publication side effect remains authoritative in state; do not turn a
		// missing optional projection into a provider failure.
		return nil
	}
	ref := job.Ref
	if !strings.HasPrefix(ref, "refs/") {
		ref = "refs/heads/" + ref
	}
	when := time.Unix(0, job.CreatedAt).UTC().Format(time.RFC3339Nano)
	if job.CreatedAt <= 0 {
		when = "1970-01-01T00:00:00Z"
	}
	correlation := "publish/" + job.ID
	payload := map[string]any{"push_job_id": job.ID, "commit_job_id": job.CommitJobID, "commit_sha": job.CommitSHA, "remote_digest": job.RemoteDigest, "ref": ref}
	if payload["commit_job_id"] == "" {
		payload["commit_job_id"] = job.ID
	}
	base := events.DomainEventRequest{OccurredAt: when, RepoID: info.RepoID, WorktreeID: info.WorktreeID, SessionID: sessionID, TaskID: taskID, ChangeID: changeID, CorrelationID: correlation}
	requested := base
	requested.EventType = "push.requested"
	requested.IdempotencyKey = correlation + "/push.requested"
	requested.Payload = cloneFactPayload(payload)
	if err := emitDomainFact(ctx, store, p, requested); err != nil {
		return err
	}
	eventType := "push.succeeded"
	if operationErr != nil {
		eventType = "push.failed"
		payload["error_code"] = publishFactErrorCode(operationErr)
	}
	fact := base
	fact.EventType = eventType
	fact.IdempotencyKey = correlation + "/" + eventType
	fact.Payload = payload
	return emitDomainFact(ctx, store, p, fact)
}

func lifecycleScopeForCommit(store *events.Store, repositoryID, sha string) (string, string, string, bool) {
	data, _, err := store.LifecycleProjection(repositoryID)
	if err != nil {
		return "", "", "", false
	}
	var projected lifecycle.State
	if json.Unmarshal(data, &projected) != nil || projected.Session.ID == "" {
		return "", "", "", false
	}
	for _, commit := range projected.Commits {
		if commit.CommitSHA != sha || commit.State != lifecycle.CommitCreatedStatus {
			continue
		}
		candidate := projected.Candidates[commit.CandidateID]
		if candidate.ID == "" || candidate.TaskID == "" {
			return "", "", "", false
		}
		return projected.Session.ID, candidate.TaskID, candidate.ID, true
	}
	return "", "", "", false
}

func publishFactErrorCode(err error) string {
	switch {
	case errors.Is(err, provider.ErrAuth):
		return "auth"
	case errors.Is(err, provider.ErrNonFastForward):
		return "non-fast-forward"
	case errors.Is(err, provider.ErrProtectedBranch):
		return "unsafe"
	case errors.Is(err, provider.ErrCollision):
		return "collision"
	case errors.Is(err, provider.ErrSecretScanning), errors.Is(err, provider.ErrRemoteBinding):
		return "unsafe"
	default:
		return "unknown"
	}
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
		if name == "--all-owned" {
			if seen[name] {
				return verifyOptions{}, cliError{"E_USAGE", "--all-owned may be provided once"}
			}
			options.AllOwned = true
			seen[name] = true
			continue
		}
		if name != "--id" && name != "--repo" && name != "--session" && name != "--client" && name != "--message" && name != "--verifiers" && name != "--path" {
			return verifyOptions{}, cliError{"E_USAGE", "verify supports --id, --repo, --session, --client, --message, --verifiers, --all-owned, and repeated --path"}
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
	if options.ID == "" || options.Repo == "" || options.Session == "" || options.Client == "" || options.Message == "" || options.Verifiers == "" || (!options.AllOwned && len(options.Paths) == 0) {
		return verifyOptions{}, cliError{"E_SCOPE", "--id, --repo, --session, --client, --message, --verifiers, and at least one --path are required for verify unless --all-owned is used"}
	}
	if options.AllOwned && len(options.Paths) > 0 {
		return verifyOptions{}, cliError{"E_USAGE", "--all-owned cannot be combined with --path"}
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
	started, err := service.ResumeFromDurable(context.Background(), session.Request{SessionID: options.Session, RepositoryID: info.RepoID, ClientID: options.Client, Root: info.Root, Paths: options.Paths, IdentityKey: key}, session.DurableBaseline{
		Head: durable.BaselineHead, IndexDigest: durable.BaselineIndex, StatusDigest: durable.StatusDigest, PathsDigest: durable.BaselinePathsDigest, Evidence: durable.BaselineEvidence,
	})
	if err != nil {
		return cliError{"E_REPOSITORY", safeMessage(err.Error())}
	}
	plan, err := service.BuildOwnedPlanAtCurrent(context.Background(), started.Request, started.Baseline)
	if err != nil {
		return cliError{"E_SCOPE", safeMessage(err.Error())}
	}
	workflowService := localworkflow.Service{Git: gittransaction.SystemRunner{}, Intents: gittransaction.NewStateIntentPort(db), VerifierRunner: verification.ExecRunner{}, TrustedVerifierDir: dir, IdentityKey: key}
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
	retryErr := coord.RetryPush(context.Background(), options.ID)
	if refreshed, refreshErr := db.PushJob(options.ID); refreshErr == nil {
		if factErr := emitPublishDomainFacts(context.Background(), filepath.Join(dir, "state.db"), loadPolicy(dir, info.RepoID), info, refreshed, retryErr); factErr != nil {
			return cliError{"E_STATE", safeMessage(factErr.Error())}
		}
	}
	if retryErr != nil {
		status, _, statusErr := coord.Store.PushStatus(context.Background(), options.ID)
		if statusErr == nil && status == state.PushRetryWait {
			return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "pending", "action": "retry", "reason_code": "PUSH_RETRY_WAIT", "retryable": true, "job_id": options.ID})
		}
		return cliError{"E_PUSH", safeMessage(retryErr.Error())}
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
	if name == "" || strings.ContainsAny(name, `/\\\x00\r\n`) {
		return "", errors.New("invalid trusted executable")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("invalid trusted executable")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("invalid trusted executable")
	}
	if info.Mode().Perm()&0111 == 0 {
		return "", errors.New("invalid trusted executable")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", errors.New("invalid trusted executable")
	}
	return filepath.Join(parent, filepath.Base(path)), nil
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
	a.IdentityKey = append([]byte(nil), key...)
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
