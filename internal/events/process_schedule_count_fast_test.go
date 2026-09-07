//go:build !soak && !race && !windows

package events

// Keep a representative sample in presubmit while reserving the full matrix
// for the explicit soak suite.
const randomizedProcessScheduleCount = 50
