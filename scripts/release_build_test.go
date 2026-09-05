//go:build release_integration

package scripts_test

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBuildProducesReproducibleTrimmedLinuxAMD64Binary(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	first := t.TempDir()
	second := t.TempDir()
	for _, output := range []string{first, second} {
		cmd := exec.Command("bash", "scripts/release-build.sh", "--target", "linux/amd64", "--output", output)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release build: %v\n%s", err, output)
		}
	}

	firstPath := filepath.Join(first, "autogit-linux-amd64")
	secondPath := filepath.Join(second, "autogit-linux-amd64")
	firstBinary, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondBinary, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBinary, secondBinary) {
		t.Fatal("release builds from identical inputs differ")
	}
	if bytes.Contains(firstBinary, []byte(repoRoot)) {
		t.Fatal("release binary contains its local source path")
	}
	info, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("release binary mode=%#o, want executable", info.Mode())
	}
	file, err := elf.Open(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.FileHeader.Machine != elf.EM_X86_64 {
		t.Fatalf("binary machine=%v, want %v", file.FileHeader.Machine, elf.EM_X86_64)
	}
}

func TestReleaseBuildNamesWindowsTargetAndBuildsItsArchitecture(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	cmd := exec.Command("bash", "scripts/release-build.sh", "--target", "windows/arm64", "--output", output)
	cmd.Dir = repoRoot
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release build: %v\n%s", err, result)
	}

	artifact := filepath.Join(output, "autogit-windows-arm64.exe")
	file, err := pe.Open(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_ARM64 {
		t.Fatalf("binary machine=%#x, want %#x", file.FileHeader.Machine, pe.IMAGE_FILE_MACHINE_ARM64)
	}
}

func TestReleaseBuildRejectsUnsupportedTargetBeforeBuilding(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "scripts/release-build.sh", "--target", "plan9/amd64", "--output", t.TempDir())
	cmd.Dir = repoRoot
	result, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("unsupported target succeeded: %s", result)
	}
	if !bytes.Contains(result, []byte("unsupported release target: plan9/amd64")) {
		t.Fatalf("unsupported target result=%q", result)
	}
}

func TestReleaseBuildDefaultsToEverySupportedReleaseTarget(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	cmd := exec.Command("bash", "scripts/release-build.sh", "--output", output)
	cmd.Dir = repoRoot
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release build: %v\n%s", err, result)
	}
	want := map[string]bool{
		"autogit-linux-amd64":       true,
		"autogit-linux-arm64":       true,
		"autogit-darwin-amd64":      true,
		"autogit-darwin-arm64":      true,
		"autogit-windows-amd64.exe": true,
		"autogit-windows-arm64.exe": true,
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want)+1 {
		t.Fatalf("release artifact count=%d, want %d", len(entries), len(want)+1)
	}
	for _, entry := range entries {
		if entry.IsDir() || (entry.Name() != "SHA256SUMS" && !want[entry.Name()]) {
			t.Fatalf("unexpected release artifact %q", entry.Name())
		}
	}
	checksums, err := os.ReadFile(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(checksums)), "\n")
	if len(lines) != len(want) {
		t.Fatalf("checksum count=%d, want %d", len(lines), len(want))
	}
	for _, line := range lines {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || !want[parts[1]] {
			t.Fatalf("invalid checksum line %q", line)
		}
		binary, err := os.ReadFile(filepath.Join(output, parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(binary)
		if parts[0] != hex.EncodeToString(sum[:]) {
			t.Fatalf("checksum for %s=%q", parts[1], parts[0])
		}
	}
}
