# AutoGit implementation tracker

Source of truth: [`docs/implementation-plan.md`](docs/implementation-plan.md).
This is a working checklist, not phase-exit evidence. A checked item means a
bounded implementation slice has local tests; it does not waive the plan's
security, provider, native-OS, or release gates.

## Implemented slices

- [x] Phase 1 foundations: Go CLI skeleton, SQLite state and lifecycle
  receipts, canonical ingress validation/deduplication, redacted audit/status
  data, and argument-safe Git/provider/process ports.
- [x] Consent and repository primitives: canonical repository/worktree
  discovery, policy merge/validation, local-only defaults, and read-only
  status/plan/config/doctor/log operations.
- [x] Candidate and safety primitives: isolated Git candidate preparation,
  immutable commit intents, secret/conflict/path scanning, bounded trusted
  verifier execution, Conventional Commit validation, and local AutoGit refs.
- [x] Local verified-commit composition: `internal/workflow` requires tracking
  consent, scans a captured snapshot, verifies the exact candidate/base/policy/
  guard evidence, and preserves the user branch and shared index.
- [x] Ownership snapshot handoff: `internal/staging` blocks edited/deleted
  baseline paths as ambiguous, excludes unchanged baseline paths, and returns
  a deep-copied `gittransaction` snapshot while retaining explicitly observed
  file modes.
- [x] Workflow snapshot isolation: `internal/workflow` copies candidate bytes
  at entry so an injected scanner/verifier or caller cannot alter the
  scan/verification/commit input after work begins.
- [x] Explicit filesystem capture: `internal/staging` captures a named regular
  file beneath a canonical root into copied bytes and mode, rejects symlinks
  at every path component, and can build an owned plan from that capture.
- [x] Ownership-plan workflow handoff: `workflow.RunPlan` replaces any raw
  caller snapshot with `staging.Plan.CandidateSnapshot`, preventing candidate
  bytes from diverging from ownership evidence.
- [x] Ownership digest binding: plan identity covers candidate bytes, mode, and
  deletion state; `RunPlan` rejects empty plans and binds the immutable plan
  digest into guard evidence.
- [x] Provider, adapter, install, coordinator, and public-preflight building
  blocks with deterministic fake/contract tests.

## Completed execution batch (2026-09-01)

These eight bounded slices advance the next implementation order without
claiming a phase exit:

- [x] Repository observation port for real `HEAD`, index, and porcelain status.
- [x] Deterministic status parsing with rename/copy source and destination.
- [x] Fail-closed repository-relative path validation, including control and
  escaping paths.
- [x] Baseline file observations with mode, presence, regular-file, and
  symlink-component handling.
- [x] Direct baseline-to-staging ownership handoff for explicit requested
  paths.
- [x] Race-aware and size-bounded current-file capture with replacement tests.
- [x] Durable session baseline and remote-job schema migrations with typed
  retrieval (current schema v6).
- [x] Idempotent baseline persistence plus redacted canonical event payloads.

## Next implementation order

- [ ] Freeze Phase 0 terminology, requirement IDs, schema/lifecycle/threat
  invariants, test-traceability matrix, and compatibility window.
- [x] Capture durable session/task baselines from real repository observations
  (HEAD, index, status, modes, and owned paths), then feed them into staging;
  lifecycle-driven completion remains a separate open slice.
- [ ] Extend real filesystem snapshot capture to detect race substitutions and
  preserve rename/delete, ignore, linked-worktree, Unicode/control-path, and
  concurrent-writer rules; explicit regular-file content/mode capture and
  component-symlink rejection are covered.
- [x] Load frozen trusted verifier configuration from policy/configuration and
  wire it into `verify` plus the explicit local workflow boundary.
- [x] Wire explicit `sync --complete` to derive an owned candidate, invoke the
  verified local workflow, and record resulting lifecycle facts.
- [x] Wire `retry` and provider intent/reconciliation while retaining one exact
  local commit SHA across transient publication failures.
- [x] Complete the tested CLI provider/publication boundary, including exact
  destination and public-consent/preflight summaries; live canary remains open.
- [x] Complete supported-client capability discovery and the bounded adapter
  installation surface without granting adapters Git mutation authority.

## Completed execution batch (2026-09-02)

These eight additional bounded slices were implemented with focused red-green
tests:

- [x] Added a session baseline service that captures through the repository
  observation port and persists only bounded durable evidence.
- [x] Added strict trusted-verifier JSON configuration loading into the frozen
  verifier registry.
- [x] Wired verifier configuration digests and counts into `config explain`.
- [x] Added an explicit maximum file-size boundary to repository baselines.
- [x] Rejected symlinked and non-regular trusted verifier configuration files.
- [x] Rejected duplicate JSON keys in trusted verifier configuration.
- [x] Enforced restrictive Unix permissions for trusted verifier configuration.
- [x] Rejected overflowing or excessive verifier timeout values before duration
  conversion.

## Required validation and release gates

- [ ] Restore or replace the missing 177 prototype cases and reach the >=609
  deterministic release-suite target.
