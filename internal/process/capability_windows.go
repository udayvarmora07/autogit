//go:build windows

package process

import "golang.org/x/sys/windows"

// ProcessBoundedAvailable probes the Job Object primitive used for process
// cleanup and resource ceilings. It deliberately does not probe AppContainer,
// because a Job Object is not a filesystem or network sandbox.
func ProcessBoundedAvailable() bool {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(job)
	return true
}
