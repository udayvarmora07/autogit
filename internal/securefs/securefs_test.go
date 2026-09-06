package securefs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadWithinRejectsTraversalAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWithin(root, "../outside", 1024); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("traversal error = %v, want ErrUnsafePath", err)
	}
	if _, err := ReadWithin(root, "link", 1024); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink error = %v, want ErrUnsafePath", err)
	}
}

func TestAtomicWriteWithinUsesRestrictiveModeAndDoesNotFollowDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteWithin(root, "nested/policy.json", []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteWithin(root, "nested/policy.json", []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "nested/policy.json"))
	if err != nil || string(got) != "second" {
		t.Fatalf("written policy=%q err=%v", got, err)
	}
	if info, err := os.Stat(filepath.Join(root, "nested/policy.json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("policy mode=%v err=%v, want 600", info.Mode(), err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "nested/policy.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "nested/policy.json")); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteWithin(root, "nested/policy.json", []byte("must not replace target"), 0600); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("destination symlink error = %v, want ErrUnsafePath", err)
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "outside" {
		t.Fatalf("outside target changed: %q err=%v", content, err)
	}
}

func TestReadWithinRejectsSymlinkedParentAndEnforcesLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "policy.json"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWithin(root, "nested/policy.json", 1024); err == nil || !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlinked parent error = %v, want ErrUnsafePath", err)
	}
	if err := os.Remove(filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "policy.json"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWithin(root, "policy.json", 4); err == nil {
		t.Fatal("oversized state file was accepted")
	}
}
