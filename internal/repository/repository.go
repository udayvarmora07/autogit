package repository

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"autogit/internal/gitport"
)

type Info struct{ Root, CommonDir, RepoID, WorktreeID string }

func Discover(candidate string) (Info, error) {
	return DiscoverWithKey(candidate, []byte("autogit-development-identity-key"))
}

func DiscoverContext(ctx context.Context, candidate string) (Info, error) {
	return DiscoverWithKeyContext(ctx, candidate, []byte("autogit-development-identity-key"))
}

// DiscoverWithKey derives non-reversible repository identities. Production
// callers should supply the per-installation key held in protected state.
func DiscoverWithKey(candidate string, key []byte) (Info, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return DiscoverWithKeyContext(ctx, candidate, key)
}

// DiscoverWithKeyContext derives repository identities while honoring the
// caller's operation budget. External Git validation inherits this context.
func DiscoverWithKeyContext(ctx context.Context, candidate string, key []byte) (Info, error) {
	if ctx == nil {
		return Info{}, errors.New("repository context is required")
	}
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
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
	if root == string(filepath.Separator) || samePath(root, home) {
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
	if root == string(filepath.Separator) || samePath(root, home) {
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
		if b, readErr := os.ReadFile(filepath.Join(common, "commondir")); readErr == nil { // #nosec G703 -- common is resolved Git metadata and the target is constrained below.
			cd := strings.TrimSpace(string(b))
			if cd != "" {
				if !filepath.IsAbs(cd) {
					cd = filepath.Join(common, cd)
				}
				if resolved, evalErr := filepath.EvalSymlinks(cd); evalErr == nil {
					if !containsPath(resolved, linkedGitDir) {
						return Info{}, errors.New("linked worktree commondir escapes Git metadata")
					}
					common = resolved
				} else {
					return Info{}, evalErr
				}
			}
		}
		if err := verifyGitfileBinding(ctx, root, gitPath, linkedGitDir); err != nil {
			return Info{}, err
		}
	}
	repoID := digest(key, "repo", common)
	workID := digest(key, "worktree", root)
	return Info{Root: root, CommonDir: common, RepoID: repoID, WorktreeID: workID}, nil
}

// verifyGitfileBinding proves that the requested directory is the worktree the
// git directory records, so a checkout cannot alias another local repository's
// identity and policy scope (TM-027). Git records the binding for a `.git`
// file in exactly one of two ways: a linked worktree's `gitdir` back-pointer
// file inside the per-worktree git directory, or core.worktree for git
// directories that carry their worktree path (submodules, and separate git
// directories configured with an explicit worktree). Metadata with neither
// binding is rejected instead of trusted, which covers git directories from
// `git init --separate-git-dir` without a recorded worktree, a manually moved
// worktree whose back-pointer is stale (`git worktree repair` fixes that),
// and every hostile alias that points at another repository's git directory.
func verifyGitfileBinding(ctx context.Context, root, gitFile, gitDir string) error {
	git, err := gitExecutable()
	if err != nil {
		return errors.New("git executable is unavailable")
	}
	effectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	runner := gitport.Runner{Executable: git}
	result, runErr := runner.Run(effectCtx, root, "-C", root, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir")
	if runErr != nil || result.Err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := effectCtx.Err(); err != nil {
			return err
		}
		return errors.New("invalid linked worktree metadata")
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 2 {
		return errors.New("invalid linked worktree metadata")
	}
	actualRoot, err := filepath.EvalSymlinks(lines[0])
	if err != nil || !samePath(actualRoot, root) {
		return errors.New("linked worktree root mismatch")
	}
	actualGit, err := filepath.EvalSymlinks(lines[1])
	if err != nil || !samePath(actualGit, gitDir) {
		return errors.New("linked worktree gitdir mismatch")
	}
	if pointer, readErr := os.ReadFile(filepath.Join(gitDir, "gitdir")); readErr == nil { // #nosec G304 G703 -- gitDir is resolved Git metadata and the target is constrained by the binding comparison below.
		if registered, err := resolveGitMetadataPath(gitDir, strings.TrimSpace(string(pointer))); err != nil || !samePath(registered, gitFile) {
			return errors.New("linked worktree back-pointer does not cover this directory; run 'git worktree repair' and retry")
		}
		return nil
	}
	configResult, configErr := runner.Run(effectCtx, root, "-C", root, "config", "--no-includes", "--file", filepath.Join(gitDir, "config"), "--path", "--get", "core.worktree")
	if configErr == nil && configResult.Err == nil {
		if recorded, err := resolveGitMetadataPath(gitDir, strings.TrimSpace(configResult.Output)); err != nil || !samePath(recorded, root) {
			return errors.New("git metadata worktree path does not cover this directory")
		}
		return nil
	}
	return errors.New("repository git metadata does not declare this directory as its worktree")
}

// resolveGitMetadataPath resolves a path recorded inside Git metadata (a
// worktree back-pointer or a core.worktree entry). Relative values are
// relative to the git directory that records them.
func resolveGitMetadataPath(gitDir, recorded string) (string, error) {
	if recorded == "" {
		return "", errors.New("empty git metadata path")
	}
	if !filepath.IsAbs(recorded) {
		recorded = filepath.Join(gitDir, recorded)
	}
	return filepath.EvalSymlinks(recorded)
}

func gitExecutable() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	return path, nil
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return left == right
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func digest(key []byte, kind, value string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(kind + "\x00" + value))
	return "hmac-sha256:" + hex.EncodeToString(h.Sum(nil))
}
