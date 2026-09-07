package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type suiteResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type control struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Requirements []string `json:"requirements"`
	Commands     []string `json:"commands"`
	Threats      []string `json:"threats,omitempty"`
}

type evidenceManifest struct {
	SchemaVersion string        `json:"schema_version"`
	Commit        string        `json:"commit"`
	WorkingTree   string        `json:"working_tree"`
	GeneratedAt   string        `json:"generated_at"`
	GoVersion     string        `json:"go_version"`
	Platform      string        `json:"platform"`
	Suites        []suiteResult `json:"suites"`
	Controls      []control     `json:"controls"`
}

var controls = []control{
	{ID: "P4-01", Status: "implemented", Requirements: []string{"NFR-TST-001", "NFR-REL-001"}, Commands: []string{"bash scripts/test-suites.sh presubmit", "bash scripts/test-suites.sh core", "bash scripts/test-suites.sh race", "bash scripts/test-suites.sh soak", "bash scripts/test-suites.sh fuzz", "bash scripts/test-suites.sh canary", "bash scripts/test-suites.sh release"}, Threats: []string{"RISK-SLOW-FEEDBACK", "GATE-006"}},
	{ID: "P4-02", Status: "implemented", Requirements: []string{"NFR-SEC-001", "NFR-REL-001"}, Commands: []string{"bash scripts/check-shell.sh", "go test ./scripts -run TestShellScripts"}, Threats: []string{"NFR-SEC-001", "NFR-REL-001"}},
	{ID: "P4-03", Status: "implemented", Requirements: []string{"NFR-POR-001", "NFR-COM-001"}, Commands: []string{"bash scripts/artifact-smoke.sh --binary PATH", "go test -tags release_integration ./scripts -run TestReleaseBuild"}, Threats: []string{"RISK-RELEASE-IDENTITY", "NFR-POR-001"}},
	{ID: "P4-04", Status: "implemented", Requirements: []string{"FR-SEC-001", "NFR-TST-001"}, Commands: []string{"bash scripts/test-suites.sh fuzz", "go test ./..."}, Threats: []string{"GATE-004", "GATE-005"}},
	{ID: "P4-05", Status: "implemented", Requirements: []string{"FR-OPS-001", "NFR-REL-001"}, Commands: []string{"go test ./internal/provider -run TestRESTNetworkStall", "go test ./internal/provider -run TestRESTPartialResponse", "go test ./..."}, Threats: []string{"RISK-DEPENDENCY-FAILURE", "GATE-006"}},
	{ID: "P4-06", Status: "implemented", Requirements: []string{"FR-GIT-001", "FR-PUB-001"}, Commands: []string{"bash scripts/scenario-eval.sh --binary PATH", "go test ./scripts -run TestScenarioEvaluation"}, Threats: []string{"GATE-003", "GATE-005"}},
	{ID: "P4-07", Status: "implemented", Requirements: []string{"FR-ADP-001", "NFR-COM-001"}, Commands: []string{"bash scripts/check-compatibility.sh", "go test ./internal/compatibility"}, Threats: []string{"RISK-CLIENT-DRIFT", "RISK-API-DRIFT"}},
	{ID: "P4-08", Status: "implemented", Requirements: []string{"NFR-TST-001", "NFR-OBS-001"}, Commands: []string{"go run ./cmd/autogit-evidence --results-dir DIR --output FILE"}, Threats: []string{"GATE-002", "NFR-REL-001"}},
	{ID: "P5-01", Status: "implemented", Requirements: []string{"NFR-POR-001"}, Commands: []string{"test -f LICENSE", "test -f SECURITY.md", "test -f CONTRIBUTING.md", "test -f CODE_OF_CONDUCT.md", "test -f CODEOWNERS", "test -f CHANGELOG.md"}, Threats: []string{"RISK-RELEASE-IDENTITY"}},
	{ID: "P5-02", Status: "implemented", Requirements: []string{"NFR-SEC-001", "NFR-COM-001"}, Commands: []string{"bash scripts/check-dependencies.sh", "actionlint .github/workflows/*.yml"}, Threats: []string{"NFR-SUP-001", "RISK-SUPPLY-CHAIN"}},
}

func main() {
	resultsDir := flag.String("results-dir", "", "directory containing one passed/failed file per suite")
	output := flag.String("output", "", "output JSON path")
	allowDirty := flag.Bool("allow-dirty", false, "record a working tree that is not clean")
	flag.Parse()
	if *resultsDir == "" || *output == "" {
		fail("results-dir and output are required")
	}
	commit := gitHead()
	if commit == "" {
		fail("cannot determine release commit")
	}
	workingTree := "clean"
	if output := gitStatus(); output != "" {
		workingTree = "dirty"
		if !*allowDirty {
			fail("working tree is dirty; use --allow-dirty only for local evidence")
		}
	}
	suites, err := readSuites(*resultsDir)
	if err != nil {
		fail(err.Error())
	}
	for _, suite := range suites {
		if suite.Status != "passed" {
			fail("cannot publish evidence with a failed suite: " + suite.Name)
		}
	}
	manifest := evidenceManifest{
		SchemaVersion: "autogit.release-evidence/1",
		Commit:        commit, WorkingTree: workingTree, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Suites: suites, Controls: cloneControls(),
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fail(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(*output, append(data, '\n'), 0600); err != nil {
		fail(err.Error())
	}
}

func readSuites(dir string) ([]suiteResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read evidence results: %w", err)
	}
	results := make([]suiteResult, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".status") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".status")
		if name == "" || strings.ContainsAny(name, "/\\\x00") {
			return nil, errors.New("invalid evidence suite name")
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name())) // #nosec G304 -- entry.Name comes from the explicitly enumerated results directory.
		if err != nil {
			return nil, err
		}
		status := strings.TrimSpace(string(data))
		if status != "passed" && status != "failed" {
			return nil, fmt.Errorf("invalid evidence status for %s", name)
		}
		results = append(results, suiteResult{Name: name, Status: status})
	}
	if len(results) == 0 {
		return nil, errors.New("no suite results were provided")
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}

func cloneControls() []control {
	result := make([]control, len(controls))
	for i, item := range controls {
		result[i] = item
		result[i].Requirements = append([]string(nil), item.Requirements...)
		result[i].Commands = append([]string(nil), item.Commands...)
		result[i].Threats = append([]string(nil), item.Threats...)
	}
	return result
}

func gitHead() string {
	return strings.TrimSpace(gitCommand("rev-parse", "--verify", "HEAD"))
}

func gitStatus() string {
	return gitCommand("status", "--porcelain")
}

func gitCommand(args ...string) string {
	output, err := exec.Command("git", args...).CombinedOutput() // #nosec G204 -- every caller supplies a fixed Git subcommand and arguments.
	if err != nil {
		return ""
	}
	return string(output)
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
