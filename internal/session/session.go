// Package session owns the boundary between a session lifecycle event and a
// repository baseline. It keeps source bytes in memory and persists only the
// bounded identity/digest evidence required for replay.
package session

import (
	"context"
	"errors"
	"os"

	"autogit/internal/policy"
	"autogit/internal/repository"
	"autogit/internal/staging"
	"autogit/internal/verification"
	localworkflow "autogit/internal/workflow"
)

type Request struct {
	SessionID    string
	RepositoryID string
	ClientID     string
	Root         string
	Paths        []string
	IdentityKey  []byte
}

type Store interface {
	RecordSessionBaseline(context.Context, string, string, string, repository.Baseline) error
}

type CaptureFunc func(context.Context, repository.Runner, string) (repository.Baseline, error)

type Service struct {
	Runner  repository.Runner
	Store   Store
	Capture CaptureFunc
}

// Started is the in-memory handoff between a session boundary and its
// completion boundary. The durable store keeps only baseline identity and
// digests; the captured bytes stay here until ownership is resolved.
type Started struct {
	Request  Request
	Baseline repository.Baseline
}

// DurableBaseline is the source-free session evidence retained across CLI
// processes. ResumeFromDurable reconstructs only clean committed paths from
// the recorded HEAD and keeps their bytes in the returned in-memory handoff.
type DurableBaseline struct {
	Head, IndexDigest, StatusDigest, PathsDigest, Evidence string
}

// Workflow is the narrow local-commit boundary used after ownership has been
// derived. Session code cannot bypass the workflow's scan and verification
// gates by receiving a Git implementation directly.
type Workflow interface {
	RunPlan(context.Context, localworkflow.Request, staging.Plan) (localworkflow.Result, error)
}

func New(store Store) Service {
	return Service{Runner: repository.SystemRunner{}, Store: store}
}

// Start captures and durably records the trusted baseline for a session.
// Callers must retain the returned value until Complete; the database is not a
// source-byte store and cannot be used to reconstruct this handoff.
func (s Service) Start(ctx context.Context, req Request) (Started, error) {
	baseline, err := s.CaptureAndRecord(ctx, req)
	if err != nil {
		return Started{}, err
	}
	return Started{Request: req, Baseline: baseline.Clone()}, nil
}

// ResumeFromDurable recreates a session handoff after a process restart. An
// explicit path list uses immutable committed-tree reconstruction; an empty
// path list requires the source-free HMAC baseline manifest so dirty
// pre-existing paths can be compared without recovering their contents.
func (s Service) ResumeFromDurable(ctx context.Context, req Request, durable DurableBaseline) (Started, error) {
	if req.SessionID == "" || req.RepositoryID == "" || req.ClientID == "" || req.Root == "" {
		return Started{}, errors.New("session baseline request is incomplete")
	}
	if len(req.Paths) == 0 && durable.Evidence == "" {
		return Started{}, errors.New("session baseline paths are required")
	}
	if len(req.Paths) == 0 {
		evidence, err := repository.DecodeDurableBaseline(durable.Evidence)
		if err != nil || evidence.PathsDigest != durable.PathsDigest {
			return Started{}, errors.New("durable session baseline evidence is invalid")
		}
		return Started{Request: req, Baseline: repository.Baseline{
			Head: durable.Head, IndexDigest: durable.IndexDigest, StatusDigest: durable.StatusDigest,
			PathsDigest: durable.PathsDigest, DurableEvidence: durable.Evidence,
		}}, nil
	}
	if durable.StatusDigest != repository.EmptyStatusDigest() {
		return Started{}, errors.New("dirty session baseline cannot be resumed safely")
	}
	if durable.PathsDigest != repository.DigestPaths(req.Paths) {
		return Started{}, errors.New("session baseline paths do not match durable evidence")
	}
	files, err := repository.CaptureCommittedFiles(ctx, s.Runner, req.Root, durable.Head, req.Paths, 0)
	if err != nil {
		return Started{}, err
	}
	paths := make([]string, 0, len(req.Paths))
	seen := make(map[string]bool, len(req.Paths))
	for _, name := range req.Paths {
		if seen[name] {
			continue
		}
		seen[name] = true
		paths = append(paths, name)
	}
	return Started{Request: req, Baseline: repository.Baseline{
		Head: durable.Head, IndexDigest: durable.IndexDigest, StatusDigest: durable.StatusDigest,
		PathsDigest: durable.PathsDigest, Paths: paths, Files: files,
	}}, nil
}

// Complete captures the current explicitly requested paths, derives ownership
// against the session-start baseline, and delegates the resulting plan to the
// verified local workflow. No workflow call occurs when ownership is
// ambiguous or the candidate is empty.
func (s Service) Complete(ctx context.Context, started Started, runner Workflow, id, message string, p policy.Policy, verifiers *verification.VerifierRegistry) (localworkflow.Result, error) {
	if runner == nil {
		return localworkflow.Result{}, errors.New("session workflow is required")
	}
	if started.Request.SessionID == "" || started.Request.RepositoryID == "" || started.Request.ClientID == "" || started.Request.Root == "" {
		return localworkflow.Result{}, errors.New("started session is incomplete")
	}
	plan, err := s.BuildOwnedPlanAtCurrent(ctx, started.Request, started.Baseline.Clone())
	if err != nil {
		return localworkflow.Result{}, err
	}
	if len(plan.CandidateSnapshot()) == 0 {
		return localworkflow.Result{}, errors.New("session has no owned changes")
	}
	return runner.RunPlan(ctx, localworkflow.Request{ID: id, RepositoryDir: started.Request.Root, Message: message, Policy: p, Verifiers: verifiers}, plan)
}

