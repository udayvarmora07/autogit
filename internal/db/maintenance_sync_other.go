//go:build !linux && !darwin

package db

// Windows does not support the Unix directory-fsync contract through the Go
// file API. The replacement path still uses an exclusive temporary file and
// native same-volume rename semantics; native Windows recovery remains part
// of the release matrix.
func syncMaintenanceDirectory(string) error { return nil }
