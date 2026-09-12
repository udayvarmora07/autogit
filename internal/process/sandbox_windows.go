//go:build windows

package process

import (
	"errors"
	"os/exec"
	"path/filepath"
)

func sandboxExecutable() (string, error) { return "", ErrSandboxUnavailable }

func NamespaceSandboxAvailable() bool { return AppContainerAvailable() }

func prepareSandbox(_ *exec.Cmd, options Options) error {
	requested := len(options.FilesystemAllowlist) != 0 || options.NetworkDisabled
	if options.AppContainer {
		if !AppContainerAvailable() {
			return ErrSandboxUnavailable
		}
		if !requested || len(options.FilesystemAllowlist) == 0 {
			return errors.New("AppContainer requires an explicit filesystem allowlist")
		}
		if options.ExecutableFile != nil {
			return errors.New("AppContainer cannot use descriptor-bound execution on Windows")
		}
		if options.Env == nil {
			return errors.New("AppContainer requires an explicit environment")
		}
		if options.Dir == "" || !filepath.IsAbs(options.Dir) || filepath.Clean(options.Dir) != options.Dir {
			return errors.New("AppContainer requires an absolute clean working directory")
		}
		return nil
	}
	if requested {
		return ErrSandboxUnavailable
	}
	return nil
}
