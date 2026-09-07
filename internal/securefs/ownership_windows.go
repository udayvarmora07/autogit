//go:build windows

package securefs

import "os"

// Windows ownership is enforced by the ACL boundary rather than a POSIX uid.
// The caller still gets the symlink, traversal, and restrictive-mode checks.
func OwnedByCurrentUser(os.FileInfo) bool { return true }
