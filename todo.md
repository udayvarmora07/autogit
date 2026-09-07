# AutoGit execution tracker

Source of truth: [world-class implementation plan](docs/implementation-plan.md)
Audited baseline: f9b692f261c22a5ca101074c644a42a33a96f9e0
Last updated: 2026-09-07
Release posture: NO-GO for private alpha

This tracker contains remaining work, not the project's historical changelog.
Completed implementation history remains in Git. Check an item only when its
acceptance evidence is linked from the pull request or release evidence bundle.
Repeated IDs are independently checkable substeps of the same plan work
package.

## Verified foundation

- [x] Consent-based, private/local-first policy and separate public consent.
- [x] Canonical bounded ingress, durable receipts, lifecycle projection, and
  idempotent replay.
- [x] Source-free session baselines, owned candidate snapshots, ambiguity
  blocking, and shared index/branch preservation.
- [x] Exact-tree local commit transactions with durable intent and recovery.
- [x] Exact destination/SHA/ref provider transaction and retry primitives.
- [x] Candidate/history guard framework and frozen verifier evidence.
- [x] Native Linux/macOS/Windows CI, race coverage, deterministic recovery
  schedules, and performance gates.
- [x] Fresh 2026-09-06 test, vet, build, module verification, actionlint, shell
  syntax, and performance validation.

These checks describe existing evidence only. They do not satisfy the release
gates below.

## Phase 0 — release safety reset

All items are release blockers.

- [x] P0-01 Upgrade modernc.org/sqlite to a current release containing SQLite
  3.51.3 or later, pin compatible transitive dependencies, assert the embedded
  SQLite version, and pass migration/WAL/concurrency/crash tests natively.
  Evidence: [Phase 0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] P0-02 Consolidate events and job state behind one database opener,
  migration owner, pragma contract, and connection policy.
  Evidence: [Phase 0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] P0-03 Protect state root, DB/WAL/SHM, identity key, policy, verifier
  config, and temp files from symlinks, traversal, replacement, wrong
  ownership, and permissive modes; add adversarial native tests.
  Evidence: [Phase 0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] P0-04 Use the hardened bounded Git runner for init and linked-worktree
  discovery; prove hostile Git config cannot execute hooks/helpers/SSH/prompts.
  Evidence: [Phase 0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] P0-05 Add command/effect deadlines, cancellation budgets, bounded
  database waits, and full process-tree termination on every supported OS.
  Evidence: [Phase 0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] P0-06 Make duplicate session.started receipt handling happen before
  baseline capture, or atomically couple first acceptance and baseline state.
  Evidence: [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] P0-07 Make initialization consent transactional and policy persistence
  locked, no-follow, atomic, fsynced, revision-aware, and corruption-visible.
  Evidence: [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] P0-08 Fix all current staticcheck findings, including ignored/overwritten
  errors in Git transaction code and dead CLI/repository code.
  Evidence: [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] P0-08 Triage the three current gosec G703 path findings and all excluded
  rule classes; replace broad exclusions with narrow documented suppressions
  backed by tests.
  Evidence: [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] P0-09 Record owner/date acceptance for requirements, lifecycle, event
  contract, threat model, compatibility window, and release terminology.
  Evidence: [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] P0-10 Reject non-empty/stale release output and inject reproducible
  version, commit, build date, and compatibility identity into binaries.
  Evidence: [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [ ] Phase 0 gate: all analysis/build/test/performance checks pass on native
  Linux, macOS, and Windows with zero unresolved critical/high findings.

## Phase 1 — trusted local core

- [x] P1-01 Assert SQLite journal, synchronous, foreign-key, busy-timeout,
  checkpoint, and connection settings; detect unsupported/network filesystems
  and fail closed or use a documented safe journal mode. Evidence: [Phase 1
  P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] P1-02 Add supported online backup, restore, integrity check,
  foreign-key check, migration rollback, repair, and redacted export flows.
  Evidence: [Phase 1 P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] P1-03 Implement bounded audit/event retention, pruning, compaction, and
  privacy-budget tests without removing active recovery evidence.
  Evidence: [Phase 1 P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] P1-04 Define and implement verification isolation tiers: none,
  process-bounded, filesystem-isolated, filesystem-and-network-isolated, and
  remote-hermetic. Evidence: [Phase 1 P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] P1-04 Enforce and test descendant cleanup, CPU/memory/file/process
  limits, filesystem allowlists, and network denial for advertised tiers.
  Evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md).
- [ ] P1-04 Prototype and validate Linux Landlock/namespaces, Windows
  AppContainer/job controls, and an honest macOS fallback.
- [x] P1-05 Introduce a pinned offline secret-scanner interface over exact
  candidate blobs and reachable history; keep online validation separately
  consented. Evidence: [Phase 1 P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] P1-05 Include scanner version, coverage, truncation/limit state, redacted
  fingerprints, and provider push-protection guidance in preflight evidence.
  Evidence: [Phase 1 P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] P1-06 Differentially test tree creation for attributes, filters, LFS,
  sparse indexes/checkouts, submodules, worktrees, unusual modes, Unicode,
  newline, control, and option-like paths. Evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md).
