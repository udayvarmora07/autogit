//go:build soak && !race && !windows

package events

// The 1,000-schedule matrix is intentionally opt-in. Presubmit and merge
// suites use the representative fast count; the soak suite runs this file.
const randomizedProcessScheduleCount = 1000
