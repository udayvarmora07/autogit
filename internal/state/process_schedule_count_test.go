//go:build soak && !race && !windows

package state

// The 1,000-schedule matrix is intentionally opt-in.
const randomizedProcessScheduleCount = 1000
