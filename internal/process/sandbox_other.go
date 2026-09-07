//go:build !linux

package process

import "os/exec"

func sandboxExecutable() (string, error) { return "", ErrSandboxUnavailable }

func NamespaceSandboxAvailable() bool { return false }

func prepareSandbox(_ *exec.Cmd, options Options) error {
	if len(options.FilesystemAllowlist) != 0 || options.NetworkDisabled {
		return ErrSandboxUnavailable
	}
	return nil
}
