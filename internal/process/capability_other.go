//go:build !linux && !darwin && !windows

package process

func ProcessBoundedAvailable() bool { return false }
