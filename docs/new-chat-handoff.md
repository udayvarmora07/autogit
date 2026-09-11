# AutoGit new-chat execution handoff

Use this file as the continuity record when starting a new chat. The
authoritative task list is [todo.md](../todo.md); the detailed scope and
promotion rules are in [implementation-plan.md](implementation-plan.md).

## Current snapshot

- Snapshot date: 2026-09-10.
- Repository: `/home/uday-varmora/autogit`.
- Evidence snapshot commit: `6fc197b281065a03dccb8bad68984222bc8c786d`.
- The manifest collection ran against a clean tree at that snapshot. This
  continuity/evidence refresh may be committed as a newer documentation
  commit, so the manifest's commit identity is intentionally historical.
- Overall release posture: **NO-GO for private alpha**.
- Tracker: 96 total checklist rows, 74 checked, 22 open.
- Earlier user-assigned implementation scope: 10 work packages—P4-01 through
  P4-08, P5-01, and P5-02. The local continuation has also implemented the
  P5-03 through P5-05 release workflow slice and consumer artifact verifier,
  a protected GitHub Release publication slice for P5-06, and the P5-07
  rollback/disclosure drill, plus hosted macOS Bash portability and
  deterministic fuzz-floor fixes.
- Those earlier packages occupy 11 checklist rows because P5-01 has two
  independent substeps. P4-01, P4-02, P4-04, P4-05, P4-06, P4-07, P5-01, and P5-02 are
  accepted for the bounded private-alpha scope under the documented owner-
  review exception; P4-03 and P4-08 stay open pending their distinct release
  gates. The full project still has 22 open rows.

## Why the tracker is still open

The plan does not treat code existence or one local test as completion. A
checkbox may be checked only when its implementation-plan acceptance evidence
is linked and includes the required exact commit/artifact identity, native
platform execution, provider evidence where applicable, and named review.

The local Phase 4/5 implementation slice exists, including test tiers, script
failure checks, artifact/scenario evaluators, fuzz budgets, chaos cases,
compatibility expiry automation, machine-readable evidence, governance files,
and dependency/security workflows. The tag-gated release workflow now adds
exact identity checks, SPDX/binary-vulnerability evidence, keyless
attestations, and an Ubuntu/macOS byte-for-byte reproducibility comparison.
The consumer verifier binds checksums to the exact workflow, tag, and source
commit. That is implementation progress, not release acceptance.

The package-by-package reconciliation is recorded in the [Phase 4/5
acceptance matrix](release-evidence/phase-4-5-acceptance-matrix.md). It keeps
P4-01 through P4-08 tied to the exact quality evidence while retaining the
distinct native-artifact, named-review, and exact-tag requirements for the
remaining P4 rows.

## Evidence warning

- The current exact-snapshot full hosted matrix is run `34481766917`, with all
 20 jobs passing against
  `6fc197b281065a03dccb8bad68984222bc8c786d`, including
  fuzz, soak, native artifact, scenario, and performance jobs. Its security
  and dependency-policy jobs also passed. The push-triggered core run
  `34481728077` and security run `34481728025` passed against the same SHA.
- [phase-4-quality.json](release-evidence/phase-4-quality.json) is a fresh
  generated collection for evidence snapshot commit `6fc197b`; it records
  10/10 local suites and six artifact hashes with a clean tree at collection
  time.
- The current dedicated-token private canary dispatch, `34489797141`, passed
  against `36c045f` for owner `udayvarmora07`; the allowlisted canary
  repository was confirmed absent after cleanup. The earlier failed dispatch
  `34324968472` remains historical only.
- Hosted compatibility-window review `34483444477` passed at current branch
  commit `6fc197b`; no expiry issue was generated.

## Immediate continuation order

1. Re-read `todo.md`, this handoff, and the relevant Phase 4/5 sections of
   `implementation-plan.md`; inspect the current evidence generator and its
   output contract before running it.
2. Preserve the fresh exact-snapshot local manifest and hosted records above; if
   source or toolchain changes, regenerate with the configured 40-second fuzz
   budget and verify its commit, dirty-tree state, suite results, artifact
   identities, and control statuses before replacement.
3. Retain the completed dedicated-token private-canary evidence, including its
   exact owner/name/visibility/ref/SHA and cleanup result.
4. Reconcile the evidence against the remaining P4-03..P4-08 acceptance
   conditions. Check only rows whose evidence is genuinely complete.
5. Handle the remaining external gates separately: exact release tag and
   release-environment approval, hosted attestation and independent-runner
   verification, provider/App permission review, named security/release
   review, native install/rollback tests, and alpha
   backup/restore/reconciliation/rollback drills.
6. Report any gate that cannot be completed with the exact missing authority,
   credential, platform, reviewer, or external event. Do not loop indefinitely
   on already-green local tests.

## Important recent changes

- `c09c059`: kept the release verifier compatible with stock macOS Bash 3.2.
- `4817750`: made fuzz execution floors deterministic with an explicit input
  count and retained timeout budget.

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
