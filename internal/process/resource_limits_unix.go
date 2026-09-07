//go:build linux

package process

import (
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

func applyResourceLimits(pid int, limits ResourceLimits) error {
	apply := func(resource int, value uint64) error {
		if value == 0 {
			return nil
		}
		limit := &unix.Rlimit{Cur: value, Max: value}
		return unix.Prlimit(pid, resource, limit, nil)
	}
	var cpu uint64
	if limits.CPUTime > 0 {
		seconds := limits.CPUTime.Seconds()
		if seconds > float64(math.MaxUint64) {
			return errors.New("CPU resource limit is too large")
		}
		cpu = uint64(math.Ceil(seconds))
	}
	if err := apply(unix.RLIMIT_CPU, cpu); err != nil {
		return err
	}
	if err := apply(unix.RLIMIT_AS, limits.MemoryBytes); err != nil {
		return err
	}
	if err := apply(unix.RLIMIT_FSIZE, limits.FileBytes); err != nil {
		return err
	}
	if err := apply(unix.RLIMIT_NPROC, limits.Processes); err != nil {
		return err
	}
	return nil
}

func platformValidateResourceLimits(_ ResourceLimits) error { return nil }
