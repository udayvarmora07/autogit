//go:build soak && !race && !windows

package coordinator

// The full process-boundary matrix is run by the explicit soak suite.
const processScheduleCount = 1000
