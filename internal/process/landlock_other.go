//go:build !linux

package process

import "errors"

// LandlockABI is unavailable outside Linux. The explicit error keeps callers
// from confusing an unsupported probe with an enforced fallback.
func LandlockABI() (int, error) {
	return 0, errors.New("Landlock is unavailable on this platform")
}

func LandlockAvailable() bool { return false }

func enforceLandlock([]string, bool) error {
	return errors.New("Landlock is unavailable on this platform")
}
