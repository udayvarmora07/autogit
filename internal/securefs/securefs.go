// Package securefs contains small, state-root filesystem operations with
// traversal, symlink, ownership-mode, and atomic-replacement checks.
package securefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultReadLimit = int64(16 << 20)

var ErrUnsafePath = errors.New("unsafe state filesystem path")

// ReadWithin opens a file through an os.Root handle. The handle keeps access
// anchored to the originally opened state directory if that directory is
// renamed, and prevents a relative path from escaping the root.
func ReadWithin(root, relative string, max int64) ([]byte, error) {
	relative, err := cleanRelative(relative)
	if err != nil {
		return nil, err
	}
	stateRoot, absolute, err := openStateRoot(root, false)
	if err != nil {
		return nil, err
	}
	defer stateRoot.Close()
	if err := verifyRelativePath(stateRoot, relative, false); err != nil {
		return nil, err
	}
	if err := verifyPrivateRootFile(stateRoot, relative); err != nil {
		return nil, err
	}
	if err := verifyPrivateRootDirectory(stateRoot, "."); err != nil {
		return nil, err
	}
	file, err := stateRoot.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimited(file, max, absolute)
}

// ReadPath is for an explicitly supplied file outside a state root, such as
// an operator-selected verifier registry. It still rejects final symlinks,
// non-regular files, broad file modes, and over-limit input.
func ReadPath(path string, max int64) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: path is empty", ErrUnsafePath)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(absolute)
	if err := verifyPathAncestors(parent); err != nil {
		return nil, err
	}
	canonicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, err
	}
	if err := verifyExistingDirectory(canonicalParent); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(canonicalParent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(absolute)
	if err := verifyRelativePath(root, name, false); err != nil {
		return nil, err
	}
	if err := verifyPrivateRootFile(root, name); err != nil {
		return nil, err
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimited(file, max, absolute)
}

// AtomicWriteWithin replaces a state file through the same root handle used
// for validation. The temporary file is created with O_EXCL, synced, and
// renamed within the root, so a destination symlink is replaced rather than
// followed and cannot redirect the write outside the state tree.
func AtomicWriteWithin(root, relative string, data []byte, mode fs.FileMode) error {
	relative, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	stateRoot, absolute, err := openStateRoot(root, true)
	if err != nil {
		return err
	}
	defer stateRoot.Close()
	if err := ensurePrivateParents(stateRoot, absolute, relative); err != nil {
		return err
	}
	if err := verifyDestination(stateRoot, relative); err != nil {
		return err
	}
	tmp, tmpRelative, err := createTemp(stateRoot, filepath.Dir(relative), ".autogit-write-", mode)
	if err != nil {
		return err
	}
	defer stateRoot.Remove(tmpRelative)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := stateRoot.Rename(tmpRelative, relative); err != nil {
		return err
	}
	return verifyPrivateRootFile(stateRoot, relative)
}

// WriteExclusiveWithin creates a state file exactly once, with no-follow
// semantics provided by O_EXCL and the root-relative handle.
func WriteExclusiveWithin(root, relative string, data []byte, mode fs.FileMode) error {
	relative, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	stateRoot, absolute, err := openStateRoot(root, true)
	if err != nil {
		return err
	}
	defer stateRoot.Close()
	if err := ensurePrivateParents(stateRoot, absolute, relative); err != nil {
		return err
	}
	file, err := stateRoot.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// EnsurePrivateRoot creates a state directory when needed and tightens an
// existing directory through its open directory handle. It rejects symlinked
// ancestors before creation and avoids chmod-by-path replacement races.
func EnsurePrivateRoot(root string) error {
	stateRoot, _, err := openStateRoot(root, true)
	if err != nil {
		return err
	}
	file, err := stateRoot.Open(".")
	if err != nil {
		_ = stateRoot.Close()
		return err
	}
	if runtime.GOOS != "windows" {
		if err := file.Chmod(0700); err != nil {
			_ = file.Close()
			_ = stateRoot.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		_ = stateRoot.Close()
		return err
	}
	if err := verifyPrivateRootDirectory(stateRoot, "."); err != nil {
		_ = stateRoot.Close()
		return err
	}
	return stateRoot.Close()
}

// CheckPrivateRoot validates an existing state directory without creating or
// changing it.
func CheckPrivateRoot(root string) error {
	stateRoot, _, err := openStateRoot(root, false)
	if err != nil {
		return err
	}
	defer stateRoot.Close()
	return verifyPrivateRootDirectory(stateRoot, ".")
}

func openStateRoot(root string, createParents bool) (*os.Root, string, error) {
	if root == "" || strings.ContainsRune(root, 0) {
		return nil, "", fmt.Errorf("%w: root is empty or contains NUL", ErrUnsafePath)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}
	absolute = filepath.Clean(absolute)
	if err := verifyPathAncestors(absolute); err != nil {
		return nil, "", err
	}
	anchorPath, err := nearestExistingAncestor(absolute)
	if err != nil {
		return nil, "", err
	}
	anchor, err := os.OpenRoot(anchorPath)
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(anchorPath, absolute)
	if err != nil || filepath.IsAbs(relative) || startsParent(relative) {
		_ = anchor.Close()
		return nil, "", fmt.Errorf("%w: state root is outside its anchor", ErrUnsafePath)
	}
	stateRoot := anchor
	if relative != "." {
		if createParents {
			if err := anchor.MkdirAll(relative, 0700); err != nil {
				_ = anchor.Close()
				return nil, "", err
			}
		}
		if err := verifyRelativePath(anchor, relative, false); err != nil {
			_ = anchor.Close()
			return nil, "", err
		}
		stateRoot, err = anchor.OpenRoot(relative)
		if err != nil {
			_ = anchor.Close()
			return nil, "", err
		}
		if err := anchor.Close(); err != nil {
			_ = stateRoot.Close()
			return nil, "", err
		}
	}
	if err := verifyRootDirectory(stateRoot, "."); err != nil {
		_ = stateRoot.Close()
		return nil, "", err
	}
	return stateRoot, absolute, nil
}

func verifyPathAncestors(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("%w: state-root ancestor is unsafe", ErrUnsafePath)
				}
				// A regular file can only be the requested root itself; the
				// caller will report that it is not a directory. An existing
				// non-directory ancestor makes the path invalid now.
				if current != filepath.Clean(path) {
					return fmt.Errorf("%w: state-root ancestor is not a directory", ErrUnsafePath)
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func nearestExistingAncestor(path string) (string, error) {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", fmt.Errorf("%w: state-root anchor is unsafe", ErrUnsafePath)
			}
			return current, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
	}
}

func cleanRelative(relative string) (string, error) {
	if relative == "" || strings.ContainsRune(relative, 0) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: path must be relative to a state root", ErrUnsafePath)
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || startsParent(clean) {
		return "", fmt.Errorf("%w: path escapes state root", ErrUnsafePath)
	}
	return clean, nil
}

func startsParent(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func ensurePrivateParents(root *os.Root, absolute, relative string) error {
	parent := filepath.Dir(relative)
	if parent != "." {
		if err := root.MkdirAll(parent, 0700); err != nil {
			return err
		}
	}
	if err := verifyPrivateRootDirectory(root, "."); err != nil {
		return err
	}
	if err := verifyRelativePath(root, parent, true); err != nil {
		return err
	}
	parentAbsolute := filepath.Dir(filepath.Join(absolute, relative))
	return verifyPrivateDirectory(parentAbsolute)
}

func verifyRelativePath(root *os.Root, relative string, allowMissingFinal bool) error {
	if relative == "." {
		return nil
	}
	parts := strings.Split(relative, string(filepath.Separator))
	current := ""
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) && allowMissingFinal && index == len(parts)-1 {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink component %q", ErrUnsafePath, current)
		}
		if index != len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("%w: path component %q is not a directory", ErrUnsafePath, current)
		}
		if info.IsDir() {
			if !OwnedByCurrentUser(info) {
				return fmt.Errorf("%w: directory component %q is not owned by the current user", ErrUnsafePath, current)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
				return fmt.Errorf("%w: directory component %q permissions are too broad", ErrUnsafePath, current)
			}
		}
	}
	return nil
}

