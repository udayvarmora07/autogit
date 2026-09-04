package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"autogit/internal/gitport"
)

const maxGeneratedHygieneBytes = 1 << 20

// InitRunner is the narrow Git mutation port required by repository
// initialization. The caller must establish consent and policy before it is
// invoked.
type InitRunner interface {
	Run(context.Context, string, ...string) (gitport.Result, error)
}

type InitResult struct {
	Root        string
	Branch      string
	Hygiene     []string
	Initialized bool
}

var initialBranchRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// ResolveUninitializedRoot canonicalizes an explicit directory and proves it
// is not an existing or nested Git repository before mutation.
func ResolveUninitializedRoot(candidate string) (string, error) {
	root, err := canonicalProjectRoot(candidate)
	if err != nil {
		return "", err
	}
	for current := root; ; current = filepath.Dir(current) {
		gitPath := filepath.Join(current, ".git")
		if info, statErr := os.Lstat(gitPath); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("git metadata symlink is unsafe")
			}
			return "", errors.New("target is already inside a git repository")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return root, nil
}

// FutureRepositoryID returns the identity that DiscoverWithKey will derive
// after a normal `git init` creates root/.git. It lets the CLI persist consent
// before Git initialization, satisfying the consent-before-mutation rule.
func FutureRepositoryID(root string, key []byte) (string, error) {
	canonical, err := canonicalProjectRoot(root)
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", errors.New("identity key is required")
	}
	return digest(key, "repo", filepath.Join(canonical, ".git")), nil
}

// Initialize creates Git metadata with an explicit initial branch and then
// merges a small, ecosystem-derived hygiene block. It never creates a commit,
// stages work, creates a remote, or overwrites existing user bytes.
func Initialize(ctx context.Context, runner InitRunner, root, branch string) (InitResult, error) {
	if ctx == nil || runner == nil {
		return InitResult{}, errors.New("initialization dependencies are missing")
	}
	canonical, err := ResolveUninitializedRoot(root)
	if err != nil {
		return InitResult{}, err
	}
	if err := ValidateInitialBranch(branch); err != nil {
		return InitResult{}, err
	}
	if _, err := runner.Run(ctx, canonical, "init", "--initial-branch="+branch); err != nil {
		return InitResult{}, fmt.Errorf("git init: %w", err)
	}
	hygiene, err := mergeHygiene(canonical)
	if err != nil {
		return InitResult{}, err
	}
	return InitResult{Root: canonical, Branch: branch, Hygiene: hygiene, Initialized: true}, nil
}

// ValidateInitialBranch checks the branch name before any initialization
// side effect occurs.
func ValidateInitialBranch(branch string) error {
	return validateInitialBranch(branch)
}

func validateInitialBranch(branch string) error {
	if branch == "" || !initialBranchRE.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.Contains(branch, "//") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, ".lock") || strings.ContainsAny(branch, "\\\r\n\t") {
		return errors.New("invalid initial branch")
	}
	return nil
}

func canonicalProjectRoot(candidate string) (string, error) {
	if candidate == "" {
		return "", errors.New("project path is required")
	}
	abs, err := filepath.Abs(candidate)
	if err != nil || filepath.Clean(abs) != abs {
		return "", errors.New("invalid project path")
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("invalid project path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("project root must be a regular directory")
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("invalid project path: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	if resolved, evalErr := filepath.EvalSymlinks(home); evalErr == nil {
		home = resolved
	}
	if root == string(filepath.Separator) || (home != "" && root == home) {
		return "", errors.New("protected project root")
	}
	return root, nil
}

func mergeHygiene(root string) ([]string, error) {
	path := filepath.Join(root, ".gitignore")
	var original []byte
	mode := os.FileMode(0600)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New(".gitignore must be a regular file")
		}
		if info.Size() > maxGeneratedHygieneBytes {
			return nil, errors.New(".gitignore exceeds size limit")
		}
		original, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	entries := []string{".env", ".env.*", ".DS_Store"}
	if existsRegular(filepath.Join(root, "package.json")) {
		entries = append(entries, "node_modules/")
	}
	if existsRegular(filepath.Join(root, "pyproject.toml")) || existsRegular(filepath.Join(root, "requirements.txt")) {
		entries = append(entries, ".venv/", "__pycache__/")
	}
	if existsRegular(filepath.Join(root, "go.mod")) {
		entries = append(entries, "/bin/")
	}

	text := string(original)
	missing := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !gitignoreContains(text, entry) {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	var block strings.Builder
	if len(original) > 0 && !strings.HasSuffix(text, "\n") {
		block.WriteByte('\n')
	}
	block.WriteString("\n# BEGIN AUTOGIT MANAGED\n")
	for _, entry := range missing {
		block.WriteString(entry)
		block.WriteByte('\n')
	}
	block.WriteString("# END AUTOGIT MANAGED\n")
	desired := append(append([]byte(nil), original...), []byte(block.String())...)
	if len(desired) > maxGeneratedHygieneBytes {
		return nil, errors.New("generated .gitignore exceeds size limit")
	}
	tmp, err := os.CreateTemp(root, ".autogit-gitignore-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(desired)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return nil, err
	}
	return missing, nil
}

func existsRegular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func gitignoreContains(content, entry string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}
