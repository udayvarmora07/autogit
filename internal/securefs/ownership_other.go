//go:build !linux && !darwin && !windows

package securefs

import "os"

func OwnedByCurrentUser(os.FileInfo) bool { return false }
