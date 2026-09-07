//go:build !linux && !darwin && !windows

package process

func applyResourceLimits(_ int, _ ResourceLimits) error { return nil }
