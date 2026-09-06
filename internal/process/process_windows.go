//go:build windows

package process

import (
	"errors"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

type supervisor struct{ job windows.Handle }

func newSupervisor(*exec.Cmd) (*supervisor, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &supervisor{job: job}, nil
}

func (s *supervisor) Attach(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return errors.New("process did not start")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.AssignProcessToJobObject(s.job, handle)
}

func (s *supervisor) Terminate(command *exec.Cmd) error {
	if s != nil && s.job != 0 {
		return windows.TerminateJobObject(s.job, 1)
	}
	if command != nil && command.Process != nil {
		return command.Process.Kill()
	}
	return nil
}

func (s *supervisor) Close() error {
	if s == nil || s.job == 0 {
		return nil
	}
	err := windows.CloseHandle(s.job)
	s.job = 0
	return err
}
