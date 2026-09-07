//go:build linux

package db

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// checkLocalFilesystem rejects filesystems whose network/cache semantics make
// SQLite WAL durability unsafe for AutoGit's multi-process state contract.
func checkLocalFilesystem(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(filepath.Clean(path), &stat); err != nil {
		return err
	}
	switch uint64(stat.Type) {
	case 0x6969, 0xFF534D42, 0xFE534D42, 0x65735546, 0x5346414F, 0x73757245: // NFS, CIFS, SMB2, FUSE, AFS, CODA
		return fmt.Errorf("unsupported network or user-space filesystem for SQLite state: %s", filepath.Clean(path))
	default:
		return nil
	}
}
