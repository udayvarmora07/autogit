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
	"autogit/internal/verification"
)

// Request contains only core-owned immutable candidate inputs. In particular,
// callers supply captured bytes rather than paths for the transaction to read
// from a mutable worktree.
type Request struct {
	ID            string
	RepositoryDir string
	Snapshot      []gittransaction.SnapshotEntry
	Message       string
	Policy        policy.Policy
	Verifiers     *verification.VerifierRegistry
}

// Result contains the evidence that authorized the local commit. It contains
// no source content, prompt material, command output, or secret findings.
type Result struct {
	Commit       gittransaction.Commit
	Verification verification.VerificationResult
	Scan         security.ScanResult
	PolicyDigest string
	GuardDigest  string
}

// Service dependencies are deliberately narrow so callers cannot bypass a
// scanner, frozen verifier set, or durable Git intent protocol.
type Service struct {
	Git            gittransaction.Runner
	Intents        gittransaction.IntentPort
	Scanner        security.CandidateScanner
	VerifierRunner verification.Runner
}

// Run creates a local AutoGit ref only after consent, a security scan, and
// trusted verification all bind to the exact immutable candidate. It never
// publishes a remote or moves the user's current branch.
func (s Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := policy.Validate(req.Policy); err != nil {
		return Result{}, fmt.Errorf("invalid effective policy: %w", err)
	}
	if !req.Policy.TrackingEnabled() {
		return Result{}, errors.New("tracking consent is required for a local commit")
	}
	if s.Git == nil || s.Intents == nil {
		return Result{}, errors.New("local commit dependencies are required")
	}
	info, err := repository.Discover(req.RepositoryDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve repository: %w", err)
	}

	scan := s.Scanner
	if scan == nil {
		scan = security.Scanner{}
	}
	snapshot := securitySnapshot(req.Snapshot)
	scanResult := scan.Scan(ctx, snapshot)
	guardDigest, err := digest(scanResult)
	if err != nil {
		return Result{}, fmt.Errorf("encode guard evidence: %w", err)
	}
	if !scanResult.Safe() {
		return Result{Scan: scanResult, GuardDigest: guardDigest}, errors.New("security scan blocked candidate")
	}
	if req.Verifiers == nil || s.VerifierRunner == nil {
		return Result{Scan: scanResult, GuardDigest: guardDigest}, errors.New("trusted verifier configuration is required")
	}
	policyDigest, err := digest(req.Policy)
	if err != nil {
		return Result{}, fmt.Errorf("encode policy evidence: %w", err)
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
		return Result{Scan: scanResult, PolicyDigest: policyDigest, GuardDigest: guardDigest}, err
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
	result := Result{Verification: verificationResult, Scan: scanResult, PolicyDigest: policyDigest, GuardDigest: guardDigest}
	if err != nil {
		return result, fmt.Errorf("verify candidate: %w", err)
	}
	if !verificationResult.ValidFor(verificationRequest, verificationPolicy, req.Verifiers) {
		return result, errors.New("verification did not pass for candidate")
	}
	committed, err := tx.CommitVerified(ctx, prepared, verificationResult, verificationPolicy, req.Verifiers)
	if err != nil {
		return result, err
	}
	result.Commit = committed
	return result, nil
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
