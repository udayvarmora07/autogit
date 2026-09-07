package adapters

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"autogit/internal/process"
)

// RegistryVersion is independent from the wire-event version. Changing a
// client contract or a fixture requires a registry revision even when the
// canonical AutoGit event envelope remains compatible.
const RegistryVersion = "autogit.adapter-registry/1"

type ContractStatus string

const (
	ContractSupported       ContractStatus = "supported"
	ContractObservationOnly ContractStatus = "observation-only"
)

type ConfigContract struct {
	Format    string
	Shape     string
	HookEvent string
}

type RegistryEntry struct {
	Adapter           string
	SchemaMajors      []string
	ClientVersions    []string
	EventMappings     map[string]string
	InstallSupported  bool
	Contract          string
	Status            ContractStatus
	UnsupportedReason string
	Capabilities      Capabilities
	Config            ConfigContract
	Executables       []string
	VersionArgs       []string
	FixtureVersion    string
	PayloadFixture    string
	ConfigFixture     string
	SourceURL         string
	SourceRevision    string
}

type CapabilityRegistry struct {
	Version string
	Entries []RegistryEntry
}

type Fixture struct {
	Adapter       string
	Version       string
	Payload       []byte
	Config        []byte
	PayloadDigest string
	ConfigDigest  string
}

// Registry is the single compatibility source used by translators, CLI
// discovery, probes, and installers. The returned value is deep-copied so a
// caller cannot mutate the process-wide contract.
func Registry() CapabilityRegistry {
	entries := make([]RegistryEntry, 0, len(registryEntries))
	for _, entry := range registryEntries {
		entries = append(entries, cloneRegistryEntry(entry))
	}
	return CapabilityRegistry{Version: RegistryVersion, Entries: entries}
}

func RegistryEntryFor(name string) (RegistryEntry, error) {
	for _, entry := range registryEntries {
		if entry.Adapter == name {
			return cloneRegistryEntry(entry), nil
		}
	}
	return RegistryEntry{}, fmt.Errorf("%w: %s", ErrAdapter, name)
}

func cloneRegistryEntry(in RegistryEntry) RegistryEntry {
	out := in
	out.SchemaMajors = append([]string(nil), in.SchemaMajors...)
	out.ClientVersions = append([]string(nil), in.ClientVersions...)
	out.EventMappings = cloneStringMap(in.EventMappings)
	out.Executables = append([]string(nil), in.Executables...)
	out.VersionArgs = append([]string(nil), in.VersionArgs...)
	return out
}

//go:embed fixtures/*
var registryFixtures embed.FS

