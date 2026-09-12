//go:build !windows

package process

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

func startAppContainer(_ *exec.Cmd, _ io.Writer, _ io.Writer, _ Options) (appContainerProcess, error) {
	return nil, ErrSandboxUnavailable
}

func attestProcess(_ *os.Process, _ string) (IsolationAttestation, error) {
	return IsolationAttestation{}, errors.New("AppContainer attestation is unavailable on this platform")
}
