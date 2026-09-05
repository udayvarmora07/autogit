//go:build race

package state

// Race-instrumented subprocesses are substantially slower. The release
// stream retains the full 1,000 schedules; race CI samples every named point
// without exceeding Go's default per-package test timeout.
const randomizedProcessScheduleCount = 50
