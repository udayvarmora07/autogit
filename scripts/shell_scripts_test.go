package scripts_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func scriptsRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate scripts directory")
	}
	return filepath.Dir(source)
}

func runShellScript(t *testing.T, env []string, name string, args ...string) ([]byte, error) {
	t.Helper()
	cmdArgs := append([]string{filepath.Join(scriptsRoot(t), name)}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = filepath.Dir(scriptsRoot(t))
	cmd.Env = append(os.Environ(), env...)
	return cmd.CombinedOutput()
}

func TestShellScriptsPassSyntaxValidation(t *testing.T) {
	root := scriptsRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}
		cmd := exec.Command("bash", "-n", filepath.Join(root, entry.Name()))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bash -n %s: %v\n%s", entry.Name(), err, output)
		}
	}
}

func TestDependencyPolicyDoesNotRequireRipgrep(t *testing.T) {
	binDir := t.TempDir()
	rgShim := filepath.Join(binDir, "rg")
	if err := os.WriteFile(rgShim, []byte("#!/usr/bin/env bash\necho 'rg intentionally unavailable for this test' >&2\nexit 127\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PATH=") {
			env = append(env, value)
		}
	}
	env = append(env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmdArgs := []string{filepath.Join(scriptsRoot(t), "check-dependencies.sh")}
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = filepath.Dir(scriptsRoot(t))
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dependency policy without rg: %v\n%s", err, output)
	}
}

func TestTestSuiteDispatcherRejectsUnknownSuite(t *testing.T) {
	output, err := runShellScript(t, nil, "test-suites.sh", "not-a-suite")
	if err == nil || !bytes.Contains(output, []byte("unknown suite")) {
		t.Fatalf("unknown suite result=%v output=%s", err, output)
	}
}

