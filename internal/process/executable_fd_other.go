//go:build !linux

package process

func executableFDPath() (string, error) { return "", ErrSandboxUnavailable }

func ExecutableBindingAvailable() bool { return false }
