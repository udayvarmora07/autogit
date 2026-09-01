package verification

import (
	"context"
	"testing"
	"time"
)

func TestVerifyUsesAllowlistedArgvScrubbedEnvironmentAndBoundedEvidence(t *testing.T) {
	r := &runner{out: "pass"}
	v := Verifier{Runner: r, Commands: map[string][]string{"go-test": {"go", "test", "./..."}}, MaxOutput: 16}
	e, err := v.Run(context.Background(), Request{CandidateDigest: "sha256:" + hex64('a'), BaseDigest: "sha256:" + hex64('b'), PolicyDigest: "sha256:" + hex64('c'), VerifierDigest: "sha256:" + hex64('d'), Name: "go-test", Dir: "/candidate", Env: map[string]string{"SECRET": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Passed != true || len(e.Stdout) != 4 {
		t.Fatalf("evidence %#v", e)
	}
	if r.args[0] != "go" || r.env["SECRET"] != "" {
		t.Fatalf("argv/env %#v %#v", r.args, r.env)
	}
}

func TestVerifyRejectsStaleEvidenceAndUnconfiguredCommand(t *testing.T) {
	v := Verifier{Runner: &runner{}, Commands: map[string][]string{"ok": {"true"}}}
	if _, err := v.Run(context.Background(), Request{Name: "missing"}); err == nil {
		t.Fatal("unconfigured verifier accepted")
	}
	e, err := v.Run(context.Background(), Request{CandidateDigest: "sha256:" + hex64('a'), BaseDigest: "sha256:" + hex64('b'), PolicyDigest: "sha256:" + hex64('c'), VerifierDigest: "sha256:" + hex64('d'), Name: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ValidFor("sha256:"+hex64('z'), e.BaseDigest, e.PolicyDigest, e.VerifierDigest) {
		t.Fatal("stale evidence reused")
	}
}
func TestVerifyCancelsAtTimeoutAndBoundsOutput(t *testing.T) {
	r := &blockingRunner{}
	v := Verifier{Runner: r, Commands: map[string][]string{"slow": {"slow"}}, MaxOutput: 4}
	e, err := v.Run(context.Background(), Request{Name: "slow", Timeout: 5 * time.Millisecond, CandidateDigest: "sha256:" + hex64('a'), BaseDigest: "sha256:" + hex64('b'), PolicyDigest: "sha256:" + hex64('c'), VerifierDigest: "sha256:" + hex64('d')})
	if err == nil || !e.TimedOut || e.Passed {
		t.Fatalf("evidence=%#v err=%v", e, err)
	}
	if len(e.Stdout) > 4 || len(e.Stderr) > 4 {
		t.Fatalf("unbounded evidence=%#v", e)
	}
}

type runner struct {
	out  string
	args []string
	env  map[string]string
}

func (r *runner) Run(_ context.Context, _ string, env map[string]string, args ...string) (Result, error) {
	r.args = args
	r.env = env
	return Result{Stdout: r.out}, nil
}
func hex64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

var _ = time.Second

type blockingRunner struct{}

func (*blockingRunner) Run(ctx context.Context, _ string, _ map[string]string, _ ...string) (Result, error) {
	<-ctx.Done()
	return Result{Stdout: "0123456789", Stderr: "987654321"}, ctx.Err()
}
