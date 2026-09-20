# AutoGit new-chat execution handoff

Use this file as the continuity record when starting a new chat. The
authoritative task list is [todo.md](../todo.md); the detailed scope and
promotion rules are in [implementation-plan.md](implementation-plan.md).

## Current snapshot

- Snapshot date: 2026-09-20.
- Repository: `/home/uday-varmora/autogit`.
- Evidence snapshot commit: `06ba87508ff53abc8b0001885e8341b6c07ad27a`; current
  source HEAD is `616b83b` (documentation/evidence descendants after the
  snapshot).
- The manifest collection ran against a clean tree at that snapshot. This
  continuity/evidence refresh may be committed as a newer documentation
  commit, so the manifest's commit identity is intentionally the clean source
  snapshot rather than a manually edited current-HEAD value.
- Overall release posture: **NO-GO for private alpha**.
- Tracker: 96 total checklist rows, 83 checked, 13 open.
- Earlier user-assigned implementation scope: 10 work packages—P4-01 through
  P4-08, P5-01, and P5-02. The local continuation has also implemented the
  P5-03 through P5-05 release workflow slice and consumer artifact verifier,
  a protected GitHub Release publication slice for P5-06, and the P5-07
  rollback/disclosure drill, plus deterministic Homebrew/Scoop metadata
  generation and hosted macOS Bash portability and deterministic fuzz-floor
  fixes.
- Those earlier packages occupy 11 checklist rows because P5-01 has two
  independent substeps. P4-01 through P4-08 and P5-01 through P5-05 now have
  exact-tag `v0.1.2` implementation/release evidence where applicable;
  package-channel lifecycle, phase-gate, cohort, and named-review rows
  remain open below. PR-011 and the process-bounded PR-012 baseline are now checked;
  Linux now has an ABI-gated in-process Landlock ruleset behind the existing
  bubblewrap namespace path, and Windows now has native AppContainer
  enforcement with parent-side token attestation. Native macOS acceptance
  and named platform-owner review remain separate P1-04 gates. The full
  project now has 13 open rows.

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
- Manual full matrix run `34627202246` passed all 20 jobs against then-current
  HEAD `a07fd84cb4b345e4dccdd3f950bd82dd662f5d87`, a documentation-only
  descendant of the evidence snapshot. It retained all six native artifact lifecycle
  drills plus native platform, fuzz, soak, reproducibility, security, and
  dependency-policy results.
- [phase-4-quality.json](release-evidence/phase-4-quality.json) is the fresh
  generated collection for evidence snapshot commit `06ba875`; it records
  10/10 local suites and six artifact hashes with `tag_verified: false` and a
  clean tree at collection time.

- Exact-tag release workflow `35506194835` passed all six jobs for `v0.1.2` at
  `abcd3fb`, including independent Ubuntu/macOS reproducibility, hosted
  attestations, consumer verification, and protected GitHub Release
  publication. The release is live at the `v0.1.2` tag with six binaries,
  checksums, SBOMs, vulnerability reports, evidence, and package metadata.
- Push core run `35505877385` passed native Linux/macOS/Windows, security,
  cross-build, reproducibility, dependency, and presubmit jobs. Manual full
  matrix `35508065704` passed all 20 jobs against exact source
  `3888b570aa072317fbe132c284ed4e844092daa0`, including six native artifact
  lifecycle drills, fuzz, soak, and performance. Current `HEAD` `0745e0b` is a
  documentation-only descendant with no implementation inputs changed, so the
  matrix remains implementation evidence but is not a new hosted run against
  the current documentation commit.
- Private canary evidence run `35508083681` passed for owner `udayvarmora07`.
  The private canary evidence is for source SHA
  `3888b570aa072317fbe132c284ed4e844092daa0`; it is not a run against
  documentation HEAD `0745e0bc77a63af3cf21fa59522a387f6b533465`. It created
  `autogit-v1-test-35508083681`, verified `main` at
  `8fd321c37412aa04f70df8667d4132286b40a150`, and its cleanup was confirmed by
  a post-run repository lookup.

- The exact-source full matrix run `34604368677` passed all 20 jobs against
  `0d40d6af1ae38618edfa904f28fd7267e41e179e`, including fuzz, soak, native
  artifact, scenario, performance, security, and dependency-policy jobs. Its
  six native artifact jobs also retained the raw-binary lifecycle evidence.
  The source push run `34604333600` passed its native Linux/macOS/Windows
  checks against the same SHA.
- The current dedicated-token private canary dispatch, `34682636514`, passed
  against exact SHA `1406352345886c116ba7750369fc3a14df960b63` for owner
  `udayvarmora07`; the allowlisted canary repository was confirmed absent after
  cleanup. The preceding failed dispatch `34678552733` exposed and led to the
  bounded REST confirmation retry now in `1406352`.
