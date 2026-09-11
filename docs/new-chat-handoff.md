# AutoGit new-chat execution handoff

Use this file as the continuity record when starting a new chat. The
authoritative task list is [todo.md](../todo.md); the detailed scope and
promotion rules are in [implementation-plan.md](implementation-plan.md).

## Current snapshot

- Snapshot date: 2026-09-11.
- Repository: `/home/uday-varmora/autogit`.
- Evidence snapshot commit: `06ba87508ff53abc8b0001885e8341b6c07ad27a`.
- The manifest collection ran against a clean tree at that snapshot. This
  continuity/evidence refresh may be committed as a newer documentation
  commit, so the manifest's commit identity is intentionally the clean source
  snapshot rather than a manually edited current-HEAD value.
- Overall release posture: **NO-GO for private alpha**.
- Tracker: 96 total checklist rows, 76 checked, 20 open.
- Earlier user-assigned implementation scope: 10 work packages—P4-01 through
  P4-08, P5-01, and P5-02. The local continuation has also implemented the
  P5-03 through P5-05 release workflow slice and consumer artifact verifier,
  a protected GitHub Release publication slice for P5-06, and the P5-07
  rollback/disclosure drill, plus deterministic Homebrew/Scoop metadata
  generation and hosted macOS Bash portability and deterministic fuzz-floor
  fixes.
- Those earlier packages occupy 11 checklist rows because P5-01 has two
  independent substeps. P4-01, P4-02, P4-04, P4-05, P4-06, P4-07, P5-01, and P5-02 are
  accepted for the bounded private-alpha scope under the documented owner-
  review exception; P4-03 and P4-08 stay open pending their distinct release
  gates. PR-011 and the process-bounded PR-012 baseline are now checked;
  Linux now has an ABI-gated in-process Landlock ruleset behind the existing
  bubblewrap namespace path, while Windows AppContainer and native macOS
  acceptance remain separate open P1-04 work. The full project still has 20
  open rows.

## Why the tracker is still open

The plan does not treat code existence or one local test as completion. A
checkbox may be checked only when its implementation-plan acceptance evidence
is linked and includes the required exact commit/artifact identity, native
platform execution, provider evidence where applicable, and named review.

The local Phase 4/5 implementation slice exists, including test tiers, script
failure checks, artifact/scenario evaluators, fuzz budgets, chaos cases,
compatibility expiry automation, machine-readable evidence, governance files,
and dependency/security workflows. The tag-gated release workflow now adds
exact identity checks, SPDX/CycloneDX SBOM and binary-vulnerability evidence, keyless
attestations, and an Ubuntu/macOS byte-for-byte reproducibility comparison.
It also derives Homebrew and Scoop metadata from the exact release checksum
manifest and compares regenerated metadata on the protected publication runner.
The consumer verifier binds checksums to the exact workflow, tag, and source
commit. That is implementation progress, not release acceptance; package
repository publication and native clean-machine package tests remain open.

The package-by-package reconciliation is recorded in the [Phase 4/5
acceptance matrix](release-evidence/phase-4-5-acceptance-matrix.md). It keeps
P4-01 through P4-08 tied to the exact quality evidence while retaining the
distinct native-artifact, named-review, and exact-tag requirements for the
remaining P4 rows.

## Evidence warning

- The current push-triggered core run `34610742466` and security run
  `34610742472` both passed against exact source SHA
  `06ba87508ff53abc8b0001885e8341b6c07ad27a`. Core included native
  Linux/macOS/Windows checks, reproducible release binaries, and the Windows
  package-metadata regression. The preceding `e432bf5` core failure was caused
  by the Windows Git Bash hash-path incompatibility and is retained only as
  diagnostic history.
- Manual full matrix run `34627202246` passed all 20 jobs against current HEAD
  `a07fd84cb4b345e4dccdd3f950bd82dd662f5d87`, a documentation-only descendant
  of the evidence snapshot. It retained all six native artifact lifecycle
  drills plus native platform, fuzz, soak, reproducibility, security, and
  dependency-policy results.
