//go:build linux

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// sandboxExecutable returns the canonical bubblewrap executable. Bubblewrap
// supplies Linux user/pid/network namespaces and read-only bind mounts; it is
// itself only a launcher and is never selected as a verifier executable.
func sandboxExecutable() (string, error) {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return "", fmt.Errorf("%w: bubblewrap is not installed", ErrSandboxUnavailable)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: bubblewrap path is not trusted", ErrSandboxUnavailable)
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("%w: bubblewrap path is not trusted", ErrSandboxUnavailable)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("%w: bubblewrap is not a regular executable", ErrSandboxUnavailable)
	}
	return path, nil
}

func NamespaceSandboxAvailable() bool {
	bwrap, err := sandboxExecutable()
	if err != nil {
		return false
	}
	if _, err := sandboxPrlimitExecutable(); err != nil {
		return false
	}
	probe := exec.Command(bwrap, "--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--clearenv", "--", "/bin/true") // #nosec G204 -- bwrap is canonicalized and the probe argv is fixed.
	probe.Dir = string(filepath.Separator)
	probe.Env = []string{}
	return probe.Run() == nil
}

func prepareSandbox(command *exec.Cmd, options Options) error {
	if len(options.FilesystemAllowlist) == 0 && !options.NetworkDisabled {
		return nil
	}
	if command == nil {
		return errors.New("sandbox command is required")
	}
	if options.Executable == "" || !filepath.IsAbs(options.Executable) || filepath.Clean(options.Executable) != options.Executable {
		return errors.New("sandbox executable must be an absolute clean path")
	}
	bwrap, err := sandboxExecutable()
	if err != nil {
		return err
	}
	if options.Dir == "" || !filepath.IsAbs(options.Dir) || filepath.Clean(options.Dir) != options.Dir {
		return errors.New("sandbox working directory must be an absolute clean path")
	}
	paths, err := canonicalSandboxPaths(options)
	if err != nil {
		return err
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid"}
	if options.NetworkDisabled {
		args = append(args, "--unshare-net")
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	// These are immutable runtime dependencies, not user data. The candidate
	// and every caller-supplied allowlisted path still remain explicit.
	for _, runtimePath := range []string{"/usr", "/bin", "/lib", "/lib64", "/etc"} {
		if _, statErr := os.Stat(runtimePath); statErr == nil {
			args = append(args, "--ro-bind", runtimePath, runtimePath)
		}
	}
	for _, path := range paths {
		for _, parent := range sandboxParents(path) {
			args = append(args, "--dir", parent)
		}
		args = append(args, "--ro-bind", path, path)
	}
	if options.ExecutableFile != nil {
		args = append(args, "--ro-bind-fd", "3", "/autogit-executable")
	}
	args = append(args, "--chdir", options.Dir, "--clearenv")
	for _, value := range options.Env {
		key, envValue, ok := strings.Cut(value, "=")
		if !ok || key == "" || strings.ContainsAny(key, "\x00\r\n=") || strings.ContainsAny(envValue, "\x00\r\n") {
			return errors.New("invalid sandbox environment")
		}
		args = append(args, "--setenv", key, envValue)
	}
	targetExecutable := options.Executable
	if options.ExecutableFile != nil {
		targetExecutable = "/autogit-executable"
	}
	target := []string{targetExecutable}
	target = append(target, options.Args...)
	if limits := sandboxResourceLimitArgs(options.Limits); len(limits) > 0 {
		prlimit, limitErr := sandboxPrlimitExecutable()
		if limitErr != nil {
			return limitErr
		}
		target = append([]string{prlimit}, append(limits, append([]string{"--"}, target...)...)...)
	}
	args = append(args, "--")
	args = append(args, target...)
	command.Path = bwrap
	command.Args = append([]string{bwrap}, args...)
	command.Dir = string(filepath.Separator)
	// Do not let bubblewrap inherit the caller's environment; all values for
	// the target are reconstructed explicitly through --setenv above.
	command.Env = []string{}
	return nil
}

func sandboxPrlimitExecutable() (string, error) {
	for _, candidate := range []string{"/usr/bin/prlimit", "/bin/prlimit"} {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: Linux prlimit is not installed", ErrSandboxUnavailable)
}

func sandboxResourceLimitArgs(limits ResourceLimits) []string {
	args := make([]string, 0, 4)
	if limits.CPUTime > 0 {
		seconds := int64(limits.CPUTime / 1e9)
		if limits.CPUTime%1e9 != 0 {
			seconds++
		}
		args = append(args, fmt.Sprintf("--cpu=%d:%d", seconds, seconds))
	}
	if limits.MemoryBytes > 0 {
		args = append(args, fmt.Sprintf("--as=%d:%d", limits.MemoryBytes, limits.MemoryBytes))
	}
	if limits.FileBytes > 0 {
		args = append(args, fmt.Sprintf("--fsize=%d:%d", limits.FileBytes, limits.FileBytes))
	}
	if limits.Processes > 0 {
		args = append(args, fmt.Sprintf("--nproc=%d:%d", limits.Processes, limits.Processes))
	}
	return args
}

func canonicalSandboxPaths(options Options) ([]string, error) {
	paths := append([]string(nil), options.FilesystemAllowlist...)
	paths = append(paths, options.Dir)
	if options.ExecutableFile == nil {
		paths = append(paths, options.Executable)
	}
	seen := make(map[string]bool, len(paths))
	canonical := make([]string, 0, len(paths))
	for _, raw := range paths {
		if raw == "" || !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
			return nil, errors.New("sandbox allowlist path must be absolute and clean")
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(raw))
		if err != nil {
			return nil, fmt.Errorf("sandbox allowlist parent: %w", err)
		}
		candidate := filepath.Join(parent, filepath.Base(raw))
		info, err := os.Lstat(candidate)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return nil, errors.New("sandbox allowlist path is not a regular file or directory")
		}
		if !seen[candidate] {
			seen[candidate] = true
			canonical = append(canonical, candidate)
		}
	}
	sort.Strings(canonical)
	return canonical, nil
}

func sandboxParents(path string) []string {
	parents := make([]string, 0, 4)
	for current := filepath.Dir(path); current != string(filepath.Separator); current = filepath.Dir(current) {
		parents = append(parents, current)
	}
	for i, j := 0, len(parents)-1; i < j; i, j = i+1, j-1 {
		parents[i], parents[j] = parents[j], parents[i]
	}
	return parents
}