- The current exact-SHA full matrix `34682716776` passed all 20 jobs against
  `1406352345886c116ba7750369fc3a14df960b63`, including native artifact,
  soak, fuzz, reproducibility, security, dependency-policy, scenario, and
  performance checks. The later Windows AppContainer follow-up is recorded
  separately in the Phase 1 evidence bundle; named acceptance and exact-tag
  release gates remain open.
- Current-HEAD full matrix `35492327966` passed all 20 jobs against exact
  source `f8ad8d5f61d046551c281f759809d607d6a9fe79`, including native
  Linux/macOS/Windows checks, six native artifact lifecycle jobs, three soak
  jobs, fuzz, reproducibility, security, and dependency-policy checks.
- Current-HEAD private canary `35493024891` passed against the same source for
  `udayvarmora07`; it verified private repository
  `autogit-v1-test-35493024891`, `main` SHA
  `75efeca81576d17eaebec6cbd916946ab6ac2a15`, and cleanup confirmed absence.
- The clean Windows AppContainer follow-up passed native job `104271291760` in
  core run `34935126986` against exact source
  `c1a94d4902e13d454ef87eb9c5d394c604bea608`. It independently attests the
  child token from the parent and proves allowlisted filesystem access,
  denied unlisted reads/writes, and owner/group plus DACL ACE/protection
  restoration (allowing only Windows' documented `AI` auto-inheritance flag
  normalization). AppContainer stdio uses inherited regular temporary files
  with a path-based bounded-output monitor because Go Windows child startup
  can block while probing synchronous pipe handles. Native macOS isolation,
  named platform-owner acceptance, and exact-tag release gates remain open.
- The current-tree manual dispatch `34961479624` passed all 20 core jobs
  against `a51eb9718bac0e3e514e196fa4e1156a47865ade`. Its six native artifact jobs all passed built-artifact
  smoke and raw-binary lifecycle drills; the run also passed native platform,
  fuzz, soak, reproducibility, security, cross-build, and dependency-policy
  jobs. This is current implementation evidence, not exact stable-tag release
  acceptance.
- Hosted compatibility-window review `34590952233` is historical evidence for
  the preceding `97ea27f` snapshot; the current local compatibility suite
  passed in the regenerated manifest and no expiry issue was generated.

## Immediate continuation order

1. Retain successful exact-source matrix `35508065704` at `3888b57` as the
   current implementation evidence. Keep failed diagnostic run `35507277048`
   visible for the Ubuntu provider-postcondition and Windows AppContainer
   timeout investigation rather than treating it as a gate pass.
2. Update the Phase 1 and Phase 2 gate records with that matrix and the fresh
   private canary evidence; obtain or record the remaining named platform/provider
   review rather than inferring it from green automation.
3. Keep P5-06 open for native clean-machine package lifecycle tests; exact
   `v0.1.2` metadata is now published in the selected Homebrew and Scoop
   repositories, and hosted initial-install smoke run `35514143281` passed.
   The verified GitHub Release path is complete.
4. Complete native clean-machine Homebrew/Scoop testing only on the claimed
   platforms and retain redacted results; winget remains deferred and attached
   metadata must not be confused with lifecycle acceptance.
5. Obtain the named security/release tabletop review for P5-07, then run the
   bounded private cohort and backup/provider/adapter/release rollback drills
   required for alpha entry.
6. Report any gate that cannot be completed with the exact missing authority,
   credential, platform, reviewer, or external event. Do not loop indefinitely
   on already-green local tests.

## Important recent changes

- `97ea27f`: retries transient provider read-after-write missing-ref
  postconditions after an exact push, eliminating the hosted publish race.
- `1406352`: retries transient GitHub REST empty-repository ref responses
  during non-mutating post-push confirmation and records a regression test.
- `60abf2d`: uses native Windows file replacement semantics for upgrade and
  rollback verification.
- `c09c059`: kept the release verifier compatible with stock macOS Bash 3.2.
- `4817750`: made fuzz execution floors deterministic with an explicit input
  count and retained timeout budget.
- Current PR-011/PR-012 slice: directory-durable backup/restore publication,
  stale maintenance-artifact recovery, subprocess crash tests for backup and
  migration, durable restore assertions, and a native process-supervisor
  capability probe. The latest P1-04 slice adds direct Linux Landlock
  enforcement and adversarial coverage plus native Windows AppContainer
  enforcement and parent-side token attestation; native macOS acceptance
  remains intentionally open.

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
