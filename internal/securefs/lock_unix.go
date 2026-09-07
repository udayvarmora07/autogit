//go:build linux || darwin

package securefs

import (
	"os"

	"golang.org/x/sys/unix"
)

func openLockFilePlatform(root *os.Root, relative string) (*os.File, error) {
	return root.OpenFile(relative, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW, 0600)
}

func acquireFileLock(file *os.File) (func() error, error) {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return nil, err
	}
	return func() error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }, nil
}

func syncDirectory(root *os.Root, relative string) error {
	directory, err := root.Open(relative)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
