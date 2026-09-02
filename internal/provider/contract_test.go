package provider

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderValidationRejectsUnsafeDestinationAndNonCanonicalSHA(t *testing.T) {
	for _, ref := range []string{"--all", "../main", "feature/../main", "feature@{1}", "feature.lock", "feature\nmain", ".", "HEAD"} {
		if validRef(ref) {
			t.Fatalf("unsafe ref accepted: %q", ref)
		}
	}
	for _, sha := range []string{strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("A", 40)} {
		if validSHA(sha) {
			t.Fatalf("non-canonical SHA accepted: %q", sha)
		}
	}
	for _, request := range []RemoteRequest{
		{Owner: "../owner", Name: "repo", Visibility: "private"},
		{Owner: "owner", Name: "repo.lock", Visibility: "private"},
		{Owner: "owner", Name: "repo@{x}", Visibility: "private"},
		{Owner: "--owner", Name: "repo", Visibility: "private"},
	} {
		if validIdentity(request) == nil {
			t.Fatalf("unsafe identity accepted: %#v", request)
		}
	}
}

func TestSafeFakeInjectsInspectionFailureAndTypedAbsentOutcome(t *testing.T) {
	f := NewSafeFake()
	request := RemoteRequest{Owner: "owner", Name: "repo", Visibility: "private"}
	if _, err := f.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Inspect(context.Background(), request, "main"); !errors.Is(err, ErrRefAbsent) {
		t.Fatalf("missing ref error=%v, want ErrRefAbsent", err)
	}
	f.FailNext("inspect", ErrTimeout)
	if _, err := f.Inspect(context.Background(), request, "main"); !errors.Is(err, ErrTimeout) {
		t.Fatalf("injected inspection error=%v, want timeout", err)
	}
}

