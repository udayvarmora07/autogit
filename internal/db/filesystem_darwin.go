//go:build darwin

package db

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func checkLocalFilesystem(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(filepath.Clean(path), &stat); err != nil {
		return err
	}
	name := strings.TrimRight(string(stat.Fstypename[:]), "\x00")
	switch strings.ToLower(name) {
	case "nfs", "smbfs", "webdav", "afpfs":
		return fmt.Errorf("unsupported network filesystem for SQLite state: %s", name)
	default:
		return nil
	}
}