- [x] P1-07 Add SHA-1 and SHA-256 repository fixtures and remove remaining
  object-ID-length assumptions. Evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md).
- [x] P1-08 Bind verifier/config validation to the executed object/identity
  where supported and report residual TOCTOU limitations elsewhere. Evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md).
- [x] P1-09 Remove or define the inconsistent public tracking policy value and
  property-test policy merge/validation. Evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md).
- [x] P1-10 Guarantee status, plan, config explain, and non-repair doctor are
  filesystem- and network-read-only. Evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md).
- [ ] Phase 1 gate: local workflow passes hostile repository, crash,
  cancellation, backup/restore, privacy, and ownership matrices on all claimed
  native platforms.

## Phase 2 — integrations and provider productization

- [x] P2-01 Build a versioned adapter capability registry from immutable,
  sanitized, client-versioned payload/config fixtures. Evidence: [Phase 2
  P2-01–P2-05 bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-02 Add install-time and doctor-time version/capability probes with
  explicit supported, degraded, and unsupported states. Evidence: [Phase 2
  P2-01–P2-05 bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-03 Refresh Codex hook contracts and installer from current official
  behavior and pinned fixtures. Evidence: [Phase 2 P2-01–P2-05
  bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-03 Refresh Claude Code hook contracts and installer from current
  official behavior and pinned fixtures. Evidence: [Phase 2 P2-01–P2-05
  bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-03 Refresh Gemini CLI hook contracts and installer from current
  official behavior and pinned fixtures. Evidence: [Phase 2 P2-01–P2-05
  bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-03 Implement Cursor's current official hook surface rather than
  reporting Cursor as hookless. Evidence: [Phase 2 P2-01–P2-05
  bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-04 Implement OpenCode and CommandCode only where stable native
  contracts exist; otherwise retain precise observation-only explanations.
  Evidence: [Phase 2 P2-01–P2-05 bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [x] P2-05 Make each client config edit schema-specific, ownership-aware,
  atomic, backed up, reversible, and safe under concurrent user edits.
  Evidence: [Phase 2 P2-01–P2-05 bundle](docs/release-evidence/phase-2-p2-01-p2-05.md).
- [ ] P2-06 Implement a typed GitHub REST transport with an explicit API
  version, bounded/redacted bodies, pagination, request IDs, retry-after/rate
  limits, conditional reads, and durable reconciliation.
- [ ] P2-07 Bind provider host/account/owner identity explicitly and test
  GH_TOKEN, GITHUB_TOKEN, multi-account, revoked/expired, wrong-owner, and
  wrong-host cases.
- [ ] P2-08 Add least-privileged GitHub App installation-token support while
  retaining gh as an optional local bootstrap/user-auth adapter.
- [ ] P2-09 Define and test a GitHub Enterprise Server support/version policy.
- [ ] P2-10 Optionally project redacted verification evidence as a head-SHA
  bound Check Run without making it a source of truth.
- [ ] P2-11 Add an optional, read-only-first MCP surface for status, plan,
  explain, and logs; require separate domain consent for verify/publish.
- [ ] Phase 2 gate: all advertised adapters have fixture and native install
  evidence, and the exact alpha provider path passes a disposable private
  GitHub canary with allowlisted cleanup.

## Phase 3 — product UX and operations

- [ ] P3-01 Refactor the 2,143-line CLI file into command parsers,
  application services, presenters, and composition root with golden behavior
  tests.
- [ ] P3-02 Add version, full per-command help, examples, shell completions,
  man pages, prerequisites, and a clean-machine quickstart.
- [ ] P3-03 Define stable stdout/stderr, human/JSON, exit-code, error-cause,
  redaction, and remediation contracts; remove duplicated error codes.
- [ ] P3-04 Expand doctor into an offline capability/health report covering
  binary, Git, state, isolation, adapters, provider mode, and safe next steps.
- [ ] P3-05 Add durable operation status, explain, resume, cancel, runlog, and
  bounded undo for AutoGit-owned effects only.
- [ ] P3-06 Add previewable config migrations, backups, rollback, and
  provenance for every owned config fragment.
- [ ] P3-07 Add local correlated logs/traces/metrics and opt-in OpenTelemetry
  export with strict field/cardinality/privacy allowlists.
- [ ] P3-08 Establish and trend p50/p95/p99 budgets for hook, path
  observation, startup, DB growth, verification, and provider operations.
- [ ] P3-09 Rewrite README/operator docs for installation, trust boundaries,
  limitations, workflows, recovery, upgrade, uninstall, and support.

## Phase 4 — verification, evals, and continuous quality

- [ ] P4-01 Split fast presubmit, race, integration, 1,000-schedule soak,
  fuzz, canary, and release suites; keep presubmit p95 below five minutes.
- [ ] P4-02 Add shellcheck and direct failure/safety tests for canary,
  performance, and release shell scripts.
- [ ] P4-03 Execute built-artifact smoke tests for every claimed OS/arch and
  supported system Git range on native hosts.
- [ ] P4-04 Expand fuzz/property/differential tests across JSON, path, ref,
  status, policy, migration, scanner, and provider boundaries.
- [ ] P4-05 Add disk-full, permission-loss, clock-change, lock-contention,
  signal/kill, network-stall, rate-limit, and partial-response chaos tests.
- [ ] P4-06 Build client/OS scenario evals that inspect final
  repository/index/ref/provider state rather than model narration.
- [ ] P4-07 Automate compatibility-window testing and expiry issues for Go,
  Git, SQLite, clients, GitHub API, MCP, and durable schemas.
- [ ] P4-08 Publish machine-readable requirement/threat/test evidence for the
  exact tagged commit and artifacts.

## Phase 5 — supply chain, governance, and distribution

- [ ] P5-01 Decide and add LICENSE.
- [ ] P5-01 Add SECURITY.md, CONTRIBUTING.md, CODE_OF_CONDUCT.md, CODEOWNERS,
  support policy, and CHANGELOG.md.
- [ ] P5-02 Add controlled dependency updates, license policy, dependency
  review, CodeQL, OpenSSF Scorecard, least workflow permissions, and pinned
  action update review.
- [ ] P5-03 Build a tag-gated clean-room release workflow that rejects dirty,
  reused, version-mismatched, or incompletely tested artifacts.
- [ ] P5-04 Produce SPDX/CycloneDX SBOMs, binary govulncheck evidence, signed
  checksums, and hosted SLSA provenance/attestations.
- [ ] P5-05 Compare release artifacts across independent runners/build paths
  and publish reproducibility evidence.
- [ ] P5-06 Publish verified GitHub Releases and add tested Homebrew plus
  winget/Scoop channels according to demonstrated platform demand.
- [ ] P5-07 Test native install, upgrade, downgrade, uninstall, checksum,
  signature, and rollback paths.
- [ ] P5-07 Define disclosure/patch SLAs, signing identity recovery, release
  rollback, and compromised-release drills.

## Phase 6 — release progression

- [ ] Private alpha entry: Phases 0 and 1 complete; the advertised Phase 2
  subset complete; signed private artifacts and support owner ready.
- [ ] Run and retain the disposable private GitHub canary evidence.
- [ ] Run a bounded private-repository cohort with opt-in telemetry and
  explicit issue/incident triage.
- [ ] Complete backup/restore, provider reconciliation, adapter rollback, and
  release rollback drills.
- [ ] Private alpha exit: two consecutive candidates meet alpha objectives
  with zero wrong-target, unconsented, unrelated-change, duplicate-effect, or
  unrecoverable-state incident.
- [ ] Public beta entry: Phases 2 through 5 complete for advertised scope.
- [ ] Run a separately confirmed disposable public canary and verify exact
  owner/name/visibility/ref/SHA plus allowlisted cleanup.
- [ ] Complete the beta observation window with signed artifacts, published
  evidence, support coverage, and no severity-1 safety incident.
- [ ] GA entry: independent security review, threat-model refresh, native
  restore/reproducibility evidence, support/deprecation policy, and signed
  evidence manifest for the exact release tag.

## Immediate PR queue

- [x] PR-001 SQLite engine upgrade and embedded-version/WAL tests. Evidence:
  [Phase 0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] PR-002 Single SQLite gateway and migration owner. Evidence: [Phase 0
  P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] PR-003 State/policy filesystem trust boundary. Evidence: [Phase 0
  P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] PR-004 Hardened Git init and worktree discovery. Evidence: [Phase 0
  P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] PR-005 Operation deadlines and descendant termination. Evidence: [Phase
  0 P0-01–P0-05 bundle](docs/release-evidence/phase-0-p0-01-p0-05.md).
- [x] PR-006 Session-start replay idempotency. Evidence: [Phase 0
  P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] PR-007 Transactional consent and durable policy file. Evidence: [Phase 0
  P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] PR-008 Staticcheck and gosec closure. Evidence: [Phase 0
  P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] PR-009 Release output hygiene and binary identity. Evidence: [Phase 0
  P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [x] PR-010 Contract acceptance plus new architecture decisions. Evidence:
  [Phase 0 P0-06–P0-10 bundle](docs/release-evidence/phase-0-p0-06-p0-10.md).
- [ ] PR-011 Backup/restore/integrity/retention.
- [ ] PR-012 Verification isolation capability baseline.

## Promotion rule

Do not convert an unchecked release gate to complete because code exists or a
single local test passes. Completion requires the implementation-plan
acceptance evidence, exact commit/artifact identity, required native platforms,
and a named reviewer. A blocked public operation may retain a safe local
AutoGit commit; no convenience path can waive consent, ownership, immutable
evidence, isolation policy, or exact destination postconditions.