var registryEntries = []RegistryEntry{
	{
		Adapter: "codex", SchemaMajors: []string{"autogit.event/1"},
		ClientVersions:   []string{"unknown", "0.x", "1.x", "2.x"},
		EventMappings:    map[string]string{"hook_event_name": "event", "session_id": "scope.session_id", "task_id": "scope.task_id"},
		InstallSupported: true, Contract: "official-hook", Status: ContractSupported,
		Capabilities: Capabilities{QueueState: "unknown", TaskBoundaries: "native", ChangedPaths: "reported", MonotonicSequence: true},
		Config:       ConfigContract{Format: "json", Shape: "event-groups", HookEvent: "SessionEnd"},
		Executables:  []string{"codex"}, VersionArgs: []string{"--version"}, FixtureVersion: "0.153.x",
		PayloadFixture: "fixtures/codex-0.153.x.payload.json", ConfigFixture: "fixtures/codex-0.153.x.config.json",
		SourceURL: "https://learn.chatgpt.com/docs/hooks", SourceRevision: "2026-09-07",
	},
	{
		Adapter: "claude-code", SchemaMajors: []string{"autogit.event/1"},
		ClientVersions:   []string{"unknown", "1.x", "2.x"},
		EventMappings:    map[string]string{"hook_event_name": "event", "session_id": "scope.session_id", "task_id": "scope.task_id", "cwd": "project.candidate_root"},
		InstallSupported: true, Contract: "official-hook", Status: ContractSupported,
		Capabilities: Capabilities{QueueState: "unknown", TaskBoundaries: "native", ChangedPaths: "reported", MonotonicSequence: true},
		Config:       ConfigContract{Format: "json", Shape: "event-groups", HookEvent: "TaskCompleted"},
		Executables:  []string{"claude"}, VersionArgs: []string{"--version"}, FixtureVersion: "2.1.x",
		PayloadFixture: "fixtures/claude-code-2.1.x.payload.json", ConfigFixture: "fixtures/claude-code-2.1.x.config.json",
		SourceURL: "https://code.claude.com/docs/en/hooks", SourceRevision: "2026-09-07",
	},
	{
		Adapter: "cursor", SchemaMajors: []string{"autogit.event/1"},
		ClientVersions:   []string{"unknown", "3.x"},
		EventMappings:    map[string]string{"hook_event_name": "event", "conversation_id": "scope.session_id", "workspace_roots": "project.candidate_root"},
		InstallSupported: true, Contract: "official-hook", Status: ContractSupported,
		Capabilities: Capabilities{QueueState: "none", TaskBoundaries: "synthetic", ChangedPaths: "reported", MonotonicSequence: false},
		Config:       ConfigContract{Format: "json", Shape: "flat-hooks", HookEvent: "sessionEnd"},
		Executables:  []string{"cursor"}, VersionArgs: []string{"--version"}, FixtureVersion: "3.14.x",
		PayloadFixture: "fixtures/cursor-3.14.x.payload.json", ConfigFixture: "fixtures/cursor-3.14.x.config.json",
		SourceURL: "https://prod.cursor.com/docs/hooks", SourceRevision: "2026-09-07",
	},
	{
		Adapter: "gemini-cli", SchemaMajors: []string{"autogit.event/1"},
		ClientVersions:   []string{"unknown", "0.x", "1.x", "2.x"},
		EventMappings:    map[string]string{"hook_event_name": "event", "session_id": "scope.session_id", "task_id": "scope.task_id"},
		InstallSupported: true, Contract: "official-hook", Status: ContractSupported,
		Capabilities: Capabilities{QueueState: "unknown", TaskBoundaries: "native", ChangedPaths: "reported", MonotonicSequence: true},
		Config:       ConfigContract{Format: "json", Shape: "event-groups", HookEvent: "SessionEnd"},
		Executables:  []string{"gemini"}, VersionArgs: []string{"--version"}, FixtureVersion: "0.55.x",
		PayloadFixture: "fixtures/gemini-cli-0.55.x.payload.json", ConfigFixture: "fixtures/gemini-cli-0.55.x.config.json",
		SourceURL: "https://github.com/google-gemini/gemini-cli/blob/main/docs/hooks/reference.md", SourceRevision: "2026-09-07",
	},
	{
		Adapter: "opencode", SchemaMajors: []string{"autogit.event/1"},
		ClientVersions:   []string{"observation"},
		EventMappings:    map[string]string{"observation": "event", "session": "scope.session_id"},
		InstallSupported: false, Contract: "synthetic-observation", Status: ContractObservationOnly,
		UnsupportedReason: "OpenCode has no stable native command-hook contract pinned by AutoGit; observation-only mode is retained",
		Capabilities:      Capabilities{QueueState: "none", TaskBoundaries: "synthetic", ChangedPaths: "derived"},
		Config:            ConfigContract{Format: "none", Shape: "observation-only"},
		FixtureVersion:    "observation", PayloadFixture: "fixtures/opencode-observation.payload.json", ConfigFixture: "fixtures/opencode-observation.config.json",
		SourceURL: "https://opencode.ai/docs", SourceRevision: "2026-09-07",
	},
	{
		Adapter: "commandcode", SchemaMajors: []string{"autogit.event/1"},
		ClientVersions:   []string{"observation"},
		EventMappings:    map[string]string{"signal": "event", "session_id": "scope.session_id"},
		InstallSupported: false, Contract: "synthetic-observation", Status: ContractObservationOnly,
		UnsupportedReason: "No stable public CommandCode hook configuration contract is pinned by AutoGit; observation-only mode is retained",
		Capabilities:      Capabilities{QueueState: "unknown", TaskBoundaries: "synthetic", ChangedPaths: "derived"},
		Config:            ConfigContract{Format: "none", Shape: "observation-only"},
		FixtureVersion:    "observation", PayloadFixture: "fixtures/commandcode-observation.payload.json", ConfigFixture: "fixtures/commandcode-observation.config.json",
		SourceURL: "", SourceRevision: "2026-09-07",
	},
}

