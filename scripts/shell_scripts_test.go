package scripts_test

import (
	"bytes"
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

func TestScenarioEvaluationGradesFinalRepositoryState(t *testing.T) {
	root := filepath.Dir(scriptsRoot(t))
	binary := filepath.Join(t.TempDir(), "autogit")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/autogit")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scenario binary: %v\n%s", err, output)
	}
	output, err := runShellScript(t, nil, "scenario-eval.sh", "--binary", binary)
	if err != nil || !bytes.Contains(output, []byte(`"passed":true`)) {
		t.Fatalf("scenario evaluation result=%v output=%s", err, output)
	}
}
