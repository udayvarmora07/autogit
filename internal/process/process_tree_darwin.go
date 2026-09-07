//go:build darwin

package process

func terminateDescendants(_ int) error { return nil }
