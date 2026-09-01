package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// TrustedVerifierSpec is trusted repository configuration. Adapter payloads
// cannot construct or select one of these specifications.
type TrustedVerifierSpec struct {
	Name             string
	Version          string
	Argv             []string
	Applicable       bool
	Timeout          time.Duration
	MaxOutput        int
	Environment      map[string]string
	ExecutableDigest string
}

// VerificationPolicy is the repository's effective verification policy.
type VerificationPolicy struct {
	Visibility        string
	LocalOnly         bool
	AllowNoVerifier   bool
	RequiredVerifiers []string
}

type VerifierRegistry struct {
	Specs              []TrustedVerifierSpec
	VerifierSetVersion string
	VerifierSetDigest  string
	ConfigDigest       string
	specs              []TrustedVerifierSpec
}

// TrustedRegistry and VerificationRequest/Evidence are descriptive aliases
// for callers that prefer the security boundary to be explicit in names.
type TrustedRegistry = VerifierRegistry
type VerificationRequest = TrustedRequest
type VerificationEvidence = TrustedEvidence

// NewVerifierRegistry validates and freezes a canonical trusted verifier set.
func NewVerifierRegistry(specs []TrustedVerifierSpec) (*VerifierRegistry, error) {
	copySpecs := make([]TrustedVerifierSpec, len(specs))
	seen := make(map[string]bool, len(specs))
	for i, in := range specs {
		if in.Name == "" || !verifierNameRE.MatchString(in.Name) || seen[in.Name] {
			return nil, fmt.Errorf("invalid or duplicate verifier name %q", in.Name)
		}
		seen[in.Name] = true
		if in.Version == "" || len(in.Argv) == 0 {
			return nil, fmt.Errorf("verifier %q has incomplete trusted specification", in.Name)
		}
		if err := trustedArgv(in.Argv); err != nil {
			return nil, fmt.Errorf("verifier %q: %w", in.Name, err)
		}
		if !filepath.IsAbs(in.Argv[0]) {
			return nil, fmt.Errorf("verifier %q: executable must be absolute", in.Name)
		}
		if err := validateEnvironment(in.Environment); err != nil {
			return nil, fmt.Errorf("verifier %q: %w", in.Name, err)
		}
		if err := rejectFinalSymlink(in.Argv[0]); err != nil {
			return nil, fmt.Errorf("verifier %q: %w", in.Name, err)
		}
		if digest, err := executableFingerprint(in.Argv[0]); err == nil {
			in.ExecutableDigest = digest
		}
		in.Argv = append([]string(nil), in.Argv...)
		in.Environment = cloneEnvironment(in.Environment)
		copySpecs[i] = in
	}
	sort.Slice(copySpecs, func(i, j int) bool { return copySpecs[i].Name < copySpecs[j].Name })
	const version = "1"
	r := &VerifierRegistry{Specs: cloneSpecs(copySpecs), specs: cloneSpecs(copySpecs), VerifierSetVersion: version}
	r.ConfigDigest = digestCanonical(registryCanonical{Version: version, Specs: canonicalSpecs(copySpecs)})
	r.VerifierSetDigest = digestCanonical(verifierSetCanonical{Version: version, Specs: canonicalSpecs(copySpecs)})
	return r, nil
}

// NewRegistry is a short alias for NewVerifierRegistry.
func NewRegistry(specs []TrustedVerifierSpec) (*VerifierRegistry, error) {
	return NewVerifierRegistry(specs)
}

type VerificationPlan struct {
	Specs              []TrustedVerifierSpec
	VerifierSetVersion string
	VerifierSetDigest  string
	ConfigDigest       string
}

