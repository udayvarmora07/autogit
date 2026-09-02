package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Sentinel errors are stable machine-readable categories. Callers should use
// errors.Is rather than matching gh output.
var (
	ErrOffline         = errors.New("provider offline")
	ErrAuth            = errors.New("provider authentication failed")
	ErrTimeout         = errors.New("provider operation timed out")
	ErrRateLimit       = errors.New("provider rate limited")
	ErrNonFastForward  = errors.New("provider rejected non-fast-forward update")
	ErrProtectedBranch = errors.New("provider rejected protected branch update")
	ErrSecretScanning  = errors.New("provider rejected secret scanning policy")
	ErrCollision       = errors.New("remote already exists")
	ErrRefAbsent       = errors.New("remote ref is absent")
	ErrRefConflict     = errors.New("remote ref points to a different commit")
	ErrPostcondition   = errors.New("provider postcondition mismatch")
	ErrOutputLimit     = errors.New("provider output exceeded limit")
	ErrLocalOnly       = errors.New("provider disabled by local-only policy")
	ErrUnsupportedPush = errors.New("provider does not perform git pushes")
	ErrRemoteBinding   = errors.New("git remote binding rejected")
)

// IsRetryable reports whether a provider failure is safe to retry. Only
// explicitly transient provider categories are retryable; unknown failures
// fail closed.
func IsRetryable(err error) bool {
	return err != nil && (errors.Is(err, ErrOffline) || errors.Is(err, ErrTimeout) || errors.Is(err, ErrRateLimit))
}

type ErrorKind string

const (
	KindOffline         ErrorKind = "offline"
	KindAuth            ErrorKind = "auth"
	KindTimeout         ErrorKind = "timeout"
	KindRateLimit       ErrorKind = "rate_limit"
	KindNonFastForward  ErrorKind = "non_fast_forward"
	KindProtectedBranch ErrorKind = "protected_branch"
	KindSecretScanning  ErrorKind = "secret_scanning"
	KindCollision       ErrorKind = "collision"
	KindAbsent          ErrorKind = "absent"
	KindConflict        ErrorKind = "conflict"
	KindPostcondition   ErrorKind = "postcondition"
	KindLocalOnly       ErrorKind = "local_only"
	KindRemoteBinding   ErrorKind = "remote_binding"
)

// ProviderError carries a category without retaining untrusted gh output.
type ProviderError struct {
	Kind ErrorKind
	Err  error
}

func (e *ProviderError) Error() string { return "provider operation failed: " + string(e.Kind) }
func (e *ProviderError) Unwrap() error { return e.Err }
func (e *ProviderError) Is(target error) bool {
	return (e.Err != nil && errors.Is(e.Err, target)) || target == sentinelFor(e.Kind)
}
func sentinelFor(k ErrorKind) error {
	switch k {
	case KindOffline:
		return ErrOffline
	case KindAuth:
		return ErrAuth
	case KindTimeout:
		return ErrTimeout
	case KindRateLimit:
		return ErrRateLimit
	case KindNonFastForward:
		return ErrNonFastForward
	case KindProtectedBranch:
		return ErrProtectedBranch
	case KindSecretScanning:
		return ErrSecretScanning
	case KindCollision:
		return ErrCollision
	case KindAbsent:
		return ErrRefAbsent
	case KindConflict:
		return ErrRefConflict
	case KindPostcondition:
		return ErrPostcondition
	case KindLocalOnly:
		return ErrLocalOnly
	case KindRemoteBinding:
		return ErrRemoteBinding
	}
	return nil
}

type RemoteRequest struct{ Owner, Name, Visibility string }
type PushRequest struct{ Owner, Name, Ref, SHA string }
type CallRecord struct{ Operation, Owner, Name, Visibility, Ref, SHA string }

// SafeProvider remains the create/inspect compatibility port.
type SafeProvider interface {
	Create(context.Context, RemoteRequest) (string, error)
	Inspect(context.Context, RemoteRequest, string) (string, error)
}
type PublicationProvider interface {
	Create(context.Context, RemoteRequest) (string, error)
	Publish(context.Context, PushRequest) error
	ConfirmPush(context.Context, PushRequest) (PushOutcome, error)
}

type PushOutcome string

const (
	PushMissing  PushOutcome = "missing"
	PushPresent  PushOutcome = "present"
	PushConflict PushOutcome = "conflict"
)

