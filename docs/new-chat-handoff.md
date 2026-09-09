# AutoGit new-chat execution handoff

Use this file as the continuity record when starting a new chat. The
authoritative task list is [todo.md](../todo.md); the detailed scope and
promotion rules are in [implementation-plan.md](implementation-plan.md).

## Current snapshot

- Snapshot date: 2026-09-09.
- Repository: `/home/uday-varmora/autogit`.
- HEAD and `origin/main`: `1427097b42b427054f0d508b6de9c90d7a1c0cce`.
- Working tree: clean at the last audit.
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

- The last successful full hosted matrix was run for commit `5635fe0` and had
  19/19 jobs pass.
- Current HEAD `1427097` contains the corrected fuzz-evidence selector, but no
  hosted run for this exact commit was present at the last audit.
- [phase-4-quality.json](release-evidence/phase-4-quality.json) is stale and
  still identifies commit `6d44b15`. Do not cite it as current exact-HEAD
  evidence and do not edit its commit field by hand.
- The previous long run was interrupted while refreshing the evidence bundle.
  A metadata typo was subsequently corrected in commit `1427097`.

## Immediate continuation order

1. Re-read `todo.md`, this handoff, and the relevant Phase 4/5 sections of
   `implementation-plan.md`; inspect the current evidence generator and its
   output contract before running it.
2. Regenerate the machine-readable and human-readable Phase 4 evidence for the
   exact current HEAD, using the corrected selector and the configured 45-second
   fuzz budget. Verify the manifest's commit, dirty-tree state, suite results,
   artifact identities, and control statuses before replacing stale checked-in
   evidence.
3. Run or dispatch hosted CI for exact HEAD `1427097`; retain the run URL/ID
   and verify all required jobs, not only the aggregate conclusion.
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
