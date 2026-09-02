// Package verification runs only configured argv vectors and returns
// digest-bound, bounded evidence.
package verification

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Result struct {
	Stdout, Stderr string
	ExitCode       int
}
type Runner interface {
	Run(context.Context, string, map[string]string, ...string) (Result, error)
}
type boundedRunner interface {
	RunBounded(context.Context, string, map[string]string, int, ...string) (Result, error)
}
type Request struct {
	CandidateDigest, BaseDigest, PolicyDigest, VerifierDigest, Name, Dir string
	Env                                                                  map[string]string
	Timeout                                                              time.Duration
}
type Evidence struct {
	CandidateDigest, BaseDigest, PolicyDigest, VerifierDigest, Verifier string
	Stdout, Stderr                                                      string
	ExitCode                                                            int
	Passed                                                              bool
	TimedOut                                                            bool
}
type Verifier struct {
	Runner      Runner
	Commands    map[string][]string
	MaxOutput   int
	Timeout     time.Duration
	TrustedPath string
}

func (v Verifier) Run(parent context.Context, req Request) (Evidence, error) {
	argv, ok := v.Commands[req.Name]
	if !ok || len(argv) == 0 {
		return Evidence{}, fmt.Errorf("verifier %q is not configured", req.Name)
	}
	if v.Runner == nil {
		return Evidence{}, errors.New("verification runner is required")
	}
	if err := trustedArgv(argv); err != nil {
		return Evidence{}, err
	}
	if !digestRE.MatchString(req.CandidateDigest) || !digestRE.MatchString(req.BaseDigest) || !digestRE.MatchString(req.PolicyDigest) || !digestRE.MatchString(req.VerifierDigest) {
		return Evidence{}, errors.New("invalid evidence digest")
	}
	if req.Timeout <= 0 {
		req.Timeout = v.Timeout
	}
	if req.Timeout <= 0 {
		req.Timeout = 2 * time.Minute
	}
	if v.MaxOutput <= 0 {
		v.MaxOutput = 1 << 20
	}
	ctx, cancel := context.WithTimeout(parent, req.Timeout)
	defer cancel()
	// Never inherit caller credentials. Only an explicitly safe path is passed.
	env := map[string]string{}
	if v.TrustedPath != "" {
		env["PATH"] = v.TrustedPath
	}
	var res Result
	var err error
	if br, ok := v.Runner.(boundedRunner); ok {
		res, err = br.RunBounded(ctx, req.Dir, env, v.MaxOutput, append([]string(nil), argv...)...)
	} else {
		res, err = v.Runner.Run(ctx, req.Dir, env, append([]string(nil), argv...)...)
	}
	e := Evidence{CandidateDigest: req.CandidateDigest, BaseDigest: req.BaseDigest, PolicyDigest: req.PolicyDigest, VerifierDigest: req.VerifierDigest, Verifier: req.Name, ExitCode: res.ExitCode, Passed: err == nil && res.ExitCode == 0}
	e.Stdout = bounded(res.Stdout, v.MaxOutput)
	e.Stderr = bounded(res.Stderr, v.MaxOutput)
	if ctx.Err() != nil {
		e.TimedOut = true
		e.Passed = false
		if err == nil {
			err = ctx.Err()
		}
	}
	return e, err
}
func (e Evidence) ValidFor(candidate, base, policy, verifier string) bool {
	return e.Passed && !e.TimedOut && e.CandidateDigest == candidate && e.BaseDigest == base && e.PolicyDigest == policy && e.VerifierDigest == verifier
}
func bounded(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var digestRE = regexp.MustCompile(`^(sha256|hmac-sha256):[a-f0-9]{64}$`)

func trustedArgv(argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty verifier argv")
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	switch base {
	case "sh", "bash", "dash", "zsh", "fish", "cmd", "cmd.exe", "powershell", "pwsh", "env", "xargs", "nohup", "sudo", "doas":
		return errors.New("shell verifier is not allowed")
	case "python", "python2", "python3", "node", "perl", "ruby", "php":
		return errors.New("interpreter verifier is not allowed")
	}
	for _, a := range argv {
		if strings.IndexByte(a, 0) >= 0 {
			return errors.New("verifier argument contains NUL")
		}
	}
	return nil
}

// ExecRunner is the production process boundary. It never invokes a shell and
// caps each stream while the process is running, so hostile commands cannot
// consume unbounded memory.
type ExecRunner struct{ MaxOutput int }

func (e ExecRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	return e.run(ctx, dir, env, 1<<20, args...)
}
func (e ExecRunner) RunBounded(ctx context.Context, dir string, env map[string]string, max int, args ...string) (Result, error) {
	if max <= 0 {
		max = e.MaxOutput
	}
	if max <= 0 {
		max = 1 << 20
	}
	return e.run(ctx, dir, env, max, args...)
}
func (ExecRunner) run(ctx context.Context, dir string, env map[string]string, max int, args ...string) (Result, error) {
	if len(args) == 0 {
		return Result{}, errors.New("empty verifier argv")
	}
	if !filepath.IsAbs(args[0]) {
		return Result{}, errors.New("verifier executable must be an absolute trusted path")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = []string{}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+env[k])
	}
	out := &capBuffer{max: max}
	errout := &capBuffer{max: max}
	cmd.Stdout = out
	cmd.Stderr = errout
	err := cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if out.limited || errout.limited {
		return Result{Stdout: out.String(), Stderr: errout.String(), ExitCode: code}, fmt.Errorf("verification output exceeded limit")
	}
	return Result{Stdout: out.String(), Stderr: errout.String(), ExitCode: code}, err
}

type capBuffer struct {
	b       []byte
	max     int
	limited bool
}

func (b *capBuffer) Write(p []byte) (int, error) {
	if len(b.b)+len(p) > b.max {
		n := b.max - len(b.b)
		if n > 0 {
			b.b = append(b.b, p[:n]...)
		}
		b.limited = true
		return len(p), errors.New("output limit")
	}
	b.b = append(b.b, p...)
	return len(p), nil
}
func (b *capBuffer) String() string { return strings.Clone(string(b.b)) }
