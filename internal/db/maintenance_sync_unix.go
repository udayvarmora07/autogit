//go:build linux || darwin

package db

import (
	"os"
	"path/filepath"
)

// syncMaintenanceDirectory makes an atomic maintenance rename durable in
// the directory entry as well as in the already-synced database file.
func syncMaintenanceDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