func FixtureFor(adapterName string) (Fixture, error) {
	entry, err := RegistryEntryFor(adapterName)
	if err != nil {
		return Fixture{}, err
	}
	payload, err := fs.ReadFile(registryFixtures, entry.PayloadFixture)
	if err != nil {
		return Fixture{}, fmt.Errorf("read payload fixture: %w", err)
	}
	config, err := fs.ReadFile(registryFixtures, entry.ConfigFixture)
	if err != nil {
		return Fixture{}, fmt.Errorf("read config fixture: %w", err)
	}
	payload = append([]byte(nil), payload...)
	config = append([]byte(nil), config...)
	return Fixture{Adapter: adapterName, Version: entry.FixtureVersion, Payload: payload, Config: config,
		PayloadDigest: bytesDigest(payload), ConfigDigest: bytesDigest(config)}, nil
}

func bytesDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// ValidateRegistry is used by tests and release tooling. It verifies that
// every advertised fixture exists, is one JSON value, and contains no
// credentials or source-bearing fields. It deliberately does not translate a
// fixture because translation requires trusted repository scope.
func ValidateRegistry() error {
	if len(registryEntries) == 0 {
		return errors.New("adapter registry is empty")
	}
	seen := map[string]bool{}
	for _, entry := range registryEntries {
		if entry.Adapter == "" || seen[entry.Adapter] || entry.FixtureVersion == "" {
			return fmt.Errorf("invalid adapter registry entry %q", entry.Adapter)
		}
		seen[entry.Adapter] = true
		fixture, err := FixtureFor(entry.Adapter)
		if err != nil {
			return err
		}
		for _, data := range [][]byte{fixture.Payload, fixture.Config} {
			if err := validateFixtureJSON(data); err != nil {
				return fmt.Errorf("%s fixture: %w", entry.Adapter, err)
			}
			lower := strings.ToLower(string(data))
			for _, forbidden := range []string{"password", "secret", "private_key", "authorization", "bearer ", "gh_token", "github_token"} {
				if strings.Contains(lower, forbidden) {
					return fmt.Errorf("%s fixture contains sensitive field %q", entry.Adapter, forbidden)
				}
			}
		}
	}
	return nil
}

func validateFixtureJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return errors.New("fixture must be a JSON object")
	}
	if _, err := decodeStrict(data); err != nil {
		return err
	}
	return nil
}

type ProbeStatus string

const (
	ProbeSupported   ProbeStatus = "supported"
	ProbeDegraded    ProbeStatus = "degraded"
	ProbeUnsupported ProbeStatus = "unsupported"
)

type ProbeExecution struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}

type ProbeOptions struct {
	Executable string
	Timeout    time.Duration
	// Run allows release tests to exercise the decision logic without starting
	// a client. Production callers leave it nil and use the bounded process
	// boundary below.
	Run func(context.Context, string, []string) (ProbeExecution, error)
}

