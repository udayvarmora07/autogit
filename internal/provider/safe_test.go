package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeProviderRecordsExactCallsAndInjectsFailure(t *testing.T) {
	f := NewSafeFake()
	f.FailNext("push", ErrOffline)
	sha := "0123456789abcdef0123456789abcdef01234567"
	if _, err := f.Create(context.Background(), RemoteRequest{Owner: "o", Name: "n", Visibility: "private"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(context.Background(), PushRequest{Owner: "o", Name: "n", Ref: "main", SHA: sha}); err != ErrOffline {
		t.Fatalf("err=%v", err)
	}
	if err := f.Push(context.Background(), PushRequest{Owner: "o", Name: "n", Ref: "main", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 3 || calls[2].SHA != sha || calls[2].Ref != "main" {
		t.Fatalf("calls %#v", calls)
	}
}

func TestGHProviderBuildsExactSafeArgv(t *testing.T) {
	r := &argRunner{results: []Result{{}, {Output: "owner/repo\n"}, {Output: "private\n"}}}
	g := GH{Runner: r}
	if _, err := g.Create(context.Background(), RemoteRequest{Owner: "owner", Name: "repo", Visibility: "private"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"repo", "create", "owner/repo", "--private"}
	for i := range want {
		if i >= len(r.calls[0]) || r.calls[0][i] != want[i] {
			t.Fatalf("argv=%#v want %#v", r.args, want)
		}
	}
}

func TestGHProviderRejectsFalseSuccessPostcondition(t *testing.T) {
	r := &argRunner{results: []Result{{}, {Output: "other/repo\n"}, {Output: "private\n"}}}
	g := GH{Runner: r}
	if _, err := g.Create(context.Background(), RemoteRequest{Owner: "owner", Name: "repo", Visibility: "private"}); err == nil {
		t.Fatal("false-success provider response accepted")
	}
}

func TestSystemRunnerRequiresCanonicalExecutableAndWorkingDirectory(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, executable, dir string
	}{
		{name: "relative executable", executable: "git"},
		{name: "missing executable", executable: "/definitely/missing/autogit-command"},
		{name: "missing working directory", executable: executable},
		{name: "relative directory", executable: executable, dir: "."},
		{name: "missing directory", executable: executable, dir: "/definitely/missing/autogit-directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, runErr := (SystemRunner{Executable: tc.executable}).Run(context.Background(), tc.dir); runErr == nil {
				t.Fatal("uncanonical command input was accepted")
			}
		})
	}
}

func TestSystemRunnerUsesConfiguredWorkingDirectoryWhenCallOmitsIt(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err = filepath.EvalSymlinks(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (SystemRunner{Executable: executable, WorkingDir: workingDir}).Run(context.Background(), "", "-test.run=^$"); err != nil {
		t.Fatalf("configured working directory was not used: %v", err)
	}
}

func TestSystemRunnerPropagatesContextCancellation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err = filepath.EvalSymlinks(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (SystemRunner{Executable: executable, WorkingDir: workingDir}).Run(ctx, "", "-test.run=^$")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context cancellation", err)
	}
}

func TestSystemRunnerBoundsCombinedOutputWithoutRetainingOverflow(t *testing.T) {
	var out boundedOutput
	out.max = 4
	if n, err := out.Write([]byte("secret-output")); err != nil || n != len("secret-output") {
		t.Fatalf("write n=%d err=%v", n, err)
	}
	if !out.truncated || out.buf.String() != "secr" {
		t.Fatalf("bounded output=%q truncated=%v", out.buf.String(), out.truncated)
	}
}

func TestSystemRunnerScrubsAmbientProviderAndGitEnvironment(t *testing.T) {
	env := controlledCommandEnvFrom([]string{
		"PATH=/trusted/bin",
		"HOME=/home/user",
		"LANG=C",
		"GIT_DIR=/untrusted/repo/.git",
		"GH_TOKEN=secret",
		"SSH_AUTH_SOCK=/tmp/agent",
		"API_TOKEN=secret",
	})
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, forbidden := range []string{"GIT_DIR=", "GH_TOKEN=", "SSH_AUTH_SOCK=", "API_TOKEN="} {
		if strings.Contains(joined, "\n"+forbidden) {
			t.Fatalf("ambient variable retained: %q in %q", forbidden, joined)
		}
	}
	for _, required := range []string{"PATH=/trusted/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1"} {
		if !strings.Contains(joined, "\n"+required+"\n") {
			t.Fatalf("safety variable missing: %q in %q", required, joined)
		}
	}
}

type argRunner struct {
	args    []string
	calls   [][]string
	results []Result
}

func (r *argRunner) Run(_ context.Context, _ string, args ...string) (Result, error) {
	r.args = args
	r.calls = append(r.calls, append([]string(nil), args...))
	var out Result
	if len(r.results) > 0 {
		out = r.results[0]
		r.results = r.results[1:]
	}
	return out, nil
}
