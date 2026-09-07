//go:build !linux && !darwin && !windows

package db

func checkLocalFilesystem(string) error { return nil }