type ProbeReport struct {
	Adapter      string       `json:"adapter"`
	Registry     string       `json:"registry_version"`
	Status       ProbeStatus  `json:"status"`
	Version      string       `json:"version,omitempty"`
	Executable   string       `json:"executable,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	Fixture      string       `json:"fixture_version"`
	Capabilities Capabilities `json:"capabilities"`
}

const (
	defaultProbeTimeout = 5 * time.Second
	maxProbeTimeout     = 10 * time.Second
)

var versionRE = regexp.MustCompile(`(?i)(?:^|[^0-9])v?([0-9]+(?:\.[0-9]+){0,2})(?:[-+][0-9A-Za-z.-]+)?`)

func Probe(ctx context.Context, adapterName string, options ProbeOptions) (ProbeReport, error) {
	if ctx == nil {
		return ProbeReport{}, errors.New("probe context is required")
	}
	entry, err := RegistryEntryFor(adapterName)
	if err != nil {
		return ProbeReport{}, err
	}
	report := ProbeReport{Adapter: entry.Adapter, Registry: RegistryVersion, Fixture: entry.FixtureVersion, Capabilities: entry.Capabilities}
	if entry.Status != ContractSupported || !entry.InstallSupported {
		report.Status = ProbeUnsupported
		report.Reason = entry.UnsupportedReason
		return report, nil
	}
	if err := ctx.Err(); err != nil {
		return ProbeReport{}, err
	}
	executable, err := probeExecutable(options.Executable, entry.Executables)
	if err != nil {
		report.Status = ProbeUnsupported
		report.Reason = "client executable is unavailable"
		return report, nil
	}
	report.Executable = executable
	timeout := options.Timeout
	if timeout <= 0 || timeout > maxProbeTimeout {
		timeout = defaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	run := options.Run
	if run == nil {
		run = runProbeCommand
	}
	execution, runErr := run(probeCtx, executable, entry.VersionArgs)
	if runErr != nil || execution.ExitCode != 0 || execution.Truncated {
		report.Status = ProbeDegraded
		report.Reason = "client version probe failed or exceeded its bound"
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			report.Reason = "client version probe timed out"
		}
		return report, nil
	}
	report.Version = extractVersion(execution.Stdout + "\n" + execution.Stderr)
	if report.Version == "" {
		report.Status = ProbeDegraded
		report.Reason = "client version output did not contain a semantic version"
		return report, nil
	}
	if !versionInWindow(report.Version, entry.ClientVersions) {
		report.Status = ProbeDegraded
		report.Reason = "client version is outside the pinned compatibility window"
		return report, nil
	}
	report.Status = ProbeSupported
	return report, nil
}

func ProbeAll(ctx context.Context, options ProbeOptions) ([]ProbeReport, error) {
	registry := Registry()
	reports := make([]ProbeReport, 0, len(registry.Entries))
	for _, entry := range registry.Entries {
		report, err := Probe(ctx, entry.Adapter, options)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func probeExecutable(requested string, candidates []string) (string, error) {
	if requested != "" {
		if !filepath.IsAbs(requested) {
			return "", errors.New("probe executable must be absolute")
		}
		return trustedProbeExecutable(requested)
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		if trusted, trustErr := trustedProbeExecutable(path); trustErr == nil {
			return trusted, nil
		}
	}
	return "", errors.New("no trusted client executable found")
}

func trustedProbeExecutable(path string) (string, error) {
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	// Package managers commonly expose clients through a launcher symlink. The
	// diagnostic boundary follows it once to a canonical target, then validates
	// and executes that target rather than trusting a mutable link spelling.
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", errors.New("client executable is not a regular file")
	}
	resolved, err = filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(resolved); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("client executable is not a regular file")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || (os.PathSeparator != '\\' && info.Mode().Perm()&0111 == 0) {
		return "", errors.New("client executable is not executable")
	}
	return resolved, nil
}

func runProbeCommand(ctx context.Context, executable string, args []string) (ProbeExecution, error) {
	env := []string{}
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	if root := os.Getenv("SYSTEMROOT"); root != "" {
		env = append(env, "SYSTEMROOT="+root)
	}
	result, err := process.Run(ctx, process.Options{Executable: executable, Args: append([]string(nil), args...), Env: env, MaxOutput: 64 << 10, SeparateOutput: true})
	return ProbeExecution{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, Truncated: result.Truncated}, err
}

func extractVersion(output string) string {
	match := versionRE.FindStringSubmatch(output)
	if len(match) == 0 {
		return ""
	}
	return match[1]
}

func versionInWindow(version string, windows []string) bool {
	major := versionMajor(version)
	if major < 0 {
		return false
	}
	for _, window := range windows {
		if window == "unknown" || window == "observation" {
			continue
		}
		if strings.HasSuffix(window, ".x") {
			if parsed, err := strconv.Atoi(strings.TrimSuffix(window, ".x")); err == nil && parsed == major {
				return true
			}
		}
	}
	return false
}

func versionMajor(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return -1
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1
	}
	return major
}

// RegistryFixtureNames returns stable sorted fixture names for release tools.
func RegistryFixtureNames() []string {
	entries := Registry().Entries
	result := make([]string, 0, len(entries)*2)
	for _, entry := range entries {
		result = append(result, entry.PayloadFixture, entry.ConfigFixture)
	}
	sort.Strings(result)
	return result
}
