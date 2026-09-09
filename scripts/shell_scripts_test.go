package scripts_test

import (
	"bytes"
	"crypto/sha256"
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
	if len(lines) != 8 {
		t.Fatalf("attestation calls=%d log=%s", len(lines), log)
	}
	for _, line := range lines[1:] {
		for _, required := range []string{
			"--repo owner/repo",
			"--signer-workflow owner/repo/.github/workflows/release.yml",
			"--source-ref refs/tags/v1.2.3",
			"--source-digest " + strings.Repeat("a", 40),
			"--predicate-type https://slsa.dev/provenance/v1",
			"--deny-self-hosted-runners",
		} {
			if !strings.Contains(line, required) {
				t.Fatalf("attestation call missing %q: %s", required, line)
			}
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

var releaseEvidenceNames = []string{
	"autogit-linux-amd64",
	"autogit-linux-arm64",
	"autogit-darwin-amd64",
	"autogit-darwin-arm64",
	"autogit-windows-amd64.exe",
	"autogit-windows-arm64.exe",
	"autogit.spdx.json",
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
