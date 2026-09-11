//go:build linux

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// sandboxExecutable returns the canonical bubblewrap executable. Bubblewrap
// supplies Linux user/pid/network namespaces and read-only bind mounts; it is
// itself only a launcher and is never selected as a verifier executable.
func sandboxExecutable() (string, error) {
	for _, candidate := range []string{"/usr/bin/bwrap", "/bin/bwrap"} {
		path, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		path, err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("%w: trusted bubblewrap is not installed", ErrSandboxUnavailable)
}

func NamespaceSandboxAvailable() bool {
	bwrap, err := sandboxExecutable()
	if err != nil {
		return false
	}
	if _, err := sandboxPrlimitExecutable(); err != nil {
		return false
	}
	probe := exec.Command(bwrap, "--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--disable-userns", "--assert-userns-disabled", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--clearenv", "--", "/bin/true") // #nosec G204 -- bwrap is canonicalized and the probe argv is fixed.
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
	landlockEnabled := LandlockAvailable()
	paths, err := canonicalSandboxPaths(options, landlockEnabled)
	if err != nil {
		return err
	}
	landlockPaths := append([]string(nil), paths...)
	if landlockEnabled {
		for _, path := range append(sandboxRuntimePaths(), "/tmp", "/dev") {
			if _, statErr := os.Stat(path); statErr != nil {
				continue
			}
			alreadyIncluded := false
			for _, included := range landlockPaths {
				if included == path {
					alreadyIncluded = true
					break
				}
			}
			if !alreadyIncluded {
				landlockPaths = append(landlockPaths, path)
			}
		}
		if options.ExecutableFile != nil {
			landlockPaths = append(landlockPaths, "/autogit-executable")
		}
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--disable-userns", "--assert-userns-disabled"}
	if options.NetworkDisabled {
		args = append(args, "--unshare-net")
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	// These are immutable runtime dependencies, not user data. The candidate
	// and every caller-supplied allowlisted path still remain explicit.
	for _, runtimePath := range sandboxRuntimePaths() {
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
	if landlockEnabled {
		helper, helperErr := sandboxHelperExecutable()
		if helperErr != nil {
			return helperErr
		}
		helperArgs := []string{helper, landlockHelperArgument}
		if options.NetworkDisabled {
			helperArgs = append(helperArgs, landlockHelperNetworkDisabled)
		}
		helperArgs = append(helperArgs, landlockHelperPathCount, strconv.Itoa(len(landlockPaths)))
		helperArgs = append(helperArgs, landlockPaths...)
		helperArgs = append(helperArgs, landlockHelperSeparator)
		helperArgs = append(helperArgs, target...)
		target = helperArgs
	}
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
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && stat.Uid == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: Linux prlimit is not installed", ErrSandboxUnavailable)
}

func sandboxRuntimePaths() []string {
	return []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}
}

func sandboxHelperExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate Landlock helper executable: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve Landlock helper executable: %w", err)
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("canonicalize Landlock helper executable: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("landlock helper executable is not a regular executable: %s", path)
	}
	return path, nil
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

func canonicalSandboxPaths(options Options, includeHelper bool) ([]string, error) {
	paths := append([]string(nil), options.FilesystemAllowlist...)
	paths = append(paths, options.Dir)
	if options.ExecutableFile == nil {
		paths = append(paths, options.Executable)
	}
	if includeHelper {
		helper, err := sandboxHelperExecutable()
		if err != nil {
			return nil, err
		}
		paths = append(paths, helper)
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