type GHRunner interface {
	Run(context.Context, string, ...string) (Result, error)
}
type Result struct {
	Output string
	Err    error
}

// SystemRunner executes one trusted executable directly with an argument
// array. Executable and working-directory paths must already be canonical;
// this prevents PATH lookup, ambient $PWD fallback, and symlink replacement
// from changing the target. Callers should configure separate instances for
// git and gh.
type SystemRunner struct {
	Executable string
	WorkingDir string
	MaxOutput  int
}

// CommandRunner is an explicit alias for callers that prefer the command
// terminology. Git and gh should be configured as separate runners.
type CommandRunner = SystemRunner

var _ GHRunner = SystemRunner{}

func (r SystemRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("command context is required")
	}
	executable, err := canonicalExecutable(r.Executable)
	if err != nil {
		return Result{}, err
	}
	if dir == "" {
		dir = r.WorkingDir
	}
	workingDir, err := canonicalWorkingDir(dir)
	if err != nil {
		return Result{}, err
	}
	max := r.MaxOutput
	if max <= 0 {
		max = maxOutput
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = workingDir
	cmd.Env = controlledCommandEnv()
	out := &boundedOutput{max: max}
	cmd.Stdout = out
	cmd.Stderr = out
	runErr := cmd.Run()
	result := Result{Output: out.buf.String()}
	if out.truncated {
		result.Err = ErrOutputLimit
		return result, ErrOutputLimit
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		result.Err = ctxErr
		return result, ctxErr
	}
	result.Err = runErr
	return result, runErr
}

func canonicalExecutable(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("invalid command executable")
	}
	// Resolve symlinked parent components (for example /var -> /private/var on
	// macOS or 8.3 short names on Windows) but keep the final component
	// untouched, then reject a final-component symlink explicitly.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", errors.New("invalid command executable")
	}
	canon := filepath.Join(parent, filepath.Base(path))
	info, err := os.Lstat(canon)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("invalid command executable")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", errors.New("invalid command executable")
	}
	return canon, nil
}

func canonicalWorkingDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("invalid command working directory")
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return "", errors.New("invalid command working directory")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		return "", errors.New("invalid command working directory")
	}
	canon := filepath.Join(parent, filepath.Base(dir))
	info, err := os.Lstat(canon)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("invalid command working directory")
	}
	return canon, nil
}

func controlledCommandEnv() []string {
	return controlledCommandEnvFrom(os.Environ())
}

func controlledCommandEnvFrom(environ []string) []string {
	out := make([]string, 0, 12)
	for _, item := range environ {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		upper := strings.ToUpper(key)
		if upper == "PATH" || upper == "HOME" || upper == "TMPDIR" || upper == "SYSTEMROOT" || strings.HasPrefix(upper, "LANG") || strings.HasPrefix(upper, "LC_") {
			out = append(out, item)
		}
	}
	// Git must not read or write ambient system/global configuration, invoke
	// credential prompts, or inherit environment-controlled object locations.
	out = append(out,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		// gh reads normal config for its authenticated session, but must not
		// stop for an interactive login prompt.
		"GH_PROMPT_DISABLED=1",
	)
	return out
}

