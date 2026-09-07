//go:build !linux && !darwin && !windows

package securefs

import "os"

func openLockFilePlatform(root *os.Root, relative string) (*os.File, error) {
	return root.OpenFile(relative, os.O_RDWR|os.O_CREATE, 0600)
}

func acquireFileLock(*os.File) (func() error, error) { return func() error { return nil }, nil }
func syncDirectory(*os.Root, string) error           { return nil }