func TestArtifactSmokeRejectsMissingBinaryBeforeCreatingState(t *testing.T) {
	output, err := runShellScript(t, nil, "artifact-smoke.sh", "--binary", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !bytes.Contains(output, []byte("artifact binary is missing")) {
		t.Fatalf("missing binary result=%v output=%s", err, output)
	}
}

func TestReleaseVerifierRejectsChecksumMismatchBeforeAttestation(t *testing.T) {
	directory := writeReleaseEvidenceFixture(t)
	if err := os.WriteFile(filepath.Join(directory, releaseEvidenceNames[0]), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, nil, "verify-release-artifacts.sh", "--directory", directory, "--repo", "owner/repo", "--tag", "v1.2.3", "--commit", strings.Repeat("a", 40))
	if err == nil || !bytes.Contains(output, []byte("release checksum verification failed")) {
		t.Fatalf("checksum mismatch result=%v output=%s", err, output)
	}
}

func TestReleaseVerifierRejectsUnexpectedBundleEntry(t *testing.T) {
	directory := writeReleaseEvidenceFixture(t)
	if err := os.WriteFile(filepath.Join(directory, "unlisted.txt"), []byte("not part of the release"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, nil, "verify-release-artifacts.sh", "--directory", directory, "--repo", "owner/repo", "--tag", "v1.2.3", "--commit", strings.Repeat("a", 40))
	if err == nil || !bytes.Contains(output, []byte("release directory contains an unexpected file")) {
		t.Fatalf("unexpected bundle entry result=%v output=%s", err, output)
	}
}

func TestReleaseVerifierBindsAttestationIdentity(t *testing.T) {
	directory := writeReleaseEvidenceFixture(t)
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gh.log")
	fakeGH := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(fakeGH, []byte("#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> \"$AUTOGIT_GH_LOG\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AUTOGIT_GH_LOG=" + logPath,
	}, "verify-release-artifacts.sh", "--directory", directory, "--repo", "owner/repo", "--tag", "v1.2.3", "--commit", strings.Repeat("a", 40))
	if err != nil || !bytes.Contains(output, []byte("release checksums and provenance verified")) {
		t.Fatalf("valid release result=%v output=%s", err, output)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 9 {
		t.Fatalf("attestation calls=%d log=%s", len(lines), log)
	}
	for i, line := range lines[1:] {
		for _, required := range []string{
			"--repo owner/repo",
			"--signer-workflow owner/repo/.github/workflows/release.yml",
			"--source-ref refs/tags/v1.2.3",
			"--source-digest " + strings.Repeat("a", 40),
			"--deny-self-hosted-runners",
		} {
			if !strings.Contains(line, required) {
				t.Fatalf("attestation call missing %q: %s", required, line)
			}
		}
		if i < 6 && !strings.Contains(line, "--predicate-type https://slsa.dev/provenance/v1") {
			t.Fatalf("binary attestation call missing provenance predicate: %s", line)
		}
		if i == 6 && !strings.Contains(line, "--predicate-type https://spdx.dev/Document/v2.3") {
			t.Fatalf("SPDX attestation call missing SPDX predicate: %s", line)
		}
		if i == 7 && !strings.Contains(line, "--predicate-type https://cyclonedx.org/bom") {
			t.Fatalf("CycloneDX attestation call missing CycloneDX predicate: %s", line)
		}
	}
}

func TestReleaseVerifierStaysCompatibleWithStockMacOSBash(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(scriptsRoot(t), "verify-release-artifacts.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("declare -A")) {
		t.Fatal("release verifier must not require Bash 4 associative arrays; macOS ships Bash 3.2")
	}
}

func TestReleaseWorkflowRevalidatesAttestedBundleBeforePublication(t *testing.T) {
	workflowPath := filepath.Join(scriptsRoot(t), "..", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"sbom-path: dist/autogit.spdx.json",
		"sbom-path: dist/autogit.cyclonedx.json",
		"generate exact-tag machine evidence",
		"--require-tag",
		"autogit.release-evidence.json",
		"generate package channel metadata",
		"scripts/generate-package-metadata.sh",
		"package-metadata/autogit.rb",
		"package-metadata/autogit.json",
		"cmp release-bundle/package-metadata/autogit.rb",
		"cmp release-bundle/package-metadata/autogit.json",
		"attestations: read",
		"artifact-metadata: read",
		"test \"$(git rev-parse HEAD)\" = \"$RELEASE_SHA\"",
		"test -z \"$(git status --porcelain=v1)\"",
		"bash scripts/verify-release-artifacts.sh",
		"--verify-tag",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release workflow is missing %q", required)
		}
	}
}

func TestSecurityPolicyDefinesSeverityResponseTargets(t *testing.T) {
	securityPath := filepath.Join(scriptsRoot(t), "..", "SECURITY.md")
	data, err := os.ReadFile(securityPath)
	if err != nil {
		t.Fatal(err)
	}
	policy := string(data)
	for _, required := range []string{
		"| Critical | 72 hours after classification | 7 calendar days |",
		"| High | 7 calendar days after classification | 30 calendar days |",
		"| Medium | 30 calendar days after classification | 90 calendar days |",
		"| Low | Next planned maintenance cycle | 180 calendar days |",
		"private advisory",
	} {
		if !strings.Contains(policy, required) {
			t.Fatalf("security policy is missing %q", required)
		}
	}
}

func TestReleaseRollbackDrillProducesRedactedEvidence(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "rollback-drill.json")
	output, err := runShellScript(t, nil, "release-rollback-drill.sh", "--output", outputPath)
	if err != nil || len(output) != 0 {
		t.Fatalf("rollback drill result=%v output=%s", err, output)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`"schema_version": "autogit.release-rollback-drill/1"`,
		`"status": "passed"`,
		`"sensitive_data_recorded": false`,
		`"tampered_candidate_rejected": "passed"`,
		`"channel_pointer_rolled_back": "passed"`,
	} {
		if !bytes.Contains(data, []byte(required)) {
			t.Fatalf("rollback evidence missing %q: %s", required, data)
		}
	}
	if bytes.Contains(data, []byte(filepath.Dir(outputPath))) {
		t.Fatalf("rollback evidence leaked a local path: %s", data)
	}
}

func TestReleaseInstallDrillCoversNativeBinaryLifecycle(t *testing.T) {
	root := t.TempDir()
	previous := filepath.Join(root, "previous")
	candidate := filepath.Join(root, "candidate")
	build := func(path, version string) {
		t.Helper()
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-buildid= -X main.buildVersion="+version, "-o", path, "./cmd/autogit")
		cmd.Dir = filepath.Dir(scriptsRoot(t))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", version, err, output)
		}
	}
	build(previous, "v1.2.2")
	build(candidate, "v1.2.3")
	evidencePath := filepath.Join(root, "install-drill.json")
	output, err := runShellScript(t, nil, "release-install-drill.sh", "--previous", previous, "--candidate", candidate, "--output", evidencePath)
	if err != nil || len(output) != 0 {
		t.Fatalf("install drill result=%v output=%s", err, output)
	}
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		SchemaVersion         string            `json:"schema_version"`
		Status                string            `json:"status"`
		SensitiveDataRecorded bool              `json:"sensitive_data_recorded"`
		Checks                map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("install evidence JSON: %v", err)
	}
	if result.SchemaVersion != "autogit.release-install-drill/1" || result.Status != "passed" || result.SensitiveDataRecorded || len(result.Checks) != 6 {
		t.Fatalf("install evidence=%+v", result)
	}
	for check, status := range result.Checks {
		if status != "passed" {
			t.Fatalf("install check %s=%q", check, status)
		}
	}
	if bytes.Contains(data, []byte(root)) {
		t.Fatalf("install evidence leaked a local path: %s", data)
	}
}