// BuildOwnedPlanAtCurrent captures the current repository observation before
// deriving ownership. HEAD and the shared index must still match the session
// baseline; status is allowed to change because it includes the candidate's
// work and is checked path-by-path by staging or by the source-free manifest.
func (s Service) BuildOwnedPlanAtCurrent(ctx context.Context, req Request, baseline repository.Baseline) (staging.Plan, error) {
	if s.Runner == nil {
		return staging.Plan{}, errors.New("owned plan observation runner is required")
	}
	current, err := repository.CaptureBaselineWithOptions(ctx, s.Runner, req.Root, repository.BaselineOptions{Paths: req.Paths})
	if err != nil {
		return staging.Plan{}, err
	}
	if current.Head != baseline.Head {
		return staging.Plan{}, errors.New("repository HEAD changed since session baseline")
	}
	if current.IndexDigest != baseline.IndexDigest {
		return staging.Plan{}, errors.New("shared index changed since session baseline")
	}
	if len(req.Paths) == 0 {
		return s.buildPlanFromDurableEvidence(ctx, req, baseline, current)
	}
	return staging.BuildPlanFromBaselines(baseline, current, req.Paths)
}

func (s Service) CaptureAndRecord(ctx context.Context, req Request) (repository.Baseline, error) {
	if req.SessionID == "" || req.RepositoryID == "" || req.ClientID == "" || req.Root == "" {
		return repository.Baseline{}, errors.New("session baseline request is incomplete")
	}
	if s.Runner == nil || s.Store == nil {
		return repository.Baseline{}, errors.New("session baseline dependencies are required")
	}
	capture := s.Capture
	if capture == nil {
		capture = repository.CaptureBaseline
	}
	var baseline repository.Baseline
	var err error
	if s.Capture == nil && len(req.Paths) > 0 {
		baseline, err = repository.CaptureBaselineWithOptions(ctx, s.Runner, req.Root, repository.BaselineOptions{Paths: req.Paths})
	} else {
		baseline, err = capture(ctx, s.Runner, req.Root)
	}
	if err != nil {
		return repository.Baseline{}, err
	}
	if len(req.IdentityKey) > 0 {
		baseline.DurableEvidence, err = repository.EncodeDurableBaseline(baseline, req.IdentityKey)
		if err != nil {
			return repository.Baseline{}, err
		}
	}
	if err := s.Store.RecordSessionBaseline(ctx, req.SessionID, req.RepositoryID, req.ClientID, baseline); err != nil {
		return repository.Baseline{}, err
	}
	return baseline, nil
}

func (s Service) buildPlanFromDurableEvidence(ctx context.Context, req Request, baseline repository.Baseline, current repository.Baseline) (staging.Plan, error) {
	if len(req.IdentityKey) == 0 || baseline.DurableEvidence == "" {
		return staging.Plan{}, errors.New("durable session baseline identity is unavailable")
	}
	evidence, err := repository.DecodeDurableBaseline(baseline.DurableEvidence)
	if err != nil || evidence.PathsDigest != baseline.PathsDigest {
		return staging.Plan{}, errors.New("durable session baseline evidence is invalid")
	}
	byID := make(map[string]repository.DurableFileEvidence, len(evidence.Files))
	for _, file := range evidence.Files {
		byID[file.PathID] = file
	}
	fingerprints := make(map[string]staging.Fingerprint)
	baselineTracked, err := repository.CaptureCommittedPathPresence(ctx, s.Runner, req.Root, baseline.Head, current.Paths)
	if err != nil {
		return staging.Plan{}, err
	}
	for _, name := range current.Paths {
		pathID, idErr := repository.DurablePathID(req.IdentityKey, name)
		if idErr != nil {
			return staging.Plan{}, idErr
		}
		file, ok := byID[pathID]
		if !ok {
			continue
		}
		fingerprints[name] = staging.Fingerprint{ContentDigest: file.ContentDigest, Mode: os.FileMode(file.Mode), Present: file.Present, Symlink: file.Symlink}
	}
	return staging.BuildPlanFromFingerprints(fingerprints, toObserved(current), current.Paths, baselineTracked)
}

func toObserved(baseline repository.Baseline) staging.ObservedSnapshot {
	observed := make(staging.ObservedSnapshot, len(baseline.Files))
	for name, file := range baseline.Files {
		observed[name] = staging.ObservedFile{Content: append([]byte(nil), file.Content...), Mode: file.Mode, Present: file.Present, Symlink: file.Symlink}
	}
	return observed
}

// BuildOwnedPlan bridges the in-memory baseline captured at session start to
// the staging ownership boundary at the current boundary. Only explicit
// adapter-requested paths are read; the durable store is never consulted for
// source bytes.
func (s Service) BuildOwnedPlan(req Request, baseline repository.Baseline) (staging.Plan, error) {
	if req.Root == "" {
		return staging.Plan{}, errors.New("owned plan root is required")
	}
	if len(req.Paths) == 0 {
		return staging.Plan{}, errors.New("owned plan paths are required")
	}
	return staging.BuildCapturedPlanFromBaseline(req.Root, baseline, req.Paths)
}
