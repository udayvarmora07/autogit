//go:build windows

package process

func applyResourceLimits(_ int, _ ResourceLimits) error { return nil }
