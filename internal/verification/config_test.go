package verification

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func verifierConfigJSON(t *testing.T, specs []map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"version": "1", "verifiers": specs})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func validVerifierConfigSpec(t *testing.T, name string) map[string]any {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"name": name, "version": "1", "argv": []string{exe}, "applicable": true, "timeout_ms": 5000, "max_output": 4096}
}

func TestLoadRegistryRejectsUnknownFieldsAndMalformedVersion(t *testing.T) {
	spec := validVerifierConfigSpec(t, "lint")
	spec["unexpected"] = true
	if _, err := LoadRegistry(verifierConfigJSON(t, []map[string]any{spec}), 1<<20); err == nil {
		t.Fatal("unknown verifier field accepted")
	}
	if _, err := LoadRegistry([]byte(`{"version":"2","verifiers":[]}`), 1<<20); err == nil {
		t.Fatal("unknown config version accepted")
	}
	if _, err := LoadRegistry([]byte(`{"version":"1","version":"1","verifiers":[]}`), 1<<20); err == nil {
		t.Fatal("duplicate top-level config key accepted")
	}
	if _, err := LoadRegistry([]byte(`{"version":"1","verifiers":[{"name":"x","name":"x","version":"1","argv":["/bin/true"]}]}`), 1<<20); err == nil {
		t.Fatal("duplicate verifier key accepted")
	}
}

func TestLoadRegistryFreezesSortedSpecsAndStableDigest(t *testing.T) {
	a := validVerifierConfigSpec(t, "z-test")
	b := validVerifierConfigSpec(t, "a-lint")
	first, err := LoadRegistry(verifierConfigJSON(t, []map[string]any{a, b}), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadRegistry(verifierConfigJSON(t, []map[string]any{b, a}), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigDigest != second.ConfigDigest || first.VerifierSetDigest != second.VerifierSetDigest || len(first.Specs) != 2 || first.Specs[0].Name != "a-lint" {
		t.Fatalf("registry ordering/digest unstable: first=%+v second=%+v", first, second)
	}
	a["name"] = "changed"
	if first.Specs[1].Name != "z-test" {
		t.Fatal("registry retained mutable config input")
	}
}

func TestLoadRegistryEnforcesInputAndVerifierLimits(t *testing.T) {
	valid := verifierConfigJSON(t, []map[string]any{validVerifierConfigSpec(t, "lint")})
	if _, err := LoadRegistry(valid, int64(len(valid)-1)); err == nil {
		t.Fatal("oversized config accepted")
	}
	large := validVerifierConfigSpec(t, "lint")
	large["timeout_ms"] = int64(25 * 60 * 1000)
	if _, err := LoadRegistry(verifierConfigJSON(t, []map[string]any{large}), 1<<20); err == nil {
		t.Fatal("unbounded timeout accepted")
	}
	large["timeout_ms"] = int64(1<<63 - 1)
	if _, err := LoadRegistry(verifierConfigJSON(t, []map[string]any{large}), 1<<20); err == nil {
		t.Fatal("overflowing timeout accepted")
	}
	large["timeout_ms"] = 5000
	large["max_output"] = 0
	if _, err := LoadRegistry(verifierConfigJSON(t, []map[string]any{large}), 1<<20); err == nil {
		t.Fatal("invalid output limit accepted")
	}
	if _, err := LoadRegistry([]byte(strings.Repeat("x", 100)), 0); err == nil {
		t.Fatal("zero input limit accepted")
	}
}

func TestLoadRegistryFileReadsBoundedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verifiers.json")
	if err := os.WriteFile(path, verifierConfigJSON(t, []map[string]any{validVerifierConfigSpec(t, "lint")}), 0600); err != nil {
		t.Fatal(err)
	}
	registry, err := LoadRegistryFile(path, 1<<20)
	if err != nil || registry == nil || registry.Specs[0].Name != "lint" {
		t.Fatalf("registry=%+v err=%v", registry, err)
	}
}

func TestLoadRegistryFileRejectsSymlinkedConfiguration(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.json")
	linkPath := filepath.Join(dir, "link.json")
	if err := os.WriteFile(realPath, verifierConfigJSON(t, nil), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := LoadRegistryFile(linkPath, 1<<20); err == nil {
		t.Fatal("symlinked verifier configuration accepted")
	}
}

func TestLoadRegistryFileRejectsBroadUnixPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission policy does not apply on Windows")
	}
	path := filepath.Join(t.TempDir(), "broad.json")
	if err := os.WriteFile(path, verifierConfigJSON(t, nil), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistryFile(path, 1<<20); err == nil {
		t.Fatal("broad-permission verifier configuration accepted")
	}
}