type boundedOutput struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.max < 0 {
		b.max = 0
	}
	remaining := b.max - b.buf.Len()
	if remaining <= 0 {
		b.truncated = len(p) > 0
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

// Pusher can update only one explicit branch from one existing SHA.
type Pusher interface {
	Push(context.Context, string, string, string) error
}
type GitPusher struct {
	Runner         GHRunner
	Dir            string
	AllowedRemotes map[string]string
}

func (p GitPusher) Push(ctx context.Context, remote, sha, ref string) error {
	if !validRemote(remote) || !validSHA(sha) || !validRef(ref) {
		return errors.New("invalid push intent")
	}
	if p.Runner == nil {
		return ErrUnsupportedPush
	}
	alias, ok := p.AllowedRemotes[remote]
	if !ok || !validGitRemoteAlias(alias) {
		return remoteBindingError()
	}
	res, runErr := p.Runner.Run(ctx, p.Dir, safeGitArgs("remote", "get-url", "--push", "--", alias)...)
	if len(res.Output) > maxOutput {
		return ErrOutputLimit
	}
	if runErr != nil || res.Err != nil {
		return classifyFailure(runErrOrResult(runErr, res.Err), res.Output)
	}
	if !canonicalGitHubRemoteMatches(res.Output, remote) {
		return remoteBindingError()
	}
	args, err := exactPushArgs(alias, sha, ref)
	if err != nil {
		return err
	}
	res, runErr = p.Runner.Run(ctx, p.Dir, safeGitArgs(args...)...)
	if runErr != nil || res.Err != nil {
		return classifyFailure(runErrOrResult(runErr, res.Err), res.Output)
	}
	if len(res.Output) > maxOutput {
		return ErrOutputLimit
	}
	return nil
}

func safeGitArgs(args ...string) []string {
	return append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.sshCommand=", "-c", "credential.helper="}, args...)
}

const maxOutput = 1 << 20

type GH struct {
	Runner      GHRunner
	Pusher      Pusher
	VerifyOwner bool
}

var _ Provider = GH{}

func (g GH) Create(ctx context.Context, r RemoteRequest) (string, error) {
	if err := validIdentity(r); err != nil {
		return "", err
	}
	if g.Runner == nil {
		return "", errors.New("provider runner is required")
	}
	if g.VerifyOwner {
		res, err := g.run(ctx, "api", "user", "--jq", ".login")
		if err != nil {
			return "", err
		}
		if parseGHString(res.Output) != r.Owner {
			return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
		}
	}
	visibility := "--private"
	if r.Visibility == "public" {
		visibility = "--public"
	}
	if _, err := g.run(ctx, "repo", "create", r.Owner+"/"+r.Name, visibility); err != nil {
		return "", err
	}
	full, err := g.run(ctx, "api", "repos/"+r.Owner+"/"+r.Name, "--jq", ".full_name")
	if err != nil {
		return "", err
	}
	if parseGHString(full.Output) != r.Owner+"/"+r.Name {
		return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	vis, err := g.run(ctx, "api", "repos/"+r.Owner+"/"+r.Name, "--jq", ".visibility")
	if err != nil {
		return "", err
	}
	if parseGHString(vis.Output) != r.Visibility {
		return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return r.Owner + "/" + r.Name, nil
}
func (g GH) EnsureRemote(ctx context.Context, owner, name, visibility string) (string, error) {
	return g.Create(ctx, RemoteRequest{Owner: owner, Name: name, Visibility: visibility})
}

// InspectRef adapts the legacy remote-string port while retaining the strict
// owner/name/ref validation used by Inspect.
func (g GH) InspectRef(ctx context.Context, remote, ref string) (string, error) {
	if !validRemote(remote) {
		return "", errors.New("invalid remote identity")
	}
	parts := strings.Split(remote, "/")
	return g.Inspect(ctx, RemoteRequest{Owner: parts[0], Name: parts[1], Visibility: "private"}, ref)
}

// Push is retained for old callers. Publish also verifies the remote SHA.
func (g GH) Push(ctx context.Context, remote, sha, ref string) error {
	if !validRemote(remote) || !validSHA(sha) || !validRef(ref) {
		return errors.New("invalid push intent")
	}
	parts := strings.Split(remote, "/")
	request := PushRequest{Owner: parts[0], Name: parts[1], Ref: ref, SHA: sha}
	outcome, err := g.ConfirmPush(ctx, request)
	if err != nil {
		return err
	}
	if outcome == PushPresent {
		return nil
	}
	if outcome == PushConflict {
		return ErrRefConflict
	}
	if err := g.pushDirect(ctx, remote, sha, ref); err != nil {
		return err
	}
	outcome, err = g.ConfirmPush(ctx, request)
	if err != nil {
		return err
	}
	if outcome != PushPresent {
		return &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return nil
}
func (g GH) pushDirect(ctx context.Context, remote, sha, ref string) error {
	if g.Pusher == nil {
		return ErrUnsupportedPush
	}
	return g.Pusher.Push(ctx, remote, sha, ref)
}
func (g GH) Publish(ctx context.Context, r PushRequest) error {
	if err := validPushRequest(r); err != nil {
		return err
	}
	outcome, err := g.ConfirmPush(ctx, r)
	if err != nil {
		return err
	}
	if outcome == PushPresent {
		return nil
	}
	if outcome == PushConflict {
		return ErrRefConflict
	}
	if err := g.pushDirect(ctx, r.Owner+"/"+r.Name, r.SHA, r.Ref); err != nil {
		return err
	}
	outcome, err = g.ConfirmPush(ctx, r)
	if err != nil {
		return err
	}
	if outcome != PushPresent {
		return &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return nil
}
func (g GH) ConfirmPush(ctx context.Context, r PushRequest) (PushOutcome, error) {
	if err := validPushRequest(r); err != nil {
		return "", err
	}
	sha, err := g.Inspect(ctx, RemoteRequest{Owner: r.Owner, Name: r.Name, Visibility: "private"}, r.Ref)
	if errors.Is(err, ErrRefAbsent) {
		return PushMissing, nil
	}
	if err != nil {
		return "", err
	}
	if sha != r.SHA {
		return PushConflict, ErrRefConflict
	}
	return PushPresent, nil
}
func (g GH) Inspect(ctx context.Context, r RemoteRequest, ref string) (string, error) {
	if err := validIdentity(r); err != nil {
		return "", err
	}
	if !validRef(ref) {
		return "", errors.New("invalid ref")
	}
	if g.Runner == nil {
		return "", errors.New("provider runner is required")
	}
	res, runErr := g.Runner.Run(ctx, "", "api", "repos/"+r.Owner+"/"+r.Name+"/git/ref/heads/"+url.PathEscape(ref), "--jq", ".object.sha")
	if len(res.Output) > maxOutput {
		return "", ErrOutputLimit
	}
	if runErr != nil || res.Err != nil {
		cause := runErrOrResult(runErr, res.Err)
		if isNotFound(cause, res.Output) {
			return "", &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
		}
		return "", classifyFailure(cause, res.Output)
	}
	if isNotFound(nil, res.Output) {
		return "", &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
	}
	sha := parseGHString(res.Output)
	if sha == "" {
		return "", &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
	}
	if !validSHA(sha) {
		return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return sha, nil
}
func (g GH) run(ctx context.Context, args ...string) (Result, error) {
	res, err := g.Runner.Run(ctx, "", args...)
	if len(res.Output) > maxOutput {
		return Result{}, ErrOutputLimit
	}
	if err != nil || res.Err != nil {
		return Result{}, classifyFailure(runErrOrResult(err, res.Err), res.Output)
	}
	return res, nil
}
func parseGHString(output string) string {
	trimmed := strings.TrimSpace(output)
	var value string
	if json.Unmarshal([]byte(trimmed), &value) == nil {
		return value
	}
	return strings.Trim(trimmed, "\"")
}
func runErrOrResult(runErr, resultErr error) error {
	if resultErr != nil {
		return resultErr
	}
	return runErr
}
func classifyFailure(cause error, output string) error {
	if cause == nil {
		return nil
	}
	if errors.Is(cause, ErrOutputLimit) {
		return ErrOutputLimit
	}
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled) {
		return &ProviderError{Kind: KindTimeout, Err: ErrTimeout}
	}
	text := strings.ToLower(cause.Error() + " " + output)
	switch {
	case strings.Contains(text, "secret scanning") || strings.Contains(text, "push protection"):
		return &ProviderError{Kind: KindSecretScanning, Err: ErrSecretScanning}
	case strings.Contains(text, "protected branch") || strings.Contains(text, "branch protection"):
		return &ProviderError{Kind: KindProtectedBranch, Err: ErrProtectedBranch}
	case strings.Contains(text, "non-fast-forward") || strings.Contains(text, "non fast forward"):
		return &ProviderError{Kind: KindNonFastForward, Err: ErrNonFastForward}
	case strings.Contains(text, "rate limit"):
		return &ProviderError{Kind: KindRateLimit, Err: ErrRateLimit}
	case strings.Contains(text, "timeout"):
		return &ProviderError{Kind: KindTimeout, Err: ErrTimeout}
	case strings.Contains(text, "auth") || strings.Contains(text, "credential") || strings.Contains(text, "not logged in") || strings.Contains(text, "unauthorized"):
		return &ProviderError{Kind: KindAuth, Err: ErrAuth}
	case strings.Contains(text, "already exists") || strings.Contains(text, "name exists"):
		return &ProviderError{Kind: KindCollision, Err: ErrCollision}
	case strings.Contains(text, "network") || strings.Contains(text, "offline") || strings.Contains(text, "connection") || strings.Contains(text, "dns") || strings.Contains(text, "dial tcp") || strings.Contains(text, "could not resolve host"):
		return &ProviderError{Kind: KindOffline, Err: ErrOffline}
	default:
		return &ProviderError{Kind: KindPostcondition, Err: cause}
	}
}

func isNotFound(cause error, output string) bool {
	text := strings.ToLower(output)
	if cause != nil {
		text += " " + strings.ToLower(cause.Error())
	}
	return strings.Contains(text, "404") || strings.Contains(text, "not found") || strings.Contains(text, "ref does not exist")
}

// SafeFake is deterministic and network-free. Failures are consumed FIFO by
// operation name: create, inspect, push, or confirm.
type SafeFake struct {
	mu       sync.Mutex
	remotes  map[string]string
	refs     map[string]string
	calls    []CallRecord
	failures map[string][]error
}

func NewSafeFake() *SafeFake {
	return &SafeFake{remotes: map[string]string{}, refs: map[string]string{}, failures: map[string][]error{}}
}
func (f *SafeFake) FailNext(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = append(f.failures[op], err)
}
func (f *SafeFake) Calls() []CallRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CallRecord(nil), f.calls...)
}
func (f *SafeFake) Add(remote, visibility string) {
	if !validRemote(remote) || (visibility != "private" && visibility != "public") {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remotes[remote] = visibility
}
func (f *SafeFake) SetRef(owner, name, ref, sha string) {
	if validRemote(owner+"/"+name) && validRef(ref) && validSHA(sha) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.refs[owner+"/"+name+"#"+ref] = sha
	}
}
func (f *SafeFake) Create(_ context.Context, r RemoteRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validIdentity(r); err != nil {
		return "", err
	}
	remote := r.Owner + "/" + r.Name
	f.calls = append(f.calls, CallRecord{Operation: "create", Owner: r.Owner, Name: r.Name, Visibility: r.Visibility})
	if err := f.fail("create"); err != nil {
		return "", err
	}
	if _, ok := f.remotes[remote]; ok {
		return "", ErrCollision
	}
	f.remotes[remote] = r.Visibility
	return remote, nil
}
func (f *SafeFake) Inspect(_ context.Context, r RemoteRequest, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validIdentity(r); err != nil || !validRef(ref) {
		return "", errors.New("invalid inspection request")
	}
	f.calls = append(f.calls, CallRecord{Operation: "inspect", Owner: r.Owner, Name: r.Name, Ref: ref})
	if err := f.fail("inspect"); err != nil {
		return "", err
	}
	v, ok := f.refs[r.Owner+"/"+r.Name+"#"+ref]
	if !ok {
		return "", ErrRefAbsent
	}
	return v, nil
}
func (f *SafeFake) Push(_ context.Context, r PushRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, CallRecord{Operation: "push", Owner: r.Owner, Name: r.Name, Ref: r.Ref, SHA: r.SHA})
	if err := f.fail("push"); err != nil {
		return err
	}
	if err := validPushRequest(r); err != nil {
		return err
	}
	if _, ok := f.remotes[r.Owner+"/"+r.Name]; !ok {
		return errors.New("unknown remote")
	}
	if existing, ok := f.refs[r.Owner+"/"+r.Name+"#"+r.Ref]; ok {
		if existing != r.SHA {
			return ErrRefConflict
		}
		return nil
	}
	f.refs[r.Owner+"/"+r.Name+"#"+r.Ref] = r.SHA
	return nil
}
func (f *SafeFake) ConfirmPush(ctx context.Context, r PushRequest) (PushOutcome, error) {
	if err := validPushRequest(r); err != nil {
		return "", err
	}
	f.mu.Lock()
	if err := f.fail("confirm"); err != nil {
		f.mu.Unlock()
		return "", err
	}
	f.mu.Unlock()
	sha, err := f.Inspect(ctx, RemoteRequest{Owner: r.Owner, Name: r.Name, Visibility: "private"}, r.Ref)
	if errors.Is(err, ErrRefAbsent) {
		return PushMissing, nil
	}
	if err != nil {
		return "", err
	}
	if sha != r.SHA {
		return PushConflict, ErrRefConflict
	}
	return PushPresent, nil
}
func (f *SafeFake) Publish(ctx context.Context, r PushRequest) error {
	outcome, err := f.ConfirmPush(ctx, r)
	if err != nil {
		return err
	}
	if outcome == PushPresent {
		return nil
	}
	if outcome == PushConflict {
		return ErrRefConflict
	}
	if err := f.Push(ctx, r); err != nil {
		return err
	}
	outcome, err = f.ConfirmPush(ctx, r)
	if err != nil {
		return err
	}
	if outcome != PushPresent {
		return &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return nil
}
func (f *SafeFake) fail(op string) error {
	q := f.failures[op]
	if len(q) == 0 {
		return nil
	}
	e := q[0]
	f.failures[op] = q[1:]
	return e
}

func validIdentity(r RemoteRequest) error {
	if !valid(r.Owner) || !valid(r.Name) || (r.Visibility != "private" && r.Visibility != "public") {
		return fmt.Errorf("invalid remote identity")
	}
	return nil
}

var ident = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func validRef(s string) bool {
	if len(s) == 0 || len(s) > 255 || s[0] == '-' || s == "HEAD" || !refRE.MatchString(s) {
		return false
	}
	if strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.Contains(s, "//") || strings.HasPrefix(s, "refs/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, component := range strings.Split(s, "/") {
		if component == "" || component == "." || component == ".." || strings.HasPrefix(component, ".") || strings.HasSuffix(strings.ToLower(component), ".lock") {
			return false
		}
	}
	return valid(s) || strings.Contains(s, "/")
}
func validPushRequest(r PushRequest) error {
	if err := validIdentity(RemoteRequest{Owner: r.Owner, Name: r.Name, Visibility: "private"}); err != nil {
		return err
	}
	if !validRef(r.Ref) || !validSHA(r.SHA) {
		return errors.New("invalid push request")
	}
	return nil
}
func exactPushArgs(remote, sha, ref string) ([]string, error) {
	if !validGitRemoteAlias(remote) || !validSHA(sha) || !validRef(ref) {
		return nil, errors.New("invalid push destination")
	}
	return []string{"push", "--", remote, sha + ":refs/heads/" + ref}, nil
}

// A local remote alias is a single Git config name, never a path or URL. It
// is passed only after `--` to direct git argv calls.
func validGitRemoteAlias(alias string) bool {
	if alias == "" || len(alias) > 100 || alias[0] == '-' || alias == "." || alias == ".." || strings.Contains(alias, "..") {
		return false
	}
	return ident.MatchString(alias)
}

func remoteBindingError() error {
	return &ProviderError{Kind: KindRemoteBinding, Err: ErrRemoteBinding}
}

// canonicalGitHubRemoteMatches accepts exactly one git remote URL line. The
// complete string comparison intentionally rejects credentials, query and
// fragment data, alternate hosts/schemes, scp variants, and extra lines.
func canonicalGitHubRemoteMatches(output, identity string) bool {
	if strings.HasSuffix(output, "\n") {
		output = strings.TrimSuffix(output, "\n")
	}
	if output == "" || strings.ContainsAny(output, "\r\n") {
		return false
	}
	return output == "https://github.com/"+identity ||
		output == "https://github.com/"+identity+".git" ||
		output == "git@github.com:"+identity ||
		output == "git@github.com:"+identity+".git"
}

// LocalOnlyProvider is a policy boundary. It fails before touching the inner
// implementation, proving local-only cannot accidentally contact a provider.
type LocalOnlyProvider struct{ inner any }

func NewLocalOnlyProvider(inner any) *LocalOnlyProvider { return &LocalOnlyProvider{inner: inner} }
func NewLocalOnly(inner any) *LocalOnlyProvider         { return NewLocalOnlyProvider(inner) }
func (p *LocalOnlyProvider) Create(context.Context, RemoteRequest) (string, error) {
	return "", ErrLocalOnly
}
func (p *LocalOnlyProvider) Inspect(context.Context, RemoteRequest, string) (string, error) {
	return "", ErrLocalOnly
}
func (p *LocalOnlyProvider) Push(context.Context, PushRequest) error { return ErrLocalOnly }
func (p *LocalOnlyProvider) ConfirmPush(context.Context, PushRequest) (PushOutcome, error) {
	return PushMissing, ErrLocalOnly
}
func (p *LocalOnlyProvider) Publish(context.Context, PushRequest) error { return ErrLocalOnly }
