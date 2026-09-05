package repository

import (
	"bytes"
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

type hygienePlan struct {
	Gitignore hygieneFilePlan
	Readme    hygieneFilePlan
	Missing   []string
}

type hygieneFilePlan struct {
	FilePath string
	Original []byte
	Desired  []byte
	Mode     os.FileMode
	Exists   bool
}

var initialBranchRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// ResolveUninitializedRoot canonicalizes an explicit directory and proves it
// is not an existing or nested Git repository before mutation.
func ResolveUninitializedRoot(candidate string) (string, error) {
	root, err := canonicalProjectRoot(candidate)
	if err != nil {
		return "", err
	}
	if looksLikeBareRepository(root) {
		return "", errors.New("target is a bare git repository")
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

// A bare repository has no .git directory, so the normal ancestor check is
// insufficient. This conservative fingerprint rejects the Git object-store
// layout before any initialization command can reconfigure it.
func looksLikeBareRepository(root string) bool {
	for _, name := range []string{"HEAD", "config", "description"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return false
		}
	}
	for _, name := range []string{"objects", "refs"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false
		}
	}
	return true
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
	hygiene, err := prepareHygiene(canonical)
	if err != nil {
		return InitResult{}, err
	}
	if _, err := runner.Run(ctx, canonical, "init", "--initial-branch="+branch); err != nil {
		return InitResult{}, fmt.Errorf("git init: %w", err)
	}
	if err := applyHygiene(hygiene); err != nil {
		return InitResult{}, err
	}
	return InitResult{Root: canonical, Branch: branch, Hygiene: hygiene.Missing, Initialized: true}, nil
}

// PlanInitialization performs the complete read-only initialization preflight
// and reports the hygiene entries that would be added.
func PlanInitialization(root, branch string) (InitResult, error) {
	canonical, err := ResolveUninitializedRoot(root)
	if err != nil {
		return InitResult{}, err
	}
	if err := ValidateInitialBranch(branch); err != nil {
		return InitResult{}, err
	}
	plan, err := prepareHygiene(canonical)
	if err != nil {
		return InitResult{}, err
	}
	return InitResult{Root: canonical, Branch: branch, Hygiene: plan.Missing}, nil
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
	if root == string(filepath.Separator) || (home != "" && samePath(root, home)) {
		return "", errors.New("protected project root")
	}
	return root, nil
}

func mergeHygiene(root string) ([]string, error) {
	plan, err := prepareHygiene(root)
	if err != nil {
		return nil, err
	}
	if err := applyHygiene(plan); err != nil {
		return nil, err
	}
	return plan.Missing, nil
}

func prepareHygiene(root string) (hygienePlan, error) {
	ignorePath := filepath.Join(root, ".gitignore")
	ignore := hygieneFilePlan{FilePath: ignorePath, Mode: 0600}
	if info, err := os.Lstat(ignorePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return hygienePlan{}, errors.New(".gitignore must be a regular file")
		}
		if info.Size() > maxGeneratedHygieneBytes {
			return hygienePlan{}, errors.New(".gitignore exceeds size limit")
		}
		ignore.Original, err = os.ReadFile(ignorePath)
		if err != nil {
			return hygienePlan{}, err
		}
		ignore.Mode = info.Mode().Perm()
		ignore.Exists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return hygienePlan{}, err
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

	text := string(ignore.Original)
	missing := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !gitignoreContains(text, entry) {
			missing = append(missing, entry)
		}
	}
	ignore.Desired = append([]byte(nil), ignore.Original...)
	if len(missing) > 0 {
		var block strings.Builder
		if len(ignore.Original) > 0 && !strings.HasSuffix(text, "\n") {
			block.WriteByte('\n')
		}
		block.WriteString("\n# BEGIN AUTOGIT MANAGED\n")
		for _, entry := range missing {
			block.WriteString(entry)
			block.WriteByte('\n')
		}
		block.WriteString("# END AUTOGIT MANAGED\n")
		ignore.Desired = append(ignore.Desired, []byte(block.String())...)
	}
	if len(ignore.Desired) > maxGeneratedHygieneBytes {
		return hygienePlan{}, errors.New("generated .gitignore exceeds size limit")
	}

	readmePath := filepath.Join(root, "README.md")
	readme := hygieneFilePlan{FilePath: readmePath, Mode: 0600}
	if info, err := os.Lstat(readmePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return hygienePlan{}, errors.New("README.md must be a regular file")
		}
		if info.Size() > maxGeneratedHygieneBytes {
			return hygienePlan{}, errors.New("README.md exceeds size limit")
		}
		readme.Original, err = os.ReadFile(readmePath)
		if err != nil {
			return hygienePlan{}, err
		}
		readme.Desired = append([]byte(nil), readme.Original...)
		readme.Mode = info.Mode().Perm()
		readme.Exists = true
	} else if errors.Is(err, os.ErrNotExist) {
		name := strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return '-'
			}
			return r
		}, filepath.Base(root))
		readme.Desired = []byte("# " + name + "\n\nDescribe this project.\n")
		missing = append(missing, "README.md")
	} else {
		return hygienePlan{}, err
	}
	return hygienePlan{Gitignore: ignore, Readme: readme, Missing: missing}, nil
}

func applyHygiene(plan hygienePlan) error {
	if err := applyHygieneFile(plan.Gitignore); err != nil {
		return err
	}
	return applyHygieneFile(plan.Readme)
}

func applyHygieneFile(plan hygieneFilePlan) error {
	if bytes.Equal(plan.Original, plan.Desired) {
		return nil
	}
	info, statErr := os.Lstat(plan.FilePath)
	if plan.Exists {
		if statErr != nil {
			return fmt.Errorf("hygiene file changed during initialization: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("hygiene file changed to a non-regular file")
		}
		current, readErr := os.ReadFile(plan.FilePath)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(current, plan.Original) {
			return errors.New("hygiene file changed during initialization")
		}
	} else if statErr == nil {
		return errors.New("hygiene file appeared during initialization")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	tmp, err := os.CreateTemp(filepath.Dir(plan.FilePath), ".autogit-hygiene-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(plan.Mode); err == nil {
		_, err = tmp.Write(plan.Desired)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, plan.FilePath); err != nil {
		return err
	}
	return nil
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
