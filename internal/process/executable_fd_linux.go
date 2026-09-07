//go:build linux

package process

import "os"

func executableFDPath() (string, error) { return "/proc/self/fd/3", nil }

func ExecutableBindingAvailable() bool {
	info, err := os.Stat("/proc/self/fd")
	return err == nil && info.IsDir()
}