func (r *VerifierRegistry) Select(policy VerificationPolicy) (VerificationPlan, error) {
	if r == nil {
		return VerificationPlan{}, errors.New("verifier registry is required")
	}
	if err := validateVerificationPolicy(policy); err != nil {
		return VerificationPlan{}, err
	}
	trustedSpecs := r.specs
	if trustedSpecs == nil {
		trustedSpecs = r.Specs
	}
	required := make(map[string]bool, len(policy.RequiredVerifiers))
	for _, name := range policy.RequiredVerifiers {
		if name == "" {
			return VerificationPlan{}, errors.New("empty required verifier")
		}
		if required[name] {
			return VerificationPlan{}, fmt.Errorf("duplicate required verifier %q", name)
		}
		required[name] = true
	}
	if len(required) > 0 {
		for name := range required {
			found := false
			for _, spec := range trustedSpecs {
				if spec.Name == name && spec.Applicable {
					found = true
					break
				}
			}
			if !found {
				return VerificationPlan{}, fmt.Errorf("required verifier %q is not configured and applicable", name)
			}
		}
	}
	selected := make([]TrustedVerifierSpec, 0, len(trustedSpecs))
	for _, spec := range trustedSpecs {
		if !spec.Applicable || len(required) > 0 && !required[spec.Name] {
			continue
		}
		selected = append(selected, cloneSpec(spec))
	}
	return VerificationPlan{Specs: selected, VerifierSetVersion: r.VerifierSetVersion, VerifierSetDigest: r.VerifierSetDigest, ConfigDigest: r.ConfigDigest}, nil
}

type TrustedRequest struct {
	CandidateDigest string
	BaseDigest      string
	PolicyDigest    string
	GuardDigest     string
	Dir             string
	OverallTimeout  time.Duration
}

type Decision string

const (
	DecisionPassed     Decision = "passed"
	DecisionFailed     Decision = "failed"
	DecisionNoVerifier Decision = "no_verifier"
	DecisionCancelled  Decision = "cancelled"
)

type Reason string

const (
	ReasonNone                 Reason = ""
	ReasonVerifierFailure      Reason = "verifier_failed"
	ReasonNoVerifierPublic     Reason = "no_configured_verifier_public"
	ReasonNoVerifierLocal      Reason = "no_verifier_explicit_local_exception"
	ReasonNoVerifierRequired   Reason = "no_configured_verifier"
	ReasonVerificationCanceled Reason = "verification_cancelled"
	ReasonVerificationTimeout  Reason = "verification_timeout"
	ReasonExecutableInvalid    Reason = "trusted_executable_invalid"
)

type TrustedEvidence struct {
	Verifier          string
	VerifierVersion   string
	CandidateDigest   string
	BaseDigest        string
	PolicyDigest      string
	GuardDigest       string
	VerifierSetDigest string
	ExitCode          int
	Passed            bool
	TimedOut          bool
	Cancelled         bool
	OutputTruncated   bool
	StdoutBytes       int
	StderrBytes       int
	StdoutDigest      string
	StderrDigest      string
	EvidenceDigest    string
}

func (e TrustedEvidence) ValidForTrusted(candidate, base, policy, guard, verifierSet string) bool {
	return e.Passed && !e.TimedOut && !e.Cancelled && e.CandidateDigest == candidate && e.BaseDigest == base && e.PolicyDigest == policy && e.GuardDigest == guard && e.VerifierSetDigest == verifierSet
}

type VerificationResult struct {
	Decision           Decision
	Reason             Reason
	Passed             bool
	VerifierSetVersion string
	VerifierSetDigest  string
	ConfigDigest       string
	Evidence           []TrustedEvidence
	EvidenceDigest     string
	FailureMetadata    string
}

func (r VerificationResult) ValidFor(req TrustedRequest, policy VerificationPolicy, registry *VerifierRegistry) bool {
	if registry == nil || r.Decision != DecisionPassed || !r.Passed || r.VerifierSetVersion != registry.VerifierSetVersion || r.VerifierSetDigest != registry.VerifierSetDigest || r.ConfigDigest != registry.ConfigDigest || len(r.Evidence) == 0 {
		return false
	}
	plan, err := registry.Select(policy)
	if err != nil || plan.VerifierSetVersion != r.VerifierSetVersion || plan.VerifierSetDigest != r.VerifierSetDigest || plan.ConfigDigest != r.ConfigDigest || len(plan.Specs) != len(r.Evidence) {
		return false
	}
	for i, e := range r.Evidence {
		if e.Verifier != plan.Specs[i].Name || e.EvidenceDigest != digestCanonical(evidenceWithoutDigest(e)) || !e.ValidForTrusted(req.CandidateDigest, req.BaseDigest, req.PolicyDigest, req.GuardDigest, r.VerifierSetDigest) {
			return false
		}
	}
	return r.EvidenceDigest == digestCanonical(r.Evidence)
}