- [ ] Add fault-injection coverage for every durable intent boundary and the
  required crash/concurrency schedules.
- [ ] Observe native hosted macOS and Windows coverage; cross-build checks are
  not native execution evidence.
- [ ] Run the opt-in disposable GitHub canary with exact owner/name/visibility/
  ref/SHA postconditions and allowlisted cleanup.
- [ ] Complete private-alpha and public-beta gates; do not claim a phase exit
  before all plan deliverables and review evidence are present.

## Completed execution batch (continuation)

These eight bounded slices were implemented with loop-level failing and
passing verification:

- [x] Added guarded `Coordinator.RetryPush` for durable `RETRY_WAIT` jobs.
- [x] Validated provider remote/ref identities before ref inspection.
- [x] Exposed the session baseline service through the application boundary.
- [x] Allowed explicit clean paths in repository baseline capture.
- [x] Added the production bounded read-only repository Git runner.
- [x] Added a production-default session service constructor.
- [x] Carried explicit owned paths through default session baseline capture.
- [x] Accepted both Git exit-code forms for an unborn `HEAD`.

## Completed execution batch (next operations loop)

These eight bounded slices were implemented with focused red-green tests:

- [x] Added explicit `retry` CLI job, repository, and remote binding parsing.
- [x] Rejected terminal retry jobs before provider executable discovery.
- [x] Validated durable push-job identity and lifecycle state before storage.
- [x] Bridged session baselines into explicit current owned staging plans.
- [x] Added workflow verification configuration loading at the trusted boundary.
- [x] Added bounded `sync` CLI validation for explicit session ownership paths.
- [x] Wired `sync` to capture and persist a redacted repository baseline.
- [x] Added verify-only owned-plan evidence without commit intent or ref effects.
- [x] Session start/complete coordinator keeps source observations in-memory,
  derives owned plans at the current boundary, and delegates only through the
  verified workflow.
- [x] Session completion rejects stale `HEAD` or shared-index evidence before
  invoking workflow while allowing path-scoped status changes.
- [x] `session.started` ingress captures a trusted repository baseline before
  accepting the receipt; baseline failures leave no lifecycle receipt.
- [x] Explicitly absent baseline paths are treated as clean ownership state,
  so later creation can be attributed without weakening pre-existing-path
  ambiguity checks.
- [x] CLI hook baseline wiring uses the state port and tolerates omitted
  optional worktree identity without leaking baseline content.
- [x] Application exposes session completion only through the verified
  workflow port.
- [x] Explicit baseline paths consult Git ignore policy through a read-only
  capability and reject ignored candidates.
- [x] Repository baseline file capture checks identity before and after reads,
  including deterministic replacement-race coverage.
- [x] Added a durable state-backed lease adapter and applied writer leases to
  provider confirmation/push effects.
- [x] Wired the CLI retry coordinator to the durable writer lease and surfaced
  commit-lease release failures after preserving the durable commit result.
- [x] Applied canonical repository/worktree writer leases to local workflow
  commit/ref effects while keeping read-only verification outside the lease.
- [x] Added clean-session restart reconstruction from immutable Git tree blobs;
  dirty durable baselines fail closed without persisting source bytes.
- [x] Wired explicit read-only CLI `verify` for clean sessions, trusted
  verifiers, owned paths, and redacted evidence without commit/ref effects.
- [x] Wired explicit `sync --complete` for clean-session owned local commits
  with trusted verification and AutoGit-ref-only mutation.
- [x] Normalized ownership mode comparison to Git's executable-bit semantics,
  avoiding false conflicts from restrictive local filesystem permissions.

## Completed execution batch (2026-09-04)

- [x] Added explicit private `publish` CLI orchestration from a completed
  AutoGit commit intent.
- [x] Required explicit destination owner/name/remote/ref/visibility and
  rejected local-only or mismatched policy before provider discovery.
- [x] Persisted the remote destination digest in durable push intents and
  preserved it through retries.
- [x] Added fake-executable CLI coverage for exact remote binding, SHA/ref
  publication, postcondition confirmation, and durable success state.
- [x] Added public publish preflight reporting with bounded lowercase JSON and
  provider resolution after the preflight boundary.
- [x] Added read-only `install --list` adapter capability discovery for all
  six registered clients.
- [x] Rejected final-component symlinks in the CLI trusted executable resolver.

## Remaining implementation order

- [x] Build candidate file/history/readiness evidence from the exact committed
  tree and allow public publication only after `publication.Evaluate` passes;
  trusted verifier execution and hosted identity/visibility confirmation remain
  after the local preflight boundary.
- [x] Emit lifecycle/domain facts from CLI `sync`/`publish` operations with
  deterministic replay identities.
- [ ] Reconstruct the complete session-driven completion path from hook events.
- [x] Add provider repository creation/local-remote transaction wiring with
  exact owner/name/visibility postconditions; never attach a collision. A
  user-facing repository-initialization CLI flow remains open.
- [ ] Add durable fault injection at every CLI/provider intent boundary and
  expand concurrency/restart schedules.