func TestGitPusherBindsCanonicalIdentityBeforeExactPush(t *testing.T) {
	r := &argRunner{results: []Result{{Output: "https://github.com/owner/repo.git\n"}, {}}}
	p := GitPusher{Runner: r, AllowedRemotes: map[string]string{"owner/repo": "origin"}}
	sha := strings.Repeat("a", 40)
	if err := p.Push(context.Background(), "owner/repo", sha, "feature/x"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.sshCommand=", "-c", "credential.helper=", "remote", "get-url", "--push", "--", "origin"},
		{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.sshCommand=", "-c", "credential.helper=", "push", "--", "origin", sha + ":refs/heads/feature/x"},
	}
	if len(r.calls) != len(want) {
		t.Fatalf("calls=%#v want %#v", r.calls, want)
	}
	for i := range want {
		if strings.Join(r.calls[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("argv[%d]=%#v want %#v", i, r.calls[i], want[i])
		}
	}
}

func TestGitPusherRequiresAllowedRemoteBinding(t *testing.T) {
	r := &argRunner{results: []Result{{}}}
	sha := strings.Repeat("a", 40)
	if err := (GitPusher{Runner: r}).Push(context.Background(), "owner/repo", sha, "main"); err == nil {
		t.Fatal("push without an allowlist was accepted")
	}
	if len(r.calls) != 0 {
		t.Fatalf("unbound push invoked git: %#v", r.calls)
	}
}

func TestGitPusherRejectsAliasInjectionWithoutInvokingGit(t *testing.T) {
	for _, alias := range []string{"--upload-pack=evil", "../origin", "/tmp/origin", "origin/name", "origin\nname", "origin;evil"} {
		t.Run(alias, func(t *testing.T) {
			r := &argRunner{results: []Result{{Output: "https://github.com/owner/repo\n"}}}
			sha := strings.Repeat("a", 40)
			err := (GitPusher{Runner: r, AllowedRemotes: map[string]string{"owner/repo": alias}}).Push(context.Background(), "owner/repo", sha, "main")
			if err == nil {
				t.Fatal("unsafe alias accepted")
			}
			if len(r.calls) != 0 {
				t.Fatalf("unsafe alias reached git: %#v", r.calls)
			}
		})
	}
}

func TestGitPusherRejectsHostileOrMismatchedRemoteURL(t *testing.T) {
	for _, output := range []string{
		"https://github.com/other/repo\n",
		"https://evil.example/owner/repo\n",
		"http://github.com/owner/repo\n",
		"https://user:secret@github.com/owner/repo\n",
		"https://github.com/owner/repo?token=secret\n",
		"https://github.com/owner/repo#fragment\n",
		"git@evil.example:owner/repo\n",
		"git@github.com:owner/repo/extra\n",
		"git@github.com:owner/repo\ngit@github.com:owner/repo\n",
	} {
		t.Run(output, func(t *testing.T) {
			r := &argRunner{results: []Result{{Output: output}, {}}}
			sha := strings.Repeat("a", 40)
			err := (GitPusher{Runner: r, AllowedRemotes: map[string]string{"owner/repo": "origin"}}).Push(context.Background(), "owner/repo", sha, "main")
			if err == nil {
				t.Fatal("hostile or mismatched URL accepted")
			}
			if len(r.calls) != 1 {
				t.Fatalf("push proceeded after URL rejection: %#v", r.calls)
			}
		})
	}
}

func TestGitPusherBindingFailureIsTypedAndRedacted(t *testing.T) {
	secret := "https://github.com/other/repo?token=do-not-return\n"
	r := &argRunner{results: []Result{{Output: secret}}}
	err := (GitPusher{Runner: r, AllowedRemotes: map[string]string{"owner/repo": "origin"}}).Push(context.Background(), "owner/repo", strings.Repeat("a", 40), "main")
	if !errors.Is(err, ErrRemoteBinding) {
		t.Fatalf("error=%v, want remote binding category", err)
	}
	if strings.Contains(err.Error(), "do-not-return") {
		t.Fatalf("remote output leaked: %v", err)
	}
}

func TestGitPusherRejectsOverlongRemoteOutput(t *testing.T) {
	r := &argRunner{results: []Result{{Output: strings.Repeat("x", maxOutput+1)}}}
	sha := strings.Repeat("a", 40)
	err := (GitPusher{Runner: r, AllowedRemotes: map[string]string{"owner/repo": "origin"}}).Push(context.Background(), "owner/repo", sha, "main")
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("error=%v, want output limit", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("push proceeded after output truncation: %#v", r.calls)
	}
}

func TestGitPusherBindsRealTemporaryGitRemoteBeforePush(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	gitPath, err = filepath.EvalSymlinks(gitPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if output, err := exec.Command(gitPath, "init", "--quiet", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, output)
	}
	if output, err := exec.Command(gitPath, "-C", repo, "remote", "add", "origin", "https://github.com/other/repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v (%s)", err, output)
	}
	p := GitPusher{
		Runner:         SystemRunner{Executable: gitPath, WorkingDir: repo},
		AllowedRemotes: map[string]string{"owner/repo": "origin"},
	}
	err = p.Push(context.Background(), "owner/repo", strings.Repeat("a", 40), "main")
	if !errors.Is(err, ErrRemoteBinding) {
		t.Fatalf("mismatched real remote error=%v, want binding rejection", err)
	}
}

func TestGHPublishConfirmsExactRemoteSHA(t *testing.T) {
	sha := strings.Repeat("b", 64)
	r := &argRunner{results: []Result{{Output: ""}, {Output: "" + sha + "\n"}}}
	p := &recordingPusher{}
	g := GH{Runner: r, Pusher: p}
	if err := g.Publish(context.Background(), PushRequest{Owner: "owner", Name: "repo", Ref: "main", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	if p.remote != "owner/repo" || p.sha != sha || p.ref != "main" {
		t.Fatalf("push=%#v", p)
	}
}

func TestGHPublishExistingExactRefIsIdempotentWithoutPush(t *testing.T) {
	sha := strings.Repeat("c", 40)
	r := &argRunner{results: []Result{{Output: sha + "\n"}}}
	p := &recordingPusher{}
	if err := (GH{Runner: r, Pusher: p}).Publish(context.Background(), PushRequest{Owner: "owner", Name: "repo", Ref: "main", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	if p.remote != "" || len(r.calls) != 1 {
		t.Fatalf("existing exact ref was pushed: pusher=%#v calls=%#v", p, r.calls)
	}
}

func TestGHPublishWrongExistingRefBlocksWithoutPush(t *testing.T) {
	r := &argRunner{results: []Result{{Output: strings.Repeat("d", 40) + "\n"}}}
	p := &recordingPusher{}
	err := (GH{Runner: r, Pusher: p}).Publish(context.Background(), PushRequest{Owner: "owner", Name: "repo", Ref: "main", SHA: strings.Repeat("e", 40)})
	if !errors.Is(err, ErrRefConflict) || p.remote != "" || len(r.calls) != 1 {
		t.Fatalf("wrong ref was overwritten: err=%v pusher=%#v calls=%#v", err, p, r.calls)
	}
}

func TestGHConfirmTransportFailureHasUnknownOutcome(t *testing.T) {
	r := &argRunner{results: []Result{{Err: errors.New("network is unreachable")}}}
	outcome, err := (GH{Runner: r}).ConfirmPush(context.Background(), PushRequest{Owner: "owner", Name: "repo", Ref: "main", SHA: strings.Repeat("a", 40)})
	if outcome != "" || !errors.Is(err, ErrOffline) {
		t.Fatalf("outcome=%q err=%v, want unknown/offline", outcome, err)
	}
}

func TestGHInspectMapsNotFoundToAbsent(t *testing.T) {
	r := &argRunner{results: []Result{{Err: errors.New("HTTP 404: ref not found")}}}
	_, err := (GH{Runner: r}).Inspect(context.Background(), RemoteRequest{Owner: "owner", Name: "repo", Visibility: "private"}, "main")
	if !errors.Is(err, ErrRefAbsent) {
		t.Fatalf("error=%v, want absent", err)
	}
}

func TestSafeFakePublishDoesNotOverwriteWrongExistingRef(t *testing.T) {
	f := NewSafeFake()
	f.Add("owner/repo", "private")
	f.SetRef("owner", "repo", "main", strings.Repeat("d", 40))
	err := f.Publish(context.Background(), PushRequest{Owner: "owner", Name: "repo", Ref: "main", SHA: strings.Repeat("e", 40)})
	if !errors.Is(err, ErrRefConflict) {
		t.Fatalf("error=%v, want conflict", err)
	}
	for _, call := range f.Calls() {
		if call.Operation == "push" {
			t.Fatal("wrong existing ref was overwritten")
		}
	}
}

func TestGHMapsProviderFailureClassesWithoutLeakingOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		want error
	}{
		{"offline", "network is unreachable", ErrOffline},
		{"auth", "authentication required", ErrAuth},
		{"rate", "API rate limit exceeded", ErrRateLimit},
		{"protected", "protected branch hook declined", ErrProtectedBranch},
		{"nonff", "non-fast-forward", ErrNonFastForward},
		{"secret", "secret scanning push protection", ErrSecretScanning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &argRunner{results: []Result{{Err: errors.New(tc.msg), Output: "token=do-not-return"}}}
			g := GH{Runner: r}
			_, err := g.Create(context.Background(), RemoteRequest{Owner: "owner", Name: "repo", Visibility: "private"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "do-not-return") {
				t.Fatalf("provider output leaked: %v", err)
			}
		})
	}
}

func TestLocalOnlyProviderNeverDelegates(t *testing.T) {
	inner := NewSafeFake()
	p := NewLocalOnlyProvider(inner)
	request := RemoteRequest{Owner: "owner", Name: "repo", Visibility: "private"}
	if _, err := p.Create(context.Background(), request); !errors.Is(err, ErrLocalOnly) {
		t.Fatalf("create error=%v", err)
	}
	if err := p.Publish(context.Background(), PushRequest{Owner: "owner", Name: "repo", Ref: "main", SHA: strings.Repeat("a", 40)}); !errors.Is(err, ErrLocalOnly) {
		t.Fatalf("publish error=%v", err)
	}
	if got := len(inner.Calls()); got != 0 {
		t.Fatalf("local-only delegated %d calls", got)
	}
}

type recordingPusher struct{ remote, sha, ref string }

func (p *recordingPusher) Push(_ context.Context, remote, sha, ref string) error {
	p.remote, p.sha, p.ref = remote, sha, ref
	return nil
}
