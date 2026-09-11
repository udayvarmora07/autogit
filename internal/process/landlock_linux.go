//go:build linux

package process

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const landlockHelperArgument = "--autogit-landlock-helper"

const (
	landlockHelperNetworkDisabled = "--network-disabled"
	landlockHelperPathCount       = "--path-count"
	landlockHelperSeparator       = "--"
)

// LandlockABI probes kernel support without creating or installing a ruleset.
// A successful version query proves that the kernel exposes the API used by
// the child wrapper below; it does not by itself restrict the caller.
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

// LandlockAvailable reports whether the kernel accepts a Landlock ruleset.
// The actual ruleset is installed only in the short-lived child immediately
// before it replaces its image with the requested executable.
func LandlockAvailable() bool {
	version, err := LandlockABI()
	return err == nil && version >= 1
}

type landlockRulesetAttr struct {
	handledAccessFS  uint64
	handledAccessNet uint64
}

const landlockRulesetAttrSize = uintptr(16)

// enforceLandlock installs a read/execute-only filesystem policy for the
// supplied hierarchies. The paths must be canonical paths visible from the
// current mount namespace. An empty rule set is rejected because Landlock is
// deny-by-default for every handled access.
func enforceLandlock(paths []string, networkDisabled bool) error {
	if len(paths) == 0 {
		return errors.New("landlock requires at least one allowed path")
	}
	abi, err := LandlockABI()
	if err != nil {
		return fmt.Errorf("landlock ABI: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs for landlock: %w", err)
	}
	attr := landlockRulesetAttr{
		handledAccessFS: landlockFilesystemAccessMask(abi),
	}
	if networkDisabled && abi >= 4 {
		attr.handledAccessNet = unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
	}
	rulesetFD, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), // #nosec G103 -- the pointer is a fixed-size syscall ABI struct with a call-scoped lifetime.
		landlockRulesetAttrSize,
		0,
		0,
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("create landlock ruleset: %w", errno)
	}
	defer unix.Close(int(rulesetFD))

	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("landlock path is not absolute and clean: %q", path)
		}
		pathInfo, statErr := os.Stat(path) // #nosec G703 -- paths are produced by the canonical sandbox boundary or fixed helper arguments.
		if statErr != nil || (!pathInfo.IsDir() && !pathInfo.Mode().IsRegular()) {
			return fmt.Errorf("landlock path is not a regular file or directory: %q", path)
		}
		allowedAccess := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE)
		if pathInfo.IsDir() {
			allowedAccess |= unix.LANDLOCK_ACCESS_FS_READ_DIR
		}
		pathFD, openErr := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return fmt.Errorf("open landlock path %q: %w", path, openErr)
		}
		if pathFD < 0 || uint64(pathFD) > uint64(^uint32(0)) {
			_ = unix.Close(pathFD)
			return fmt.Errorf("landlock path descriptor is not representable: %q", path)
		}
		// landlock_path_beneath_attr is packed in the kernel ABI (8-byte
		// access mask followed by a 4-byte descriptor), so do not pass a Go
		// struct whose trailing alignment could change the syscall size.
		pathRule := make([]byte, 12)
		binary.LittleEndian.PutUint64(pathRule[0:8], allowedAccess)
		binary.LittleEndian.PutUint32(pathRule[8:12], uint32(pathFD))
		_, _, addErrno := unix.Syscall6(
			unix.SYS_LANDLOCK_ADD_RULE,
			rulesetFD,
			unix.LANDLOCK_RULE_PATH_BENEATH,
			uintptr(unsafe.Pointer(&pathRule[0])), // #nosec G103 -- the pointer is a packed kernel ABI buffer with a fixed 12-byte lifetime.
			0,
			0,
			0,
		)
		closeErr := unix.Close(pathFD)
		if addErrno != 0 {
			return fmt.Errorf("add landlock path %q: %w", path, addErrno)
		}
		if closeErr != nil {
			return fmt.Errorf("close landlock path %q: %w", path, closeErr)
		}
	}
	_, _, restrictErrno := unix.Syscall6(
		unix.SYS_LANDLOCK_RESTRICT_SELF,
		rulesetFD,
		0,
		0,
		0,
		0,
		0,
	)
	if restrictErrno != 0 {
		return fmt.Errorf("restrict process with landlock: %w", restrictErrno)
	}
	return nil
}

func landlockFilesystemAccessMask(abi int) uint64 {
	mask := uint64(
		unix.LANDLOCK_ACCESS_FS_EXECUTE |
			unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
			unix.LANDLOCK_ACCESS_FS_READ_FILE |
			unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
			unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
			unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
			unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG |
			unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
			unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
			unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
			unix.LANDLOCK_ACCESS_FS_MAKE_SYM,
	)
	if abi >= 2 {
		mask |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		mask |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	return mask
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != landlockHelperArgument {
		return
	}
	if err := runLandlockHelper(os.Args[2:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "autogit landlock helper: %v\n", err)
		os.Exit(126)
	}
}

func runLandlockHelper(arguments []string) error {
	networkDisabled := false
	if len(arguments) > 0 && arguments[0] == landlockHelperNetworkDisabled {
		networkDisabled = true
		arguments = arguments[1:]
	}
	if len(arguments) < 2 || arguments[0] != landlockHelperPathCount {
		return errors.New("invalid Landlock helper arguments")
	}
	pathCount, err := strconv.Atoi(arguments[1])
	if err != nil || pathCount <= 0 || pathCount > 1024 {
		return errors.New("invalid Landlock helper path count")
	}
	pathStart := 2
	pathEnd := pathStart + pathCount
	if pathEnd >= len(arguments) || arguments[pathEnd] != landlockHelperSeparator || pathEnd+1 >= len(arguments) {
		return errors.New("invalid Landlock helper path list")
	}
	paths := arguments[pathStart:pathEnd]
	targetArguments := arguments[pathEnd+1:]
	target := targetArguments[0]
	if target != "/autogit-executable" && (!filepath.IsAbs(target) || filepath.Clean(target) != target) {
		return errors.New("invalid Landlock helper target")
	}
	if err := enforceLandlock(paths, networkDisabled); err != nil {
		return err
	}
	return syscall.Exec(target, targetArguments, os.Environ()) // #nosec G702 G204 -- target is an absolute, clean path parsed after the fixed helper separator; argv is never interpreted by a shell.
}
