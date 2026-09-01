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
  file beneath a canonical root into copied bytes and mode, rejects a final
  symlink, and can build an owned plan from that capture.
- [x] Ownership-plan workflow handoff: `workflow.RunPlan` replaces any raw
  caller snapshot with `staging.Plan.CandidateSnapshot`, preventing candidate
  bytes from diverging from ownership evidence.
- [x] Provider, adapter, install, coordinator, and public-preflight building
  blocks with deterministic fake/contract tests.

## Next implementation order

- [ ] Freeze Phase 0 terminology, requirement IDs, schema/lifecycle/threat
  invariants, test-traceability matrix, and compatibility window.
- [ ] Capture durable session/task baselines from real repository observations
  (HEAD, index, status, modes, and owned paths), then feed them into staging.
- [ ] Extend real filesystem snapshot capture to reject symlink components and
  race substitutions, and preserve rename/delete, ignore, linked-worktree,
  Unicode/control-path, and concurrent-writer rules; explicit regular-file
  content/mode capture and final-symlink rejection are covered.
- [ ] Load frozen trusted verifier configuration from policy/configuration and
  wire it into `verify` plus the session-driven local workflow.
- [ ] Wire `sync` to reconcile lifecycle state, derive an owned candidate,
  invoke the verified local workflow, and record resulting facts.
- [ ] Wire `retry` and provider intent/reconciliation while retaining one exact
  local commit SHA across transient publication failures.
- [ ] Complete CLI provider/publication flows, including exact destination and
  public-consent/preflight summaries.
- [ ] Complete supported-client discovery, adapter installation, and workflow
  orchestration without granting adapters Git mutation authority.

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