// Verify executes only the immutable, registry-selected argv vectors.
func (r *VerifierRegistry) Verify(parent context.Context, policy VerificationPolicy, req TrustedRequest, runner Runner) (VerificationResult, error) {
	if runner == nil {
		return VerificationResult{}, errors.New("verification runner is required")
	}
	if err := validateVerificationPolicy(policy); err != nil {
		return VerificationResult{}, err
	}
	if err := validateTrustedRequest(req); err != nil {
		return VerificationResult{}, err
	}
	plan, err := r.Select(policy)
	if err != nil {
		return VerificationResult{}, err
	}
	if len(plan.Specs) == 0 {
		result := VerificationResult{Decision: DecisionNoVerifier, VerifierSetVersion: plan.VerifierSetVersion, VerifierSetDigest: plan.VerifierSetDigest, ConfigDigest: plan.ConfigDigest}
		if isPublicPolicy(policy) {
			result.Reason = ReasonNoVerifierPublic
		} else if policy.AllowNoVerifier {
			result.Reason = ReasonNoVerifierLocal
		} else {
			result.Decision = DecisionFailed
			result.Reason = ReasonNoVerifierRequired
		}
		return result, nil
	}
	overall := req.OverallTimeout
	if overall <= 0 {
		overall = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, overall)
	defer cancel()
	result := VerificationResult{Decision: DecisionPassed, Passed: true, VerifierSetVersion: plan.VerifierSetVersion, VerifierSetDigest: plan.VerifierSetDigest, ConfigDigest: plan.ConfigDigest}
	for _, spec := range plan.Specs {
		e, runErr := runTrustedOne(ctx, spec, req, runner)
		e.VerifierSetDigest = plan.VerifierSetDigest
		e.EvidenceDigest = digestCanonical(e)
		result.Evidence = append(result.Evidence, e)
		if runErr != nil || !e.Passed {
			result.Passed = false
			result.Decision = DecisionFailed
			result.Reason = ReasonVerifierFailure
			if errors.Is(runErr, errTrustedExecutable) {
				result.Reason = ReasonExecutableInvalid
			} else if e.TimedOut {
				result.Reason = ReasonVerificationTimeout
			} else if e.Cancelled {
				result.Reason = ReasonVerificationCanceled
			}
			result.FailureMetadata = appendFailureMetadata(result.FailureMetadata, e, runErr)
		}
		if ctx.Err() != nil {
			result.Passed = false
			result.Decision = DecisionCancelled
			result.Reason = ReasonVerificationCanceled
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				result.Reason = ReasonVerificationTimeout
			}
			break
		}
	}
	result.EvidenceDigest = digestCanonical(result.Evidence)
	if result.Decision == DecisionCancelled {
		return result, ctx.Err()
	}
	return result, nil
}

