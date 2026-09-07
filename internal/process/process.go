// Package process provides the bounded, cancellable process boundary used by
// external Git, provider, and verifier commands.
package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const DefaultMaxOutput = 1 << 20

var ErrOutputLimit = errors.New("process output exceeded limit")
var ErrSandboxUnavailable = errors.New("requested process sandbox is unavailable")
var ErrUnsupportedResourceLimit = errors.New("requested resource limit is unsupported on this platform")

type Options struct {
	Executable string
	// ExecutableFile binds execution to an already-open executable object on
	// platforms that provide descriptor execution. Executable remains the
	// descriptive path and is used as a fallback where no descriptor path
	// exists.
	ExecutableFile *os.File
	Dir            string
	Env            []string
	Args           []string
	MaxOutput      int
	SeparateOutput bool
	Limits         ResourceLimits
	// FilesystemAllowlist requests a namespace exposing only these paths plus
	// the command working directory and the host runtime paths needed to load
	// the executable. Paths are canonicalized and must already exist.
	FilesystemAllowlist []string
	NetworkDisabled     bool
}

// IsolationOptions describes optional OS-level namespace restrictions. An
// empty value retains the process-bounded execution path.
type IsolationOptions struct {
	FilesystemAllowlist []string
	NetworkDisabled     bool
}

// ResourceLimits are best-effort OS-enforced ceilings for a process-bounded
// operation. Zero fields retain the historical unlimited value.
type ResourceLimits struct {
	CPUTime     time.Duration
	MemoryBytes uint64
	FileBytes   uint64
	Processes   uint64
}

type Result struct {
	Output    string
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}

// ValidateResourceLimits rejects malformed or unrepresentable limits before
// a child is started. A zero field means that the caller did not request a
// ceiling for that dimension.
func ValidateResourceLimits(limits ResourceLimits) error {
	if limits.CPUTime < 0 {
		return errors.New("resource limits cannot be negative")
	}
	if limits.MemoryBytes != 0 && limits.MemoryBytes < 16<<20 {
		return errors.New("memory limit is below the supported minimum")
	}
	if limits.FileBytes != 0 && limits.FileBytes < 1<<20 {
		return errors.New("file limit is below the supported minimum")
	}
	if limits.Processes > 1024 {
		return errors.New("process limit exceeds the supported maximum")
	}
	if err := platformValidateResourceLimits(limits); err != nil {
		return err
	}
	return nil
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
	if err := ValidateResourceLimits(options.Limits); err != nil {
		return Result{}, err
	}
	max := options.MaxOutput
	if max <= 0 {
		max = DefaultMaxOutput
	}
	executable := options.Executable
	if options.ExecutableFile != nil {
		var err error
		executable, err = executableFDPath()
		if err != nil {
			return Result{}, err
		}
	}
	command := exec.Command(executable, options.Args...) // #nosec G204 -- executable and argv are validated by the owning boundary.
	command.Dir = options.Dir
	if options.Env != nil {
		command.Env = append([]string(nil), options.Env...)
	}
	if options.ExecutableFile != nil {
		command.ExtraFiles = []*os.File{options.ExecutableFile}
	}
	if err := prepareSandbox(command, options); err != nil {
		return Result{}, err
	}
	sandboxed := len(options.FilesystemAllowlist) != 0 || options.NetworkDisabled
	stdout := &boundedOutput{max: max}
	overflow := &outputLimitSignal{ch: make(chan struct{})}
	stdout.overflow = overflow
	stderr := stdout
	if options.SeparateOutput {
		stderr = &boundedOutput{max: max, overflow: overflow}
	}
	command.Stdout = stdout
	command.Stderr = stderr
	supervisor, err := newSupervisor(command, options.Limits)
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
	if !sandboxed {
		if err := applyResourceLimits(command.Process.Pid, options.Limits); err != nil {
			_ = supervisor.Terminate(command)
			_ = command.Wait()
			return Result{}, err
		}
	}

	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
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
	<-watcherDone
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