- [phase-4-quality.json](release-evidence/phase-4-quality.json) is the fresh
  generated collection for evidence snapshot commit `06ba875`; it records
  10/10 local suites and six artifact hashes with `tag_verified: false` and a
  clean tree at collection time.

- The exact-source full matrix run `34604368677` passed all 20 jobs against
  `0d40d6af1ae38618edfa904f28fd7267e41e179e`, including fuzz, soak, native
  artifact, scenario, performance, security, and dependency-policy jobs. Its
  six native artifact jobs also retained the raw-binary lifecycle evidence.
  The source push run `34604333600` passed its native Linux/macOS/Windows
  checks against the same SHA.
- The current dedicated-token private canary dispatch, `34489797141`, passed
  against `36c045f` for owner `udayvarmora07`; the allowlisted canary
  repository was confirmed absent after cleanup. The earlier failed dispatch
  `34324968472` remains historical only.
- Hosted compatibility-window review `34590952233` is historical evidence for
  the preceding `97ea27f` snapshot; the current local compatibility suite
  passed in the regenerated manifest and no expiry issue was generated.

## Immediate continuation order

1. Re-read `todo.md`, this handoff, and the relevant Phase 4/5 sections of
   `implementation-plan.md`; inspect the current evidence generator and its
   output contract before running it.
2. Preserve the fresh exact-snapshot local manifest and hosted records above; if
   source or toolchain changes, regenerate with the configured 40-second fuzz
   budget and verify its commit, dirty-tree state, suite results, artifact
   identities, and control statuses before replacement. Do not edit the
   generated commit identity by hand.
3. Retain the completed dedicated-token private-canary evidence, including its
   exact owner/name/visibility/ref/SHA and cleanup result.
4. Reconcile the evidence against the remaining P4-03..P4-08 acceptance
   conditions. Check only rows whose evidence is genuinely complete.
5. Handle the remaining external gates separately: exact release tag and
   release-environment approval, hosted attestation and independent-runner
   verification, provider/App permission review, named security/release
   review, package-channel demand and publication, native install/rollback
   tests, and alpha
   backup/restore/reconciliation/rollback drills.
6. Report any gate that cannot be completed with the exact missing authority,
   credential, platform, reviewer, or external event. Do not loop indefinitely
   on already-green local tests.

## Important recent changes

- `97ea27f`: retries transient provider read-after-write missing-ref
  postconditions after an exact push, eliminating the hosted publish race.
- `60abf2d`: uses native Windows file replacement semantics for upgrade and
  rollback verification.
- `c09c059`: kept the release verifier compatible with stock macOS Bash 3.2.
- `4817750`: made fuzz execution floors deterministic with an explicit input
  count and retained timeout budget.
- Current PR-011/PR-012 slice: directory-durable backup/restore publication,
  stale maintenance-artifact recovery, subprocess crash tests for backup and
  migration, durable restore assertions, and a native process-supervisor
  capability probe. The latest local P1-04 slice adds direct Linux Landlock
  enforcement and adversarial coverage; Windows AppContainer and native macOS
  acceptance remain intentionally open.

## Continuation rules

- Preserve existing user changes and do not reset or rewrite history.
- Read a file before editing it and use `apply_patch` for edits.
- Do not mark a release gate complete merely because local code exists.
- Do not claim current-HEAD hosted validation until a current-HEAD run is
  verified.
- Do not claim the stale evidence manifest proves current status.
- Avoid repeating expensive fuzz/soak/native loops unless the input, code,
  commit, or evidence output has changed.
- Keep the release posture NO-GO until the plan's promotion rule is satisfied.

## Definition of success for the next chat

The next agent should either close the applicable P4/P5 rows with fresh,
traceable evidence, or leave the tracker honest and produce a precise blocker
list. It must finish with a fresh count of open rows, assigned-package status,
exact commit/CI/evidence identity, and the next smallest actionable step.