func TestPackageMetadataGeneratorUsesReleaseManifest(t *testing.T) {
	dist := filepath.Join(t.TempDir(), "dist")
	if err := os.Mkdir(dist, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{
		"autogit-linux-amd64",
		"autogit-linux-arm64",
		"autogit-darwin-amd64",
		"autogit-darwin-arm64",
		"autogit-windows-amd64.exe",
		"autogit-windows-arm64.exe",
	}
	checksums := make(map[string]string, len(names))
	var manifest strings.Builder
	for _, name := range names {
		content := []byte("fixture binary: " + name)
		if err := os.WriteFile(filepath.Join(dist, name), content, 0700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		checksums[name] = fmt.Sprintf("%x", sum)
		manifest.WriteString(fmt.Sprintf("%s  %s\n", checksums[name], name))
	}
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(manifest.String()), 0600); err != nil {
		t.Fatal(err)
	}

	metadata := filepath.Join(t.TempDir(), "package-metadata")
	output, err := runShellScript(t, nil, "generate-package-metadata.sh",
		"--version", "v1.2.3",
		"--repo", "owner/repo",
		"--directory", dist,
		"--output", metadata,
	)
	if err != nil || len(output) != 0 {
		t.Fatalf("package metadata result=%v output=%s", err, output)
	}
	entries, err := os.ReadDir(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("package metadata entries=%d, want 2", len(entries))
	}
	formula, err := os.ReadFile(filepath.Join(metadata, "autogit.rb"))
	if err != nil {
		t.Fatal(err)
	}
	formulaText := string(formula)
	for _, required := range []string{
		"class Autogit < Formula",
		"version \"1.2.3\"",
		"https://github.com/owner/repo/releases/download/v1.2.3/autogit-linux-amd64",
		"https://github.com/owner/repo/releases/download/v1.2.3/autogit-darwin-arm64",
		"sha256 \"" + checksums["autogit-linux-amd64"] + "\"",
		"sha256 \"" + checksums["autogit-darwin-arm64"] + "\"",
		"bin.install \"autogit-darwin-arm64\" => \"autogit\"",
	} {
		if !strings.Contains(formulaText, required) {
			t.Fatalf("Homebrew formula is missing %q: %s", required, formulaText)
		}
	}

	manifestData, err := os.ReadFile(filepath.Join(metadata, "autogit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var scoop struct {
		Version      string `json:"version"`
		Homepage     string `json:"homepage"`
		License      string `json:"license"`
		Architecture map[string]struct {
			URL  string     `json:"url"`
			Hash string     `json:"hash"`
			Bin  [][]string `json:"bin"`
		} `json:"architecture"`
	}
	if err := json.Unmarshal(manifestData, &scoop); err != nil {
		t.Fatalf("Scoop manifest JSON: %v", err)
	}
	if scoop.Version != "1.2.3" || scoop.Homepage != "https://github.com/owner/repo" || scoop.License != "Apache-2.0" {
		t.Fatalf("Scoop metadata identity=%+v", scoop)
	}
	for architecture, name := range map[string]string{
		"64bit": "autogit-windows-amd64.exe",
		"arm64": "autogit-windows-arm64.exe",
	} {
		entry, ok := scoop.Architecture[architecture]
		if !ok || entry.Hash != checksums[name] || !strings.Contains(entry.URL, "/"+name) || len(entry.Bin) != 1 || len(entry.Bin[0]) != 2 || entry.Bin[0][0] != name || entry.Bin[0][1] != "autogit" {
			t.Fatalf("Scoop %s metadata=%+v", architecture, entry)
		}
	}
	if bytes.Contains(formula, []byte(filepath.Dir(dist))) || bytes.Contains(manifestData, []byte(filepath.Dir(dist))) {
		t.Fatal("package metadata leaked a local path")
	}

	output, err = runShellScript(t, nil, "generate-package-metadata.sh",
		"--version", "v1.2.3",
		"--repo", "owner/repo",
		"--directory", dist,
		"--output", metadata,
	)
	if err == nil || !bytes.Contains(output, []byte("output already exists")) {
		t.Fatalf("metadata overwrite result=%v output=%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(dist, names[0]), []byte("tampered binary"), 0700); err != nil {
		t.Fatal(err)
	}
	output, err = runShellScript(t, nil, "generate-package-metadata.sh",
		"--version", "v1.2.3",
		"--repo", "owner/repo",
		"--directory", dist,
		"--output", filepath.Join(t.TempDir(), "tampered-metadata"),
	)
	if err == nil || !bytes.Contains(output, []byte("checksum mismatch for "+names[0])) {
		t.Fatalf("metadata checksum result=%v output=%s", err, output)
	}
}

func TestPackageMetadataGeneratorUsesPowerShellHashOnWindowsShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shimmed Windows shell is only needed for non-Windows hosts")
	}
	dist := filepath.Join(t.TempDir(), "dist")
	if err := os.Mkdir(dist, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{
		"autogit-linux-amd64",
		"autogit-linux-arm64",
		"autogit-darwin-amd64",
		"autogit-darwin-arm64",
		"autogit-windows-amd64.exe",
		"autogit-windows-arm64.exe",
	}
	var manifest strings.Builder
	for _, name := range names {
		content := []byte("Windows shell fixture: " + name)
		if err := os.WriteFile(filepath.Join(dist, name), content, 0700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		manifest.WriteString(fmt.Sprintf("%x  %s\n", sum, name))
	}
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(manifest.String()), 0600); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	writeExecutable := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable("uname", "#!/usr/bin/env bash\nprintf '%s\\n' MSYS_NT-10.0\n")
	writeExecutable("cygpath", "#!/usr/bin/env bash\nprintf '%s\\n' \"$2\"\n")
	writeExecutable("powershell.exe", "#!/usr/bin/env bash\nsha256sum \"$AUTOGIT_HASH_PATH\" | awk '{print $1}'\n")
	metadata := filepath.Join(t.TempDir(), "package-metadata")
	env := []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}
	output, err := runShellScript(t, env, "generate-package-metadata.sh",
		"--version", "v1.2.3",
		"--repo", "owner/repo",
		"--directory", dist,
		"--output", metadata,
	)
	if err != nil || len(output) != 0 {
		t.Fatalf("Windows-shell package metadata result=%v output=%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(metadata, "autogit.rb")); err != nil {
		t.Fatal(err)
	}
}

func TestPackageMetadataGeneratorRejectsInvalidTag(t *testing.T) {
	output, err := runShellScript(t, nil, "generate-package-metadata.sh",
		"--version", "release-1.2.3",
		"--repo", "owner/repo",
		"--directory", t.TempDir(),
		"--output", filepath.Join(t.TempDir(), "package-metadata"),
	)
	if err == nil || !bytes.Contains(output, []byte("exact vMAJOR.MINOR.PATCH tag")) {
		t.Fatalf("invalid tag result=%v output=%s", err, output)
	}
}

var releaseEvidenceNames = []string{
	"autogit-linux-amd64",
	"autogit-linux-arm64",
	"autogit-darwin-amd64",
	"autogit-darwin-arm64",
	"autogit-windows-amd64.exe",
	"autogit-windows-arm64.exe",
	"autogit.spdx.json",
	"autogit.cyclonedx.json",
	"autogit.release-evidence.json",
	"autogit-linux-amd64.govulncheck.txt",
	"autogit-linux-arm64.govulncheck.txt",
	"autogit-darwin-amd64.govulncheck.txt",
	"autogit-darwin-arm64.govulncheck.txt",
	"autogit-windows-amd64.exe.govulncheck.txt",
	"autogit-windows-arm64.exe.govulncheck.txt",
}

func writeReleaseEvidenceFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	var manifest strings.Builder
	for _, name := range releaseEvidenceNames {
		content := []byte("release evidence: " + name)
		if err := os.WriteFile(filepath.Join(directory, name), content, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		manifest.WriteString(fmt.Sprintf("%x  %s\n", sum, name))
	}
	if err := os.WriteFile(filepath.Join(directory, "SHA256SUMS"), []byte(manifest.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestArtifactSmokeRejectsGitBelowCompatibilityFloor(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	gitScript := "#!/usr/bin/env bash\nif [[ \"$1\" == \"--version\" ]]; then echo 'git version 2.38.0'; exit 0; fi\nexec \"$AUTOGIT_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(fakeGit, []byte(gitScript), 0700); err != nil {
		t.Fatal(err)
	}
	fakeArtifact := filepath.Join(t.TempDir(), "autogit")
	if err := os.WriteFile(fakeArtifact, []byte("#!/usr/bin/env bash\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AUTOGIT_REAL_GIT=" + realGit,
	}, "artifact-smoke.sh", "--binary", fakeArtifact)
	if err == nil || !bytes.Contains(output, []byte("below supported minimum")) {
		t.Fatalf("old Git result=%v output=%s", err, output)
	}
}

func TestCanaryPreconditionsFailClosed(t *testing.T) {
	output, err := runShellScript(t, []string{
		"AUTOGIT_CANARY_OWNER=owner",
		"AUTOGIT_CANARY_TOKEN=token",
		"AUTOGIT_CANARY_VISIBILITY=public",
		"AUTOGIT_CANARY_RUN_ID=123",
	}, "github-canary.sh")
	if err == nil || !bytes.Contains(output, []byte("public canary requires")) {
		t.Fatalf("public consent result=%v output=%s", err, output)
	}
}

func TestPerformanceGateRejectsBadSampleCount(t *testing.T) {
	fakeBin := t.TempDir()
	fakeGo := filepath.Join(fakeBin, "go")
	content := "#!/usr/bin/env bash\nfor i in $(seq 1 19); do echo 'BenchmarkFake-1 1 1 ns/op'; done\n"
	if err := os.WriteFile(fakeGo, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}, "performance-gate.sh")
	if err == nil || !bytes.Contains(output, []byte("produced 19 samples")) {
		t.Fatalf("bad benchmark sample result=%v output=%s", err, output)
	}
}

func TestPerformanceGateRetriesTransientBudgetFailure(t *testing.T) {
	fakeBin := t.TempDir()
	counter := filepath.Join(t.TempDir(), "counter")
	fakeGo := filepath.Join(fakeBin, "go")
	content := "#!/usr/bin/env bash\ncount=0\nif [[ -f \"$AUTOGIT_TEST_COUNTER\" ]]; then count=$(<\"$AUTOGIT_TEST_COUNTER\"); fi\ncount=$((count + 1))\nprintf '%s\\n' \"$count\" > \"$AUTOGIT_TEST_COUNTER\"\nvalue=1\nif [[ \"$count\" -eq 1 ]]; then value=200000000; fi\nfor i in $(seq 1 20); do echo \"BenchmarkFake-1 1 $value ns/op\"; done\n"
	if err := os.WriteFile(fakeGo, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AUTOGIT_PERF_RETRIES=2",
		"AUTOGIT_TEST_COUNTER=" + counter,
	}, "performance-gate.sh")
	if err != nil || !bytes.Contains(output, []byte("retrying (1/2)")) {
		t.Fatalf("transient benchmark result=%v output=%s", err, output)
	}
}

func TestFuzzSuiteRejectsInsufficientExecutionBudget(t *testing.T) {
	fakeBin := t.TempDir()
	fakeGo := filepath.Join(fakeBin, "go")
	content := "#!/usr/bin/env bash\nprintf '%s\\n' 'fuzz: elapsed: 1s, execs: 1 (1/sec)'\n"
	if err := os.WriteFile(fakeGo, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := runShellScript(t, []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AUTOGIT_FUZZ_MIN_EXECS=100",
	}, "test-suites.sh", "fuzz")
	if err == nil || !bytes.Contains(output, []byte("executed 1 fuzz inputs; want at least 100")) {
		t.Fatalf("fuzz budget result=%v output=%s", err, output)
	}
}

func TestScenarioEvaluationGradesFinalRepositoryState(t *testing.T) {
	root := filepath.Dir(scriptsRoot(t))
	binary := filepath.Join(t.TempDir(), "autogit")
	trace := filepath.Join(t.TempDir(), "scenario-trace.json")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/autogit")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scenario binary: %v\n%s", err, output)
	}
	output, err := runShellScript(t, nil, "scenario-eval.sh", "--binary", binary, "--trace", trace)
	if err != nil || !bytes.Contains(output, []byte(`"scenario_count":20,"passed_count":20,"passed":true`)) {
		t.Fatalf("scenario evaluation result=%v output=%s", err, output)
	}
	traceData, err := os.ReadFile(trace)
	if err != nil || !bytes.Contains(traceData, []byte(`"schema_version":"autogit.scenario-trace/1"`)) || !bytes.Contains(traceData, []byte(`"passed_count":20`)) {
		t.Fatalf("scenario trace=%s err=%v", traceData, err)
	}
}
