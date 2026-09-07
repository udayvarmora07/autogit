//go:build darwin

package process

// macOS keeps the process-bounded tier honest with process-group termination,
// timeout, and output ceilings. Resource ceilings require a future sandbox
// boundary; they are not silently emulated with a shell or inherited ulimit.
func applyResourceLimits(_ int, _ ResourceLimits) error { return nil }
