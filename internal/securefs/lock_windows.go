//go:build windows

package securefs

import (
	"os"

	"golang.org/x/sys/windows"
)

func openLockFilePlatform(root *os.Root, relative string) (*os.File, error) {
	return root.OpenFile(relative, os.O_RDWR|os.O_CREATE, 0600)
}

func acquireFileLock(file *os.File) (func() error, error) {
	overlapped := new(windows.Overlapped)
	handle := windows.Handle(file.Fd())
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		return nil, err
	}
	return func() error { return windows.UnlockFileEx(handle, 0, 1, 0, overlapped) }, nil
}

func syncDirectory(*os.Root, string) error { return nil }
