# AutoGit new-chat execution handoff

Use this file as the continuity record when starting a new chat. The
authoritative task list is [todo.md](../todo.md); the detailed scope and
promotion rules are in [implementation-plan.md](implementation-plan.md).

## Current snapshot

- Snapshot date: 2026-09-09.
- Repository: `/home/uday-varmora/autogit`.
- Evidence snapshot commit: `0fb0ad5eb7e84b17d28c765f2bdcb8649dd59878`.
- The manifest collection ran against a clean tree at that snapshot. This
  continuity/evidence refresh may be committed as a newer documentation
  commit, so the manifest's commit identity is intentionally historical.
- Overall release posture: **NO-GO for private alpha**.
- Tracker: 96 total checklist rows, 65 checked, 31 open.
- User-assigned implementation scope: 10 work packages—P4-01 through P4-08,
  P5-01, and P5-02.
- Those packages occupy 11 checklist rows because P5-01 has two independent
  substeps. All 11 remain unchecked pending acceptance evidence.
- Therefore, the assigned batch is locally implemented but formally closed at
  0/10 packages; the full project has 31 open rows.

## Why the tracker is still open

The plan does not treat code existence or one local test as completion. A
checkbox may be checked only when its implementation-plan acceptance evidence
is linked and includes the required exact commit/artifact identity, native
platform execution, provider evidence where applicable, and named review.

The local Phase 4/5 implementation slice exists, including test tiers, script
failure checks, artifact/scenario evaluators, fuzz budgets, chaos cases,
compatibility expiry automation, machine-readable evidence, governance files,
and dependency/security workflows. That is implementation progress, not release
acceptance.

## Evidence warning

- The current exact-snapshot full hosted matrix is run `34334723962`, with all
  20 jobs passing against `0fb0ad5eb7e84b17d28c765f2bdcb8649dd59878`, including
  fuzz, soak, native artifact, scenario, and performance jobs. Its security
  and dependency-policy jobs also passed. The push-triggered core run
  `34333523392` and security run `34333523253` passed against the same SHA.
- [phase-4-quality.json](release-evidence/phase-4-quality.json) is a fresh
  generated collection for evidence snapshot commit `0fb0ad5`; it records
  10/10 local suites and six artifact hashes with a clean tree at collection
  time.
- The last canary dispatch, `34324968472`, targeted the earlier `1ec8c39`
  snapshot and failed before mutation because the dedicated
  `AUTOGIT_CANARY_TOKEN` secret was empty; the allowlisted canary repository
  was confirmed absent. It is not current-HEAD canary evidence.

## Immediate continuation order

1. Re-read `todo.md`, this handoff, and the relevant Phase 4/5 sections of
   `implementation-plan.md`; inspect the current evidence generator and its
   output contract before running it.
2. Preserve the fresh exact-snapshot local manifest and hosted records above; if
   source or toolchain changes, regenerate with the configured 45-second fuzz
   budget and verify its commit, dirty-tree state, suite results, artifact
   identities, and control statuses before replacement.
3. Supply the dedicated canary credential and rerun the private canary; retain
   its exact owner/name/visibility/ref/SHA and cleanup evidence.
4. Reconcile the evidence against each P4-01..P4-08, P5-01, and P5-02
   acceptance condition. Check only rows whose evidence is genuinely complete.
5. Handle the remaining external gates separately: exact release tag,
   provider/App permission review, signed checksums/SBOM/provenance,
   reproducibility, named security/release review, native install/rollback
   tests, and alpha backup/restore/reconciliation/rollback drills.
6. Report any gate that cannot be completed with the exact missing authority,
   credential, platform, reviewer, or external event. Do not loop indefinitely
   on already-green local tests.

## Important recent changes

- `210c4c3`: lowered the default/manual fuzz duration from 60s to 45s while
  retaining the 100,000-execution minimum and leaving CI deadline headroom.
- `5635fe0`: aligned the evidence command with the 45s fuzz budget.
- `1427097`: corrected the fuzz evidence selector to the actual test function.

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
