package verification

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testDigest(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }

func TestRegistrySelectsTrustedApplicableSpecsAndCanonicalDigest(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	specs := []TrustedVerifierSpec{
		{Name: "z-test", Version: "2", Argv: []string{exe, "-test.run=TestHelperProcess"}, Applicable: true},
		{Name: "a-lint", Version: "1", Argv: []string{exe, "-test.run=TestHelperProcess"}, Applicable: true},
		{Name: "off", Version: "1", Argv: []string{exe}, Applicable: false},
	}
	a, err := NewVerifierRegistry(specs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewVerifierRegistry([]TrustedVerifierSpec{specs[1], specs[0], specs[2]})
	if err != nil {
		t.Fatal(err)
	}
	if a.VerifierSetDigest != b.VerifierSetDigest || a.ConfigDigest != b.ConfigDigest {
		t.Fatalf("registry digest depends on input order: %q/%q vs %q/%q", a.VerifierSetDigest, a.ConfigDigest, b.VerifierSetDigest, b.ConfigDigest)
	}
	plan, err := a.Select(VerificationPolicy{RequiredVerifiers: []string{"a-lint", "z-test"}})
	if err != nil || len(plan.Specs) != 2 || plan.Specs[0].Name != "a-lint" || plan.Specs[1].Name != "z-test" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestRegistryRejectsUnknownInapplicableAndDuplicateRequiredVerifiers(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "lint", Version: "1", Argv: []string{exe}, Applicable: false}})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range [][]string{{"missing"}, {"lint"}, {"lint", "lint"}} {
		if _, err := reg.Select(VerificationPolicy{RequiredVerifiers: required}); err == nil {
			t.Fatalf("required=%v unexpectedly accepted", required)
		}
	}
}

func TestTrustedVerifyFailsClosedForPublicAndAllowsExplicitLocalNoVerifier(t *testing.T) {
	r := &trustedRecordingRunner{}
	reg, err := NewVerifierRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}
	public, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, req, r)
	if err != nil || public.Decision != DecisionNoVerifier || public.Passed || public.Reason != ReasonNoVerifierPublic {
		t.Fatalf("public=%#v err=%v", public, err)
	}
	local, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "local", AllowNoVerifier: true}, req, r)
	if err != nil || local.Decision != DecisionNoVerifier || local.Passed || local.Reason != ReasonNoVerifierLocal {
		t.Fatalf("local=%#v err=%v", local, err)
	}
}

func TestTrustedVerifyRunsAllVerifiersInDeterministicOrderAndBindsEvidence(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{
		{Name: "z-test", Version: "1", Argv: []string{exe, "z-test"}, Applicable: true},
		{Name: "a-lint", Version: "3", Argv: []string{exe, "a-lint"}, Applicable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &trustedRecordingRunner{}
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}
	got, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, req, r)
	if err != nil || got.Decision != DecisionPassed || len(got.Evidence) != 2 {
		t.Fatalf("result=%#v err=%v", got, err)
	}
	if strings.Join(r.names(), ",") != "a-lint,z-test" {
		t.Fatalf("order=%v", r.names())
	}
	for _, e := range got.Evidence {
		if !e.ValidForTrusted(req.CandidateDigest, req.BaseDigest, req.PolicyDigest, req.GuardDigest, reg.VerifierSetDigest) || e.EvidenceDigest == "" {
			t.Fatalf("unbound evidence=%#v", e)
		}
	}
	if got.ValidFor(req, VerificationPolicy{Visibility: "public"}, reg) == false {
		t.Fatal("matching verification was not reusable")
	}
	staleReq := req
	staleReq.PolicyDigest = testDigest('e')
	if got.ValidFor(staleReq, VerificationPolicy{Visibility: "public"}, reg) {
		t.Fatal("policy mutation reused verification")
	}
}

func TestTrustedEvidenceInvalidatesEveryBindingDimension(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "ok", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}
	policy := VerificationPolicy{Visibility: "public"}
	result, err := reg.Verify(context.Background(), policy, req, &trustedRecordingRunner{})
	if err != nil || !result.ValidFor(req, policy, reg) {
		t.Fatalf("baseline invalid: %#v err=%v", result, err)
	}
	mutations := []func(*TrustedRequest, *VerifierRegistry){
		func(r *TrustedRequest, _ *VerifierRegistry) { r.CandidateDigest = testDigest('e') },
		func(r *TrustedRequest, _ *VerifierRegistry) { r.BaseDigest = testDigest('e') },
		func(r *TrustedRequest, _ *VerifierRegistry) { r.PolicyDigest = testDigest('e') },
		func(r *TrustedRequest, _ *VerifierRegistry) { r.GuardDigest = testDigest('e') },
		func(_ *TrustedRequest, reg *VerifierRegistry) { reg.VerifierSetDigest = testDigest('e') },
	}
	for i, mutate := range mutations {
		mutatedReq, mutatedReg := req, *reg
		mutate(&mutatedReq, &mutatedReg)
		if result.ValidFor(mutatedReq, policy, &mutatedReg) {
			t.Fatalf("mutation %d reused evidence", i)
		}
	}
}

func TestTrustedVerifyRejectsShellAndSymlinkExecutables(t *testing.T) {
	for name, argv := range map[string][]string{
		"shell":      {"/bin/sh", "-c", "true"},
		"shell-wrap": {"/usr/bin/env", "sh", "-c", "true"},
	} {
		if _, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: name, Version: "1", Argv: argv, Applicable: true}}); err == nil {
			t.Fatalf("%s command accepted", name)
		}
	}
	d := t.TempDir()
	target := filepath.Join(d, "target")
	link := filepath.Join(d, "link")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink capability unavailable: %v", err)
	}
	if _, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "link", Version: "1", Argv: []string{link}, Applicable: true}}); err == nil {
		t.Fatal("symlink executable accepted")
	}
}

