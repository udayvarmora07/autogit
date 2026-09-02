package repository

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Info struct{ Root, CommonDir, RepoID, WorktreeID string }

func Discover(candidate string) (Info, error) {
	return DiscoverWithKey(candidate, []byte("autogit-development-identity-key"))
}

// DiscoverWithKey derives non-reversible repository identities. Production
// callers should supply the per-installation key held in protected state.
func DiscoverWithKey(candidate string, key []byte) (Info, error) {
	if len(key) == 0 {
		return Info{}, errors.New("identity key is required")
	}
	if candidate == "" {
		return Info{}, errors.New("repository path is required")
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return Info{}, err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Info{}, fmt.Errorf("invalid repository path: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Info{}, err
	}
	home, _ := os.UserHomeDir()
	home, _ = filepath.EvalSymlinks(home)
	if root == string(filepath.Separator) || root == home {
		return Info{}, errors.New("protected repository root")
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return Info{}, errors.New("repository root is not a directory")
	}
	// Resolve a client cwd to the nearest canonical Git top-level. Never use
	// the process cwd as an implicit fallback.
	gitPath := filepath.Join(root, ".git")
	for {
		if _, statErr := os.Lstat(gitPath); statErr == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return Info{}, errors.New("not a git repository")
		}
		root = parent
		gitPath = filepath.Join(root, ".git")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Info{}, err
	}
	if root == string(filepath.Separator) || root == home {
		return Info{}, errors.New("protected repository root")
	}
	gst, err := os.Lstat(gitPath)
	if err != nil {
		return Info{}, errors.New("not a git repository")
	}
	if gst.Mode()&os.ModeSymlink != 0 {
		return Info{}, errors.New("git metadata symlink is unsafe")
	}
	common := gitPath
	if !gst.IsDir() {
		b, err := os.ReadFile(gitPath)
		if err != nil {
			return Info{}, err
		}
		line := strings.TrimSpace(string(b))
		if !strings.HasPrefix(line, "gitdir:") {
			return Info{}, errors.New("invalid worktree metadata")
		}
		gd := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if !filepath.IsAbs(gd) {
			gd = filepath.Join(root, gd)
		}
		common, err = filepath.EvalSymlinks(gd)
		if err != nil {
			return Info{}, err
		}
		linkedGitDir := common
		// Linked worktrees point at $COMMON/worktrees/<name>. Resolve their
		// commondir marker so repository identity is shared while worktree
		// identity remains distinct.
		if b, readErr := os.ReadFile(filepath.Join(common, "commondir")); readErr == nil {
			cd := strings.TrimSpace(string(b))
			if cd != "" {
				if !filepath.IsAbs(cd) {
					cd = filepath.Join(common, cd)
				}
				if resolved, evalErr := filepath.EvalSymlinks(cd); evalErr == nil {
					common = resolved
				} else {
					return Info{}, evalErr
				}
			}
		}
		if err := verifyLinkedWorktree(root, linkedGitDir); err != nil {
			return Info{}, err
		}
	}
	repoID := digest(key, "repo", common)
	workID := digest(key, "worktree", root)
	return Info{Root: root, CommonDir: common, RepoID: repoID, WorktreeID: workID}, nil
}

func verifyLinkedWorktree(root, expectedGitDir string) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return errors.New("Git executable is unavailable")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(git))
	if err != nil {
		return errors.New("Git executable is invalid")
	}
	git = filepath.Join(parent, filepath.Base(git))
	if info, statErr := os.Lstat(git); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("Git executable is invalid")
	}
	cmd := exec.Command(git, "-C", root, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir")
	cmd.Env = []string{"PATH=" + filepath.Dir(git), "HOME=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
	out, err := cmd.Output()
	if err != nil {
		return errors.New("invalid linked worktree metadata")
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return errors.New("invalid linked worktree metadata")
	}
	actualRoot, err := filepath.EvalSymlinks(lines[0])
	if err != nil || actualRoot != root {
		return errors.New("linked worktree root mismatch")
	}
	actualGit, err := filepath.EvalSymlinks(lines[1])
	if err != nil || actualGit != expectedGitDir {
		return errors.New("linked worktree gitdir mismatch")
	}
	return nil
}
func digest(key []byte, kind, value string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(kind + "\x00" + value))
	return "hmac-sha256:" + hex.EncodeToString(h.Sum(nil))
}
