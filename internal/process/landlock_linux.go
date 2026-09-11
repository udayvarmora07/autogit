//go:build linux

package process

import (
	"golang.org/x/sys/unix"
)

// LandlockABI probes kernel support without creating or installing a ruleset.
// A successful version query only proves that the kernel exposes Landlock; it
// does not claim that a verifier has been restricted with a ruleset.
func LandlockABI() (int, error) {
	version, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0,
		0,
		unix.LANDLOCK_CREATE_RULESET_VERSION,
		0,
		0,
		0,
	)
	if errno != 0 {
		return 0, errno
	}
	return int(version), nil
}
