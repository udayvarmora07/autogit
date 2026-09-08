package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectArtifactsHashesFilesWithoutRecordingLocalPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "autogit-linux-amd64")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("artifact bytes\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}

	got, err := collectArtifacts([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("artifacts=%+v", got)
	}
	wantSum := sha256.Sum256(content)
	if got[0].Name != "autogit-linux-amd64" || got[0].SHA256 != hex.EncodeToString(wantSum[:]) || got[0].Size != int64(len(content)) {
		t.Fatalf("artifact=%+v", got[0])
	}
	if strings.Contains(got[0].Name, root) {
		t.Fatalf("artifact leaked local path: %+v", got[0])
	}
}

func TestCollectArtifactsRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "autogit-linux-amd64")
	if err := os.WriteFile(target, []byte("artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := collectArtifacts([]string{link}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink result=%v", err)
	}
}

func TestStringFlagsRejectsEmptyValues(t *testing.T) {
	var flags stringFlags
	if err := flags.Set(" "); err == nil {
		t.Fatal("empty evidence flag was accepted")
	}
	if err := flags.Set("artifact"); err != nil || flags.String() != "artifact" {
		t.Fatalf("flags=%q err=%v", flags.String(), err)
	}
}
