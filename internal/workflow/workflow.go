// Package workflow composes the local, verified commit path from the narrow
// ownership, guard, verification, and Git transaction boundaries.
package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"autogit/internal/gittransaction"
	"autogit/internal/policy"
	"autogit/internal/repository"
	"autogit/internal/security"
	"autogit/internal/staging"
	"autogit/internal/verification"
)

// Request contains only core-owned immutable candidate inputs. In particular,
// callers supply captured bytes rather than paths for the transaction to read
// from a mutable worktree.
type Request struct {
	ID              string
	RepositoryDir   string
	Snapshot        []gittransaction.SnapshotEntry
	Message         string
	Policy          policy.Policy
	Verifiers       *verification.VerifierRegistry
	ownershipDigest string
}

// Result contains the evidence that authorized the local commit. It contains
// no source content, prompt material, command output, or secret findings.
type Result struct {
	Commit          gittransaction.Commit
	Verification    verification.VerificationResult
	Scan            security.ScanResult
	PolicyDigest    string
	GuardDigest     string
	OwnershipDigest string
}

// Service dependencies are deliberately narrow so callers cannot bypass a
// scanner, frozen verifier set, or durable Git intent protocol.
type Service struct {
	Git            gittransaction.Runner
	Intents        gittransaction.IntentPort
	Scanner        security.CandidateScanner
	VerifierRunner verification.Runner
}

// RunWithVerifierConfig loads and freezes trusted verifier configuration at
// the workflow boundary before any candidate Git intent is prepared.
func (s Service) RunWithVerifierConfig(ctx context.Context, req Request, configPath string) (Result, error) {
	if configPath == "" {
		return Result{}, errors.New("trusted verifier configuration path is required")
	}
	registry, err := verification.LoadRegistryFile(configPath, 0)
	if err != nil {
		return Result{}, fmt.Errorf("load verifier configuration: %w", err)
	}
	req.Verifiers = registry
	return s.Run(ctx, req)
}

// RunPlan accepts a candidate only through staging's ownership boundary. Any
// raw snapshot in req is intentionally replaced, preventing a caller from
// pairing plan evidence with different bytes.
func (s Service) RunPlan(ctx context.Context, req Request, plan staging.Plan) (Result, error) {
	req.Snapshot = plan.CandidateSnapshot()
	if len(req.Snapshot) == 0 {
		return Result{}, errors.New("owned plan is empty")
	}
	req.ownershipDigest = plan.OwnershipDigest()
	if req.ownershipDigest == "" {
		return Result{}, errors.New("owned plan evidence is missing")
	}
	return s.Run(ctx, req)
}

// VerifyPlan runs guards and trusted verification for an owned plan without
// persisting a commit intent or moving an AutoGit ref.
func (s Service) VerifyPlan(ctx context.Context, req Request, plan staging.Plan) (Result, error) {
	req.Snapshot = plan.CandidateSnapshot()
	if len(req.Snapshot) == 0 {
		return Result{}, errors.New("owned plan is empty")
	}
	req.ownershipDigest = plan.OwnershipDigest()
	if req.ownershipDigest == "" {
		return Result{}, errors.New("owned plan evidence is missing")
	}
	result, _, _, _, err := s.prepareAndVerify(ctx, req)
	return result, err
}

// Run creates a local AutoGit ref only after consent, a security scan, and
// trusted verification all bind to the exact immutable candidate. It never
// publishes a remote or moves the user's current branch.
func (s Service) Run(ctx context.Context, req Request) (Result, error) {
	result, tx, prepared, verificationPolicy, err := s.prepareAndVerify(ctx, req)
	if err != nil {
		return result, err
	}
	committed, err := tx.CommitVerified(ctx, prepared, result.Verification, verificationPolicy, req.Verifiers)
	if err != nil {
		return result, err
	}
	result.Commit = committed
	return result, nil
}