func TestTrustedVerifyScrubsEnvironmentBoundsFailureMetadataAndHonorsCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "slow", Version: "1", Argv: []string{exe}, Applicable: true, Timeout: time.Hour, MaxOutput: 8}})
	if err != nil {
		t.Fatal(err)
	}
	r := &trustedBlockingRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(5 * time.Millisecond); cancel() }()
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir(), OverallTimeout: time.Second}
	got, err := reg.Verify(ctx, VerificationPolicy{Visibility: "public"}, req, r)
	if err == nil || got.Decision != DecisionCancelled || got.Passed {
		t.Fatalf("result=%#v err=%v", got, err)
	}
	env := r.env()
	if env["SECRET"] != "" || env["SSH_AUTH_SOCK"] != "" || env["HOME"] != "" || strings.Contains(got.FailureMetadata, "TOP_SECRET") || len(got.FailureMetadata) == 0 {
		t.Fatalf("environment or failure metadata leaked: env=%#v result=%#v", r.lastEnv, got)
	}
}

func TestTrustedVerifyTurnsOversizedOutputIntoRedactedFailureMetadata(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "loud", Version: "1", Argv: []string{exe}, Applicable: true, MaxOutput: 4}})
	if err != nil {
		t.Fatal(err)
	}
	r := &trustedLoudRunner{}
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}
	got, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, req, r)
	if err != nil || got.Decision != DecisionFailed || got.Passed || !got.Evidence[0].OutputTruncated {
		t.Fatalf("result=%#v err=%v", got, err)
	}
	if strings.Contains(got.FailureMetadata, "TOP_SECRET") || got.Evidence[0].StdoutBytes > 4 || got.Evidence[0].StderrBytes > 4 {
		t.Fatalf("output leaked or was not bounded: %#v", got)
	}
}

func TestTrustedVerifyRejectsExecutableReplacementAndTamperedEvidence(t *testing.T) {
	d := t.TempDir()
	path := filepath.Join(d, "verifier")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "stable", Version: "1", Argv: []string{path}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	good, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}, &trustedRecordingRunner{})
	if err != nil || !good.ValidFor(TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}, VerificationPolicy{Visibility: "public"}, reg) {
		t.Fatalf("baseline evidence invalid: %#v err=%v", good, err)
	}
	good.Evidence[0].Passed = false
	if good.ValidFor(TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}, VerificationPolicy{Visibility: "public"}, reg) {
		t.Fatal("tampered evidence reused")
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}
	got, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, req, &trustedRecordingRunner{})
	if err != nil || got.Decision != DecisionFailed || got.Reason != ReasonExecutableInvalid {
		t.Fatalf("replacement accepted: result=%#v err=%v", got, err)
	}
}

func TestTrustedVerifyReportsPerCommandTimeout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "short", Version: "1", Argv: []string{exe}, Applicable: true, Timeout: 5 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	req := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: t.TempDir()}
	got, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, req, &trustedBlockingRunner{})
	if err != nil || got.Decision != DecisionFailed || got.Reason != ReasonVerificationTimeout || !got.Evidence[0].TimedOut {
		t.Fatalf("timeout result=%#v err=%v", got, err)
	}
}

func TestTrustedVerifyRequiresExistingCanonicalCandidateDirectory(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "ok", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	base := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d')}
	for _, dir := range []string{"relative", filepath.Join(t.TempDir(), "missing")} {
		base.Dir = dir
		if _, err := reg.Verify(context.Background(), VerificationPolicy{Visibility: "public"}, base, &trustedRecordingRunner{}); err == nil {
			t.Fatalf("directory %q unexpectedly accepted", dir)
		}
	}
}

type trustedRecordingRunner struct {
	mu      sync.Mutex
	called  []string
	lastEnv map[string]string
}

func (r *trustedRecordingRunner) Run(_ context.Context, _ string, env map[string]string, args ...string) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastEnv = env
	if len(args) > 1 {
		r.called = append(r.called, args[1])
	} else if len(args) > 0 {
		r.called = append(r.called, filepath.Base(args[0]))
	}
	return Result{ExitCode: 0}, nil
}
func (r *trustedRecordingRunner) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.called...)
}

type trustedBlockingRunner struct {
	mu      sync.Mutex
	lastEnv map[string]string
}

func (r *trustedBlockingRunner) Run(ctx context.Context, _ string, env map[string]string, _ ...string) (Result, error) {
	r.mu.Lock()
	r.lastEnv = env
	r.mu.Unlock()
	<-ctx.Done()
	return Result{Stdout: "TOP_SECRET output", Stderr: "TOP_SECRET error"}, ctx.Err()
}
func (r *trustedBlockingRunner) env() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastEnv
}

type trustedLoudRunner struct{}

func (*trustedLoudRunner) Run(_ context.Context, _ string, _ map[string]string, _ ...string) (Result, error) {
	return Result{Stdout: "TOP_SECRET stdout", Stderr: "TOP_SECRET stderr", ExitCode: 0}, nil
}
func (*trustedLoudRunner) RunBounded(_ context.Context, _ string, _ map[string]string, _ int, _ ...string) (Result, error) {
	return Result{Stdout: "TOP_SECRET stdout", Stderr: "TOP_SECRET stderr", ExitCode: 0}, nil
}

func TestHelperProcess(t *testing.T) {}
