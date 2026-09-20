# AutoGit execution tracker

Source of truth: [world-class implementation plan](docs/implementation-plan.md)
Audited baseline: f9b692f261c22a5ca101074c644a42a33a96f9e0
Last updated: 2026-09-20
Release posture: NO-GO for private alpha
Private-alpha review policy: the documented single-maintainer exception allows
named release-owner approval; it does not waive exact-tag, canary, provenance,
reproducibility, or consumer-verification evidence and does not apply to beta
or GA.

## New-chat continuation handoff

For cross-chat continuity, read [docs/new-chat-handoff.md](docs/new-chat-handoff.md)
before starting work. Current audit: 96 total rows, 83 checked, 13 open. The
user-assigned implementation batch is verified, and exact-tag `v0.1.2` release
evidence now covers the GitHub Release path; package-channel, clean-machine,
phase-gate, cohort, and named-review requirements remain open where listed
below.
Implementation state for that batch is tracked separately in the
[Phase 4/5 implementation ledger](docs/release-evidence/phase-4-5-implementation-ledger.json):
10/10 packages are verified against the recorded implementation evidence;
2 release-acceptance rows remain pending review.
The checked-in Phase 4 machine manifest records a generated clean collection
for evidence snapshot commit
`06ba87508ff53abc8b0001885e8341b6c07ad27a`. Documentation and release-workflow
commits now advance the branch after that snapshot. The exact-tag `v0.1.2`
release is separately recorded in the Phase 5 evidence bundle. If source or
toolchain inputs change, regenerate the manifest with the evidence command;
never update its commit identity by hand.
The package-level reconciliation is recorded in the [Phase 4/5 acceptance
matrix](docs/release-evidence/phase-4-5-acceptance-matrix.md).
The recommended two-ledger closure model and depth-first loop are documented
in the [task closure protocol](docs/task-closure-protocol.md).

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
- [x] Phase 0 gate: all analysis/build/test/performance checks pass on native
  Linux, macOS, and Windows with zero unresolved critical/high findings.
  Evidence: exact-SHA manual full matrix `34682716776` passed all 20 jobs on
  `1406352345886c116ba7750369fc3a14df960b63`, including native platform,
  security, and performance checks; named exit acceptance is recorded in
  [acceptance-record.md](docs/acceptance-record.md#phase-0-exit-acceptance).

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
- [x] P1-04 Prototype and validate Linux Landlock/namespaces, Windows
  AppContainer/job controls, and an honest macOS fallback. Local capability
  evidence: [Phase 1 P1-06–P1-10 bundle](docs/release-evidence/phase-1-p1-06-p1-10.md);
  local Linux direct-enforcement and namespace tests now cover the implemented
  Landlock/bubblewrap paths, while exact-source hosted run `34604368677`
  and current exact-SHA full matrix `34682716776` validate the namespace,
  Windows job-object, and macOS process-group paths; current-tree hosted
  Landlock evidence is now retained. The Windows follow-up creates an
  ephemeral AppContainer with explicit read/execute ACLs, launches suspended
  through `STARTUPINFOEX` security capabilities, attaches the Job Object before
  resume, and independently attests the child token from the parent (including
  the package SID, low-integrity token, and zero network capabilities). Native
  Windows proof is recorded in the dated follow-up in the linked evidence
  bundle. Named platform-owner review remains a separate release gate.
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
  native platforms. Exact-HEAD manual run `35508065704` passed all 20 jobs,
  including native platform, artifact lifecycle, soak, fuzz, performance,
  security, and recovery paths; the complete Phase 1 exit record and named
  platform-owner acceptance remain outstanding.

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
- [x] P2-06 Implement a typed GitHub REST transport with an explicit API
  version, bounded/redacted bodies, pagination, request IDs, retry-after/rate
  limits, conditional reads, and durable reconciliation. Evidence: [Phase 2
  P2-06–P2-10 bundle](docs/release-evidence/phase-2-p2-06-p2-10.md).
- [x] P2-07 Bind provider host/account/owner identity explicitly and test
  GH_TOKEN, GITHUB_TOKEN, multi-account, revoked/expired, wrong-owner, and
  wrong-host cases. Evidence: [Phase 2 P2-06–P2-10
  bundle](docs/release-evidence/phase-2-p2-06-p2-10.md).
- [x] P2-08 Add least-privileged GitHub App installation-token support while
  retaining gh as an optional local bootstrap/user-auth adapter. Evidence:
  [Phase 2 P2-06–P2-10 bundle](docs/release-evidence/phase-2-p2-06-p2-10.md).
- [x] P2-09 Define and test a GitHub Enterprise Server support/version policy.
  Evidence: [Phase 2 P2-06–P2-10 bundle](docs/release-evidence/phase-2-p2-06-p2-10.md).
- [x] P2-10 Optionally project redacted verification evidence as a head-SHA
  bound Check Run without making it a source of truth. Evidence: [Phase 2
  P2-06–P2-10 bundle](docs/release-evidence/phase-2-p2-06-p2-10.md).
- [x] P2-11 Add an optional, read-only-first MCP surface for status, plan,
  explain, and logs; require separate domain consent for verify/publish.
  Evidence: [P2-11 and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).

### 2026-09-07 local implementation slice

P2-06 through P2-10 now have a typed local implementation and deterministic
contract evidence in [Phase 2 P2-06–P2-10 bundle](docs/release-evidence/phase-2-p2-06-p2-10.md): current-version bounded REST transport, explicit host/account/owner binding,
durable no-duplicate reconciliation, least-privilege in-memory GitHub App
installation tokens, Enterprise version negotiation, and an optional redacted
head-SHA Check Run projection. The support window is documented in
[provider support policy](docs/provider-support.md). The five task boxes are
complete against the linked implementation and acceptance evidence. The
private disposable provider requirement is also evidenced by an exact
owner/name/visibility/ref/SHA canary and verified cleanup. The separate Phase
2 release gate remains open until exact App permission, native-artifact, and
named-review requirements are executed and reviewed.

- [ ] Phase 2 gate: all advertised adapters have fixture and native install
  evidence, and the exact alpha provider path passes a disposable private
  GitHub canary with allowlisted cleanup.
  Current exact-HEAD canary `35508083681` passed for private target
  `udayvarmora07/autogit-v1-test-35508083681`, verified `main` at
  `8fd321c37412aa04f70df8667d4132286b40a150`, and confirmed allowlisted
  cleanup. Current native adapter/artifact jobs also passed in manual run
  `35508065704`; named permission/release review remains outstanding.

## Phase 3 — product UX and operations

- [x] P3-01 Refactor the 2,143-line CLI file into command parsers,
  application services, presenters, and composition root with golden behavior
  tests. Evidence: [P2-11 and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-02 Add version, full per-command help, examples, shell completions,
  man pages, prerequisites, and a clean-machine quickstart. Evidence: [P2-11
  and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-03 Define stable stdout/stderr, human/JSON, exit-code, error-cause,
  redaction, and remediation contracts; remove duplicated error codes. Evidence:
  [P2-11 and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-04 Expand doctor into an offline capability/health report covering
  binary, Git, state, isolation, adapters, provider mode, and safe next steps.
  Evidence: [P2-11 and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-05 Add durable operation status, explain, resume, cancel, runlog, and
  bounded undo for AutoGit-owned effects only. Evidence: [P2-11 and Phase 3
  bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-06 Add previewable config migrations, backups, rollback, and
  provenance for every owned config fragment. Evidence: [P2-11 and Phase 3
  bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-07 Add local correlated logs/traces/metrics and opt-in OpenTelemetry
  export with strict field/cardinality/privacy allowlists. Evidence: [P2-11
  and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-08 Establish and trend p50/p95/p99 budgets for hook, path
  observation, startup, DB growth, verification, and provider operations.
  Evidence: [P2-11 and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).
- [x] P3-09 Rewrite README/operator docs for installation, trust boundaries,
  limitations, workflows, recovery, upgrade, uninstall, and support. Evidence:
  [P2-11 and Phase 3 bundle](docs/release-evidence/phase-2-p2-11-phase-3.md).

## Phase 4 — verification, evals, and continuous quality

- [x] P4-01 Split fast presubmit, race, integration, 1,000-schedule soak,
  fuzz, canary, and release suites; keep presubmit p95 below five minutes.
  Evidence: [P4-01 quality-tier review](docs/acceptance-record.md#p4-01-quality-tier-review).
- [x] P4-02 Add shellcheck and direct failure/safety tests for canary,
  performance, and release shell scripts.
  Evidence: [P4-02 shell-safety review](docs/acceptance-record.md#p4-02-shell-safety-review).
- [x] P4-03 Execute built-artifact smoke tests for every claimed OS/arch and
  supported system Git range on native hosts.
  Evidence: [P4-03 native-artifact review](docs/acceptance-record.md#p4-03-native-artifact-review); exact-tag CI run `35497942544` passed all 20 jobs at `v0.1.1` / `0743443224dd809e652ea69d5d6b275eef29a4ce`, including native artifact jobs `106044355136`, `106044355251`, `106044355261`, `106044355270`, `106044355282`, and `106044355321` for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64/arm64.
- [x] P4-04 Expand fuzz/property/differential tests across JSON, path, ref,
  status, policy, migration, scanner, and provider boundaries.
  Evidence: [P4-04 fuzz-floor review](docs/acceptance-record.md#p4-04-fuzz-floor-review).
- [x] P4-05 Add disk-full, permission-loss, clock-change, lock-contention,
  signal/kill, network-stall, rate-limit, and partial-response chaos tests.
  Evidence: [P4-05 chaos and recovery review](docs/acceptance-record.md#p4-05-chaos-and-recovery-review).
- [x] P4-06 Build client/OS scenario evals that inspect final
  repository/index/ref/provider state rather than model narration.
  Evidence: [P4-06 scenario-evaluation review](docs/acceptance-record.md#p4-06-scenario-evaluation-review).
- [x] P4-07 Automate compatibility-window testing and expiry issues for Go,
  Git, SQLite, clients, GitHub API, MCP, and durable schemas.
  Evidence: [P4-07 compatibility-window review](docs/acceptance-record.md#p4-07-compatibility-window-review).
- [x] P4-08 Publish machine-readable requirement/threat/test evidence for the
  exact tagged commit and artifacts.
  Evidence: [P4-08 evidence-manifest review](docs/acceptance-record.md#p4-08-evidence-manifest-review); release run `35496909526` generated and retained `autogit.release-evidence.json` for `v0.1.1` / `0743443224dd809e652ea69d5d6b275eef29a4ce` with `tag_verified: true`, clean worktree, ten passed suites, six artifact hashes, and thirteen control records.

## Phase 5 — supply chain, governance, and distribution

- [x] P5-01 Decide and add LICENSE. Evidence: [Apache License 2.0](LICENSE)
  and the license statement in [README.md](README.md).
- [x] P5-01 Add SECURITY.md, CONTRIBUTING.md, CODE_OF_CONDUCT.md, CODEOWNERS,
  support policy, and CHANGELOG.md. Evidence: [governance and security-contact
  review](docs/acceptance-record.md#governance-and-security-contact-review).
- [x] P5-02 Add controlled dependency updates, license policy, dependency
  review, CodeQL, OpenSSF Scorecard, least workflow permissions, and pinned
  action update review. Evidence: [dependency and workflow policy review](docs/acceptance-record.md#dependency-and-workflow-policy-review).
- [x] P5-03 Build a tag-gated clean-room release workflow that rejects dirty,
  reused, version-mismatched, or incompletely tested artifacts. Evidence:
  [Phase 5 release workflow](docs/release-evidence/phase-5-release-workflow.md);
  exact-tag release run `35496909526` passed the quality and attestation jobs
  for `v0.1.1` after the clean-checkout evidence download fix in `0743443`.
- [x] P5-04 Produce SPDX/CycloneDX SBOMs, binary govulncheck evidence, signed
  checksums, and hosted SLSA provenance/attestations. Evidence:
  [Phase 5 release workflow](docs/release-evidence/phase-5-release-workflow.md);
  the exact-tag attestation job in run `35496909526` passed SBOM generation,
  six binary govulncheck scans, checksum verification, three attestations, and
  consumer verification.
- [x] P5-05 Compare release artifacts across independent runners/build paths
  and publish reproducibility evidence. Evidence:
  [Phase 5 release workflow](docs/release-evidence/phase-5-release-workflow.md);
  Ubuntu/macOS independent builds and the byte-for-byte comparison all passed
  in run `35496909526` for `v0.1.1`.
- [ ] P5-06 Publish verified GitHub Releases and add tested Homebrew plus
  winget/Scoop channels according to demonstrated platform demand. Evidence:
  [tag-gated release publication](docs/release-evidence/phase-5-release-workflow.md)
  published and consumer-verified `v0.1.2` at `abcd3fb`; the protected
  publication runner regenerated byte-identical Homebrew/Scoop metadata from
  `SHA256SUMS`. Package-repository publication and native clean-machine
  package evidence remain open.
- [ ] P5-07 Test native install, upgrade, downgrade, uninstall, checksum,
  signature, and rollback paths. Local raw-binary lifecycle evidence:
  [Phase 5 install drill](docs/release-evidence/phase-5-install-drill.md); the
  published `v0.1.2` bundle passed exact consumer verification and the native
  Linux lifecycle drill, while package-channel and clean-machine native tests
  remain open.
- [ ] P5-07 Define disclosure/patch SLAs, signing identity recovery, release
  rollback, and compromised-release drills. Evidence: [rollback and disclosure
  implementation evidence](docs/release-evidence/phase-5-rollback-drill.md),
  including explicit severity targets in [SECURITY.md](SECURITY.md) and a
  passing technical rollback drill alongside `v0.1.2`; named tabletop/security
  review remains required for release acceptance.

## Phase 6 — release progression

- [ ] Private alpha entry: Phases 0 and 1 complete; the advertised Phase 2
  subset complete; signed private artifacts and support owner ready.
- [x] Run and retain the disposable private GitHub canary evidence. Evidence:
  [Phase 2 P2-06–P2-10 bundle](docs/release-evidence/phase-2-p2-06-p2-10.md).
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

### 2026-09-11 exact-snapshot validation refresh

The P4-01 through P4-08 and P5-01 through P5-02 work packages have local
implementations and fresh exact-snapshot evidence in
[phase-4-quality.md](docs/release-evidence/phase-4-quality.md): named
test tiers, ShellCheck/failure tests, built-artifact smoke with the manifest's
system-Git floor, enforced 100,000 input fuzz budgets, deterministic chaos
cases, 20-case final-state scenario evaluation across all six registered
clients, bounded native platform traces, compatibility expiry automation,
exact-commit/artifact evidence, governance files, and dependency/security
workflows. The latest hosted evidence snapshot `16c6325` passed the full
20-job matrix (`34600488282`); the current local manifest was regenerated at
`16c6325` and records 10/10 suites and six artifact hashes. The push-triggered
core and security runs (`34600109706`, `34600109672`) also passed at the same
commit. Compatibility review `34590952233` remains historical evidence for
`97ea27f`; the current local compatibility suite passed in the regenerated
manifest. The current dedicated-token
private canary dispatch (`34489797141`) passed against `36c045f` for owner
`udayvarmora07`, and the allowlisted repository was confirmed absent after
cleanup. The remaining P4 checkboxes stay open until the plan's exact-tag,
native, signing, and remaining named-review acceptance evidence is executed.
The P5-03/P5-05
implementation details and local checks are in
[phase-5-release-workflow.md](docs/release-evidence/phase-5-release-workflow.md);
they do not represent hosted signing or cross-runner evidence.

The current-tree manual full matrix `34627202246` passed all 20 jobs against
`a07fd84cb4b345e4dccdd3f950bd82dd662f5d87`, including native Linux/macOS/
Windows checks, all six native artifact lifecycle drills, fuzz, soak,
reproducibility, security, and dependency-policy jobs. The commit is a
documentation-only descendant of the generated evidence snapshot
`06ba87508ff53abc8b0001885e8341b6c07ad27a`; no acceptance checkbox changes
follow from this run. Exact-tag binding, native macOS isolation/acceptance,
provider/native adapter evidence, and named review remain external gates.

The 2026-09-11 follow-up fixed the Windows Git Bash package-metadata hashing
path in `scripts/generate-package-metadata.sh`, using `cygpath` and PowerShell
`Get-FileHash` with a regression test for the Windows-shell branch. The clean
manifest now binds to `06ba87508ff53abc8b0001885e8341b6c07ad27a` and records
10/10 local suites, six artifact hashes, and `tag_verified: false`. Core run
`34610742466` and security run `34610742472` passed against that exact SHA;
the earlier `e432bf5` core failure is retained as diagnostic history. This
fixes the implementation-level CI defect without changing any acceptance
checkbox or the NO-GO release posture.

### 2026-09-15 Windows AppContainer follow-up

The clean source commit
`c1a94d4902e13d454ef87eb9c5d394c604bea608` passed the push-triggered core
workflow's native Windows job in run `34935126986` (job
`104271291760`). The follow-up exercises a unique ephemeral AppContainer with
zero declared capabilities, parent-side package-SID/low-integrity/zero-
capability attestation before resume, Job Object attachment before resume,
explicit read/execute ACLs for the allowlist, denied unlisted reads and
writes, and restoration of the original owner/group plus DACL ACE/protection
semantics after cleanup. Windows may normalize the system-managed
`SE_DACL_AUTO_INHERITED` (`AI`) control bit while applying a directory DACL;
the test allows that documented normalization but remains strict about the
owner, group, ACEs, and DACL protection state. AppContainer standard output
and error use inherited regular temporary files, avoiding the Go Windows
synchronous-pipe startup probe deadlock; a parent-side path monitor preserves
the configured output limit and output is collected after process exit. This
closes the Windows implementation gap under P1-04; native macOS isolation,
named platform-owner review, and the remaining exact-tag/release gates stay
open.

The current-tree manual dispatch `34961479624` passed all 20 core jobs against
`a51eb9718bac0e3e514e196fa4e1156a47865ade`, including all six native artifact lifecycle drills, native
platform checks, fuzz, soak, reproducibility, security, cross-build, and
dependency-policy checks. It adds current validation for P4-03 but leaves the
exact-tag-bound P4-08/release evidence gate unchanged.

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
- [x] PR-011 Backup/restore/integrity/retention, including durable jobs/receipts
  restore checks and subprocess crash-recovery coverage. Evidence: [Phase 1
  P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).
- [x] PR-012 Verification isolation capability baseline, including a native
  process-supervisor capability probe and fail-closed stronger-tier behavior.
  The Windows AppContainer and parent-side token-attestation follow-up is
  complete under P1-04; native macOS filesystem/network isolation and named
  platform-owner acceptance remain separate gates.
  Evidence: [Phase 1 P1-01–P1-05 bundle](docs/release-evidence/phase-1-p1-01-p1-05.md).

## Promotion rule

Do not convert an unchecked release gate to complete because code exists or a
single local test passes. Completion requires the implementation-plan
acceptance evidence, exact commit/artifact identity, required native platforms,
and a named reviewer. A blocked public operation may retain a safe local
AutoGit commit; no convenience path can waive consent, ownership, immutable
evidence, isolation policy, or exact destination postconditions.
