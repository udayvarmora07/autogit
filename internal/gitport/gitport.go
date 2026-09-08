package gitport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"autogit/internal/process"
)

type Result struct {
	Output    string
	Err       error
	Truncated bool
}
type Runner struct {
	Executable string
	MaxOutput  int
}

func (r Runner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("git context is required")
	}
	executable, err := canonicalExecutable(r.Executable)
	if err != nil {
		return Result{}, err
	}
	workingDir, err := canonicalWorkingDir(dir)
	if err != nil {
		return Result{}, err
	}
	if r.MaxOutput <= 0 {
		r.MaxOutput = 1 << 20
	}
	if isGitExecutable(executable) {
		args = safeGitArgs(args...)
	}
	result, runErr := process.Run(ctx, process.Options{Executable: executable, Dir: workingDir, Env: controlledEnvironment(), Args: args, MaxOutput: r.MaxOutput})
	if errors.Is(runErr, process.ErrOutputLimit) {
		runErr = io.ErrShortBuffer
	}
	return Result{Output: result.Output, Err: runErr, Truncated: result.Truncated}, runErr
}

func isGitExecutable(executable string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(executable)), ".exe")
	return base == "git"
}

func safeGitArgs(args ...string) []string {
	return append([]string{"-c", "core.hooksPath=", "-c", "core.fsmonitor=false", "-c", "core.sshCommand=", "-c", "credential.helper="}, args...)
}

func controlledEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+7)
	for _, item := range os.Environ() {
		key := item
		if at := strings.IndexByte(item, '='); at >= 0 {
			key = item[:at]
		}
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "SSH_") || key == "CDPATH" {
			continue
		}
		if upper == "PATH" || upper == "HOME" || upper == "TMPDIR" || upper == "SYSTEMROOT" || strings.HasPrefix(upper, "LANG") || strings.HasPrefix(upper, "LC_") {
			env = append(env, item)
		}
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	sort.Strings(env)
	return env
}

func canonicalExecutable(path string) (string, error) {
	if path == "" {
		path = "git"
	}
	resolved, err := exec.LookPath(path)
	if err != nil || !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("invalid Git executable")
	}
	canon, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid Git executable")
	}
	canon, err = filepath.Abs(filepath.Clean(canon))
	if err != nil {
		return "", fmt.Errorf("invalid Git executable")
	}
	info, err := os.Lstat(canon)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
		return "", fmt.Errorf("invalid Git executable")
	}
	return canon, nil
}

func canonicalWorkingDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("invalid Git working directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", errors.New("invalid Git working directory")
	}
	abs = filepath.Clean(abs)
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", errors.New("invalid Git working directory")
	}
	canonical := filepath.Join(parent, filepath.Base(abs))
	info, err := os.Lstat(canonical)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("invalid Git working directory")
	}
	return canonical, nil
}

var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var refRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var remoteRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func PushArgs(remote, sha, ref string) ([]string, error) {
	if remote == "" || !remoteRE.MatchString(remote) || !shaRE.MatchString(sha) || !refRE.MatchString(ref) || ref[0] == '-' {
		return nil, fmt.Errorf("invalid push destination")
	}
	if ref == "HEAD" || ref == "" {
		return nil, fmt.Errorf("invalid branch ref")
	}
	return []string{"push", "--", remote, sha + ":refs/heads/" + ref}, nil
}
