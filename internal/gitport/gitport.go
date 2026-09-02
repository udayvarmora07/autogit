package gitport

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	executable, err := canonicalExecutable(r.Executable)
	if err != nil {
		return Result{}, err
	}
	if r.MaxOutput <= 0 {
		r.MaxOutput = 1 << 20
	}
	c := exec.CommandContext(ctx, executable, args...)
	c.Dir = dir
	b := &cappedBuffer{max: r.MaxOutput}
	c.Stdout = b
	c.Stderr = b
	err = c.Run()
	if b.limited && err == nil {
		err = io.ErrShortBuffer
	}
	return Result{Output: string(b.b), Err: err, Truncated: b.limited}, err
}

func canonicalExecutable(path string) (string, error) {
	if path == "" {
		path = "git"
	}
	resolved, err := exec.LookPath(path)
	if err != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) != resolved {
		return "", fmt.Errorf("invalid Git executable")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(resolved))
	if err != nil {
		return "", fmt.Errorf("invalid Git executable")
	}
	canon := filepath.Join(parent, filepath.Base(resolved))
	info, err := os.Lstat(canon)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
		return "", fmt.Errorf("invalid Git executable")
	}
	return canon, nil
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