func runTrustedOne(ctx context.Context, spec TrustedVerifierSpec, req TrustedRequest, runner Runner) (TrustedEvidence, error) {
	e := TrustedEvidence{Verifier: spec.Name, VerifierVersion: spec.Version, CandidateDigest: req.CandidateDigest, BaseDigest: req.BaseDigest, PolicyDigest: req.PolicyDigest, GuardDigest: req.GuardDigest}
	// The executable is checked again at execution time to close replacement and
	// symlink races between registry construction and verification.
	canonical, err := canonicalTrustedExecutable(spec.Argv[0])
	if err != nil {
		return e, fmt.Errorf("%w: %v", errTrustedExecutable, err)
	}
	if spec.ExecutableDigest != "" {
		current, fingerprintErr := executableFingerprint(canonical)
		if fingerprintErr != nil || current != spec.ExecutableDigest {
			return e, fmt.Errorf("%w: executable identity changed", errTrustedExecutable)
		}
	}
	e.VerifierSetDigest = "pending"
	commandTimeout := spec.Timeout
	if commandTimeout <= 0 {
		commandTimeout = 2 * time.Minute
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	env := controlledEnvironment(spec, canonical)
	max := spec.MaxOutput
	if max <= 0 {
		max = 1 << 20
	}
	type runResponse struct {
		result Result
		err    error
	}
	response := make(chan runResponse, 1)
	go func() {
		var res Result
		var runErr error
		if br, ok := runner.(boundedRunner); ok {
			res, runErr = br.RunBounded(commandCtx, req.Dir, env, max, append([]string(nil), spec.Argv...)...)
		} else {
			res, runErr = runner.Run(commandCtx, req.Dir, env, append([]string(nil), spec.Argv...)...)
		}
		response <- runResponse{result: res, err: runErr}
	}()
	select {
	case completed := <-response:
		res, runErr := completed.result, completed.err
		e.ExitCode = res.ExitCode
		e.Passed = runErr == nil && res.ExitCode == 0
		e.TimedOut = errors.Is(commandCtx.Err(), context.DeadlineExceeded)
		e.Cancelled = errors.Is(commandCtx.Err(), context.Canceled)
		e.OutputTruncated = len(res.Stdout) > max || len(res.Stderr) > max || outputLimitError(runErr)
		e.StdoutBytes, e.StderrBytes = minInt(len(res.Stdout), max), minInt(len(res.Stderr), max)
		e.StdoutDigest, e.StderrDigest = outputDigest(res.Stdout), outputDigest(res.Stderr)
		if e.OutputTruncated {
			e.Passed = false
			runErr = errors.New("verification output exceeded limit")
		}
		return e, runErr
	case <-commandCtx.Done():
		e.TimedOut = errors.Is(commandCtx.Err(), context.DeadlineExceeded)
		e.Cancelled = errors.Is(commandCtx.Err(), context.Canceled)
		return e, commandCtx.Err()
	}
}

func validateTrustedRequest(req TrustedRequest) error {
	for name, value := range map[string]string{"candidate": req.CandidateDigest, "base": req.BaseDigest, "policy": req.PolicyDigest, "guard": req.GuardDigest} {
		if !digestRE.MatchString(value) {
			return fmt.Errorf("invalid %s evidence digest", name)
		}
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return errors.New("candidate working directory must be an explicit absolute path")
	}
	clean := filepath.Clean(req.Dir)
	// Resolve symlinked parent components (for example /var -> /private/var on
	// macOS or 8.3 short names on Windows) but keep the final component
	// untouched, then reject a final-component symlink explicitly.
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return errors.New("candidate working directory must be an existing directory")
	}
	canon := filepath.Join(parent, filepath.Base(clean))
	st, err := os.Lstat(canon)
	if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return errors.New("candidate working directory must be an existing directory")
	}
	return nil
}

func isPublicPolicy(p VerificationPolicy) bool {
	return !p.LocalOnly && strings.EqualFold(p.Visibility, "public")
}

func validateVerificationPolicy(p VerificationPolicy) error {
	switch strings.ToLower(p.Visibility) {
	case "", "local", "private", "public":
	default:
		return fmt.Errorf("invalid verification visibility %q", p.Visibility)
	}
	if p.LocalOnly && strings.EqualFold(p.Visibility, "public") {
		return errors.New("public verification cannot be local-only")
	}
	return nil
}

var verifierNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var errTrustedExecutable = errors.New("trusted executable invalid")

func validateEnvironment(env map[string]string) error {
	for k, v := range env {
		if !envNameRE.MatchString(k) || strings.ContainsAny(v, "\x00\r\n") || unsafeEnvironmentName(k) || k == "PATH" || k == "LANG" || k == "LC_ALL" || strings.HasPrefix(k, "GIT_") {
			return fmt.Errorf("unsafe verifier environment variable %q", k)
		}
	}
	return nil
}

func unsafeEnvironmentName(k string) bool {
	u := strings.ToUpper(k)
	for _, term := range []string{"SECRET", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE_KEY", "AUTH", "SSH_", "AWS_", "GIT_CONFIG", "GIT_SSH", "HOME", "USERPROFILE", "XDG_"} {
		if strings.Contains(u, term) {
			return true
		}
	}
	return false
}

func controlledEnvironment(spec TrustedVerifierSpec, executable string) map[string]string {
	env := map[string]string{
		"PATH":                filepath.Dir(executable),
		"LANG":                "C",
		"LC_ALL":              "C",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
	}
	for k, v := range spec.Environment {
		env[k] = v
	}
	return env
}

