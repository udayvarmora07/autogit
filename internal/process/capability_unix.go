//go:build linux || darwin

package process

// ProcessBoundedAvailable reports whether this platform has the process-group
// supervisor used for process-bounded execution.
func ProcessBoundedAvailable() bool { return true }