func (s Service) prepareAndVerify(ctx context.Context, req Request) (Result, *gittransaction.Transaction, *gittransaction.Prepared, verification.VerificationPolicy, error) {
	// Detach from caller-owned slices before any injected scanner or verifier
	// can run. The same captured bytes must be scanned, verified, and prepared.
	req.Snapshot = cloneSnapshot(req.Snapshot)
	if err := policy.Validate(req.Policy); err != nil {
		return Result{}, nil, nil, verification.VerificationPolicy{}, fmt.Errorf("invalid effective policy: %w", err)
	}
	if !req.Policy.TrackingEnabled() {
		return Result{}, nil, nil, verification.VerificationPolicy{}, errors.New("tracking consent is required for a local commit")
	}
	if s.Git == nil || s.Intents == nil {
		return Result{}, nil, nil, verification.VerificationPolicy{}, errors.New("local commit dependencies are required")
	}
	info, err := repository.Discover(req.RepositoryDir)
	if err != nil {
		return Result{}, nil, nil, verification.VerificationPolicy{}, fmt.Errorf("resolve repository: %w", err)
	}

	scan := s.Scanner
	if scan == nil {
		scan = security.Scanner{}
	}
	snapshot := securitySnapshot(req.Snapshot)
	scanResult := scan.Scan(ctx, snapshot)
	guardDigest, err := digest(guardEvidence{Scan: scanResult, OwnershipDigest: req.ownershipDigest})
	if err != nil {
		return Result{}, nil, nil, verification.VerificationPolicy{}, fmt.Errorf("encode guard evidence: %w", err)
	}
	guardResult := Result{Scan: scanResult, GuardDigest: guardDigest, OwnershipDigest: req.ownershipDigest}
	if !scanResult.Safe() {
		return guardResult, nil, nil, verification.VerificationPolicy{}, errors.New("security scan blocked candidate")
	}
	if req.Verifiers == nil || s.VerifierRunner == nil {
		return guardResult, nil, nil, verification.VerificationPolicy{}, errors.New("trusted verifier configuration is required")
	}
	policyDigest, err := digest(req.Policy)
	if err != nil {
		return Result{}, nil, nil, verification.VerificationPolicy{}, fmt.Errorf("encode policy evidence: %w", err)
	}

	tx := gittransaction.New(s.Git, s.Intents)
	prepared, err := tx.Prepare(ctx, gittransaction.Request{
		ID:             req.ID,
		RepoDir:        info.Root,
		Snapshot:       append([]gittransaction.SnapshotEntry(nil), req.Snapshot...),
		Message:        req.Message,
		PolicyDigest:   policyDigest,
		VerifierDigest: req.Verifiers.VerifierSetDigest,
		GuardDigest:    guardDigest,
	})
	if err != nil {
		return Result{Scan: scanResult, PolicyDigest: policyDigest, GuardDigest: guardDigest, OwnershipDigest: req.ownershipDigest}, tx, nil, verification.VerificationPolicy{}, err
	}

	verificationPolicy := verification.VerificationPolicy{
		Visibility: req.Policy.Visibility,
		LocalOnly:  req.Policy.LocalOnly,
	}
	verificationRequest := verification.TrustedRequest{
		CandidateDigest: prepared.CandidateDigest(),
		BaseDigest:      sha256Digest(prepared.BaseSHA()),
		PolicyDigest:    policyDigest,
		GuardDigest:     guardDigest,
		Dir:             info.Root,
	}
	verificationResult, err := req.Verifiers.Verify(ctx, verificationPolicy, verificationRequest, s.VerifierRunner)
	result := Result{Verification: verificationResult, Scan: scanResult, PolicyDigest: policyDigest, GuardDigest: guardDigest, OwnershipDigest: req.ownershipDigest}
	if err != nil {
		return result, tx, prepared, verificationPolicy, fmt.Errorf("verify candidate: %w", err)
	}
	if !verificationResult.ValidFor(verificationRequest, verificationPolicy, req.Verifiers) {
		return result, tx, prepared, verificationPolicy, errors.New("verification did not pass for candidate")
	}
	return result, tx, prepared, verificationPolicy, nil
}

type guardEvidence struct {
	Scan            security.ScanResult `json:"scan"`
	OwnershipDigest string              `json:"ownership_digest,omitempty"`
}

func cloneSnapshot(entries []gittransaction.SnapshotEntry) []gittransaction.SnapshotEntry {
	copyEntries := make([]gittransaction.SnapshotEntry, len(entries))
	for i, entry := range entries {
		copyEntries[i] = entry
		copyEntries[i].Content = append([]byte(nil), entry.Content...)
	}
	return copyEntries
}

func securitySnapshot(entries []gittransaction.SnapshotEntry) security.CandidateSnapshot {
	files := make([]security.CandidateFile, 0, len(entries))
	for _, entry := range entries {
		if entry.Delete {
			continue
		}
		files = append(files, security.CandidateFile{
			Path:    entry.Path,
			Content: append([]byte(nil), entry.Content...),
			Mode:    uint32(entry.Mode),
			Symlink: entry.Mode&0o120000 == 0o120000,
		})
	}
	return security.CandidateSnapshot{Files: files}
}

func digest(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

func sha256Digest(value string) string {
	h := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(h[:])
}
