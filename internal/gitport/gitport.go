package gitport

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
)

type Result struct {
	Output    string
	Err       error
	Truncated bool
}
type Runner struct {
	Executable string
	MaxOutput  int
}

func (r Runner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if r.Executable == "" {
		r.Executable = "git"
	}
	if r.MaxOutput <= 0 {
		r.MaxOutput = 1 << 20
	}
	c := exec.CommandContext(ctx, r.Executable, args...)
	c.Dir = dir
	b := &cappedBuffer{max: r.MaxOutput}
	c.Stdout = b
	c.Stderr = b
	err := c.Run()
	if b.limited && err == nil {
		err = io.ErrShortBuffer
	}
	return Result{Output: string(b.b), Err: err, Truncated: b.limited}, err
}

var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var refRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var remoteRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func PushArgs(remote, sha, ref string) ([]string, error) {
	if remote == "" || !remoteRE.MatchString(remote) || !shaRE.MatchString(sha) || !refRE.MatchString(ref) || ref[0] == '-' {
		return nil, fmt.Errorf("invalid push destination")
	}
	if ref == "HEAD" || ref == "" {
		return nil, fmt.Errorf("invalid branch ref")
	}
	return []string{"push", "--", remote, sha + ":refs/heads/" + ref}, nil
}

type cappedBuffer struct {
	b       []byte
	max     int
	limited bool
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(w.b)+n > w.max {
		take := w.max - len(w.b)
		if take > 0 {
			w.b = append(w.b, p[:take]...)
		}
		w.limited = true
		return n, io.ErrShortBuffer
	}
	w.b = append(w.b, p...)
	return n, nil
}
