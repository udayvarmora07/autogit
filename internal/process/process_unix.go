//go:build linux || darwin

package process

import (
	"os/exec"
	"syscall"
)

type supervisor struct{}

func newSupervisor(command *exec.Cmd) (*supervisor, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &supervisor{}, nil
}

func (*supervisor) Attach(*exec.Cmd) error { return nil }

func (*supervisor) Terminate(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return command.Process.Kill()
}

func (*supervisor) Close() error { return nil }