func canonicalTrustedExecutable(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("executable must be absolute and clean")
	}
	canon, err := resolveFinalComponent(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(canon)
	if err != nil || !st.Mode().IsRegular() {
		if err == nil {
			err = errors.New("executable is not a regular executable file")
		}
		return "", err
	}
	if runtime.GOOS != "windows" && st.Mode()&0111 == 0 {
		return "", errors.New("executable is not a regular executable file")
	}
	return canon, nil
}

// resolveFinalComponent resolves symlinked parent components of path but keeps
// the final component untouched, then rejects a final-component symlink. This
// allows canonical absolute paths under symlinked roots (for example /var ->
// /private/var on macOS or 8.3 short names on Windows) while still refusing
// executable paths whose final component is a symlink.
func resolveFinalComponent(path string) (string, error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	canon := filepath.Join(parent, filepath.Base(path))
	st, err := os.Lstat(canon)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("executable symlink is not trusted")
	}
	return canon, nil
}

// rejectFinalSymlink reports whether path is a symlink or has an unresolvable
// parent. It mirrors resolveFinalComponent but returns nil when the path is a
// regular file so registry construction can tolerate missing files.
func rejectFinalSymlink(path string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil
	}
	canon := filepath.Join(parent, filepath.Base(path))
	st, err := os.Lstat(canon)
	if err != nil {
		return nil
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return errors.New("executable symlink is not trusted")
	}
	return nil
}

func appendFailureMetadata(existing string, e TrustedEvidence, runErr error) string {
	category := "exit"
	if e.TimedOut {
		category = "timeout"
	} else if e.Cancelled {
		category = "cancelled"
	} else if e.OutputTruncated {
		category = "output_limit"
	} else if runErr != nil {
		category = "execution"
	}
	item := fmt.Sprintf("verifier=%s;category=%s;exit_code=%d;stdout_bytes=%d;stderr_bytes=%d;stdout_digest=%s;stderr_digest=%s", e.Verifier, category, e.ExitCode, e.StdoutBytes, e.StderrBytes, e.StdoutDigest, e.StderrDigest)
	if existing != "" {
		return existing + "|" + item
	}
	return item
}

func outputDigest(s string) string { return digestCanonical(s) }

func outputLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "output") && strings.Contains(s, "limit")
}

type canonicalSpec struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Argv             []string          `json:"argv"`
	Applicable       bool              `json:"applicable"`
	Timeout          int64             `json:"timeout_ns"`
	MaxOutput        int               `json:"max_output"`
	Environment      map[string]string `json:"environment,omitempty"`
	ExecutableDigest string            `json:"executable_digest,omitempty"`
}
type registryCanonical struct {
	Version string          `json:"version"`
	Specs   []canonicalSpec `json:"specs"`
}
type verifierSetCanonical struct {
	Version string          `json:"version"`
	Specs   []canonicalSpec `json:"specs"`
}

func canonicalSpecs(specs []TrustedVerifierSpec) []canonicalSpec {
	out := make([]canonicalSpec, len(specs))
	for i, s := range specs {
		out[i] = canonicalSpec{Name: s.Name, Version: s.Version, Argv: append([]string(nil), s.Argv...), Applicable: s.Applicable, Timeout: s.Timeout.Nanoseconds(), MaxOutput: s.MaxOutput, Environment: cloneEnvironment(s.Environment), ExecutableDigest: s.ExecutableDigest}
	}
	return out
}
func digestCanonical(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func cloneEnvironment(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneSpec(in TrustedVerifierSpec) TrustedVerifierSpec {
	in.Argv = append([]string(nil), in.Argv...)
	in.Environment = cloneEnvironment(in.Environment)
	return in
}

func cloneSpecs(in []TrustedVerifierSpec) []TrustedVerifierSpec {
	out := make([]TrustedVerifierSpec, len(in))
	for i, spec := range in {
		out[i] = cloneSpec(spec)
	}
	return out
}

func evidenceWithoutDigest(in TrustedEvidence) TrustedEvidence {
	in.EvidenceDigest = ""
	return in
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func executableFingerprint(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}