func verifyDestination(root *os.Root, relative string) error {
	info, err := root.Lstat(relative)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: destination is not a regular file", ErrUnsafePath)
	}
	if !OwnedByCurrentUser(info) {
		return fmt.Errorf("%w: destination is not owned by the current user", ErrUnsafePath)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: destination permissions are too broad", ErrUnsafePath)
	}
	return nil
}

func createTemp(root *os.Root, parent, prefix string, mode fs.FileMode) (*os.File, string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		var token [8]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, "", err
		}
		name := prefix + hex.EncodeToString(token[:])
		relative := name
		if parent != "." {
			relative = filepath.Join(parent, name)
		}
		file, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if runtime.GOOS != "windows" {
			if err := file.Chmod(mode.Perm()); err != nil {
				_ = file.Close()
				_ = root.Remove(relative)
				return nil, "", err
			}
		}
		return file, relative, nil
	}
	return nil, "", errors.New("could not create a unique temporary state file")
}

func readLimited(file *os.File, max int64, path string) ([]byte, error) {
	if max <= 0 {
		max = defaultReadLimit
	}
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s exceeds input limit", path)
	}
	return data, nil
}

func verifyExistingDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: directory is not a real directory", ErrUnsafePath)
	}
	if !OwnedByCurrentUser(info) {
		return fmt.Errorf("%w: directory is not owned by the current user", ErrUnsafePath)
	}
	return nil
}

func verifyPrivateDirectory(path string) error {
	if err := verifyExistingDirectory(path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("%w: directory permissions are too broad", ErrUnsafePath)
		}
	}
	return nil
}

func verifyPrivateRootDirectory(root *os.Root, relative string) error {
	if err := verifyRootDirectory(root, relative); err != nil {
		return err
	}
	info, err := root.Lstat(relative)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: directory permissions are too broad", ErrUnsafePath)
	}
	return nil
}

func verifyRootDirectory(root *os.Root, relative string) error {
	info, err := root.Lstat(relative)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: directory is not a real directory", ErrUnsafePath)
	}
	if !OwnedByCurrentUser(info) {
		return fmt.Errorf("%w: directory is not owned by the current user", ErrUnsafePath)
	}
	return nil
}

func verifyPrivateRootFile(root *os.Root, relative string) error {
	info, err := root.Lstat(relative)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: file is not a regular non-symlink", ErrUnsafePath)
	}
	if !OwnedByCurrentUser(info) {
		return fmt.Errorf("%w: file is not owned by the current user", ErrUnsafePath)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: file permissions are too broad", ErrUnsafePath)
	}
	return nil
}
