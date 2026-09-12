//go:build !windows

package process

func AppContainerAvailable() bool { return false }
