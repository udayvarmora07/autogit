//go:build linux || darwin

package securefs

import (
	"os"
	"syscall"
)

// OwnedByCurrentUser reports whether a filesystem object belongs to the
// account running AutoGit.
func OwnedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	uid := os.Getuid()
	return ok && uid >= 0 && uint64(stat.Uid) == uint64(uid)
}
