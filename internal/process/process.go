// Package process provides the bounded, cancellable process boundary used by
// external Git, provider, and verifier commands.
package process

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
)

const DefaultMaxOutput = 1 << 20

var ErrOutputLimit = errors.New("process output exceeded limit")

type Options struct {
	Executable     string
	Dir            string
	Env            []string
	Args           []string
	MaxOutput      int
	SeparateOutput bool
}

type Result struct {
	Output    string
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}

func Run(ctx context.Context, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("process context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if options.Executable == "" {
		return Result{}, errors.New("process executable is required")
	}
	max := options.MaxOutput
	if max <= 0 {
		max = DefaultMaxOutput
	}
	command := exec.Command(options.Executable, options.Args...)
	command.Dir = options.Dir
	if options.Env != nil {
		command.Env = append([]string(nil), options.Env...)
	}
	stdout := &boundedOutput{max: max}
	overflow := &outputLimitSignal{ch: make(chan struct{})}
	stdout.overflow = overflow
	stderr := stdout
	if options.SeparateOutput {
		stderr = &boundedOutput{max: max, overflow: overflow}
	}
	command.Stdout = stdout
	command.Stderr = stderr
	supervisor, err := newSupervisor(command)
	if err != nil {
		return Result{}, err
	}
	defer supervisor.Close()
	if err := command.Start(); err != nil {
		return Result{}, err
	}
	if err := supervisor.Attach(command); err != nil {
		_ = supervisor.Terminate(command)
		_ = command.Wait()
		return Result{}, err
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = supervisor.Terminate(command)
		case <-overflow.ch:
			_ = supervisor.Terminate(command)
		case <-done:
		}
	}()
	waitErr := command.Wait()
	close(done)
	exitCode := 0
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	result := Result{Output: string(stdout.bytes), Stdout: string(stdout.bytes), Stderr: string(stderr.bytes), ExitCode: exitCode, Truncated: stdout.truncated || stderr.truncated}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if result.Truncated {
		return result, ErrOutputLimit
	}
	return result, waitErr
}

type boundedOutput struct {
	bytes     []byte
	max       int
	truncated bool
	overflow  *outputLimitSignal
}

type outputLimitSignal struct {
	ch   chan struct{}
	once sync.Once
}

func (b *boundedOutput) signalOverflow() {
	if b.overflow != nil {
		b.overflow.once.Do(func() { close(b.overflow.ch) })
	}
}

func (b *boundedOutput) Write(value []byte) (int, error) {
	remaining := b.max - len(b.bytes)
	if remaining <= 0 {
		b.truncated = len(value) > 0
		b.signalOverflow()
		return len(value), io.ErrShortBuffer
	}
	if len(value) > remaining {
		b.bytes = append(b.bytes, value[:remaining]...)
		b.truncated = true
		b.signalOverflow()
		return len(value), io.ErrShortBuffer
	}
	b.bytes = append(b.bytes, value...)
	return len(value), nil
}
