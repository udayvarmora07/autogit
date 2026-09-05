# AutoGit v1 test strategy

Status: Draft for Phase 0 acceptance  
Last updated: 2026-09-05

## 1. Purpose and test contract

This document turns the requirements, lifecycle, and threat model into a
release test plan. It covers the Go v1 core and its adapters; the installed
Bash hook under `~/.agents/hooks` is a behavioral reference and compatibility
fixture, not the v1 security boundary.

The primary quality invariant is:

> No test may demonstrate a path from an untrusted event or repository state
> to an unconsented, ambiguous, unverified, destructive, or wrong-destination
> commit/push.

Tests must use dependency-injected Git and provider ports wherever possible.
The only tests allowed to contact GitHub use a dedicated disposable provider
account/organization, uniquely tagged repositories, and an explicit cleanup
allowlist. No test discovers, modifies, commits to, or deletes a user project.

The normative inputs are the [product requirements](product-requirements.md),
[lifecycle](lifecycle.md), [threat model](threat-model.md),
[architecture](architecture.md), and the [canonical event contract](event-contract.md).

## 2. Test layers and minimum release targets

Counts below are minimum independent cases, not a claim that parameterized
examples are equivalent to a single happy-path test. Generated property and
fuzz executions are counted separately.

| Layer/suite | Phase 0 current coverage | v1 minimum target | Primary purpose |
| --- | ---: | ---: | --- |
| Prototype regression (`test-auto-git.sh`) | 16 | 16 retained | Consent, verification, guards, gates, remotes, idempotence |
| Prototype real-world (`test-auto-git-realworld.sh`) | 53 | 53 retained | Adapter directory sources, path/artifact/lockfile and lifecycle cases |
| Prototype advanced (`test-auto-git-advanced.sh`) | 105 | 105 retained | Marker permutations, branches, classifications, safety |
| Prototype security regression (`test-auto-git-security-regressions.sh`) | 3 | 3 retained | Local-only provider boundary and argument-safe ref regressions |
| Domain/policy/state unit suite | Not present | >=120 | State transitions, policy precedence, idempotency, error codes |
| Event/schema and adapter contract suite | Not present | >=48 (6 adapters x 8) | Canonical envelope, capability degradation, replay and exit mapping |
| Disposable Git/worktree suite | Partial prototype | >=80 | Ownership, candidate trees, path safety, refs, hooks, worktrees, LFS |
| Security/verification suite | Partial prototype | >=70 | Secret, command, symlink, resource, output, and stale-evidence controls |
| Provider fake/contract suite | Mocked `gh` only | >=40 | Create, collision, visibility, remote identity, retry, postconditions |
| Resilience/concurrency/crash suite | Not present | >=50 | Leases, duplicate/out-of-order events, kill/restart at every intent |
| Portfolio/publication suite | Partial prototype | >=15 | Public consent, readiness report, README/license/destination quality |
| Performance/compatibility suite | Not present | >=12 benchmarks | p95 latency, scale, binary/OS behavior |
| **Deterministic total** | **674 Go cases** | **>=609** | All must-level acceptance paths |

The 177 prototype scenarios are a regression floor. A v1 release cannot claim
coverage merely because those shell scripts pass: they do not prove session
ownership, durable state recovery, safe provider identity, or cross-platform
behavior.

## 3. Execution tiers

### Presubmit smoke

Run on every change and in under five minutes: schema validation; consent and
decline; path/root safety; owned candidate happy path; secret/conflict/size
blocks; verification binding; local commit; local-only no-network behavior;
duplicate event; and one adapter contract case per adapter.

### Core merge suite

Run on every merge: all domain, policy, event, staging, verification, commit,
fake-provider, and adapter contract tests. No network or credentials are
permitted. Failures block the merge.

### Full regression

Run nightly and before a release: all disposable Git/worktree cases, security
cases, generated/path matrix, retry and crash cases, and the full adapter
matrix. Record duration, pass rate, and flaky retries.

### Extended release suite

Run on release candidates: OS matrix, performance benchmarks, provider
contract tests, one controlled GitHub canary, public portfolio review, and
dependency/license/supply-chain checks. A flaky safety or provider test is a
release failure, not a candidate for silent quarantine.

## 4. Current prototype baseline

At the Phase 0 audit snapshot, the reference scripts passed all of the
following in disposable local repositories/remotes. They remain the regression
floor; rerun them after any prototype hardening change and update this snapshot
if the behavioral contract intentionally changes:

- `tests/test-auto-git.sh`: 16 cases covering consent-before-init, declined
  tracking, plan mode, deterministic noninteractive verification, gates and
  stale gates, generated files, Conventional Commit shape, remote defaults,
  and push failure retention.
- `tests/test-auto-git-realworld.sh`: 53 cases covering marker aliases and
  normalization, all documented directory sources, retry/idle behavior,
  invalid Python/whitespace/conflict/secret guards, artifacts and lockfiles,
  nested roots, setup hygiene, local remote push, and message classes.
- `tests/test-auto-git-advanced.sh`: 105 cases covering 16 marker forms, 20
  path forms, 14 lockfiles, 18 artifact classes, lifecycle changes, six branch
  names, 12 message classifications, GPG handling, trailer sanitization, and
  secret filenames.
- `tests/test-auto-git-security-regressions.sh`: 3 cases covering local-only
  provider isolation, missing-identity behavior, and option-looking branch refs.

Known limitations are deliberately recorded: the prototype uses shared
whole-worktree staging, has no durable session database or crash protocol, and
uses mocked/local remotes rather than proving provider identity. v1 tests must
prevent these limitations from becoming implementation shortcuts.

On 2026-09-05, the reference scripts were recovered from the installed
compatibility checkout at `~/.agents/hooks/tests` and rerun in their disposable
local-repository environments: 16 + 53 + 105 + 3 = 177 scenarios passed. This
is evidence that the legacy regression floor is runnable, but it does not
replace Go v1 coverage because those scripts exercise the Bash compatibility
hook rather than the new core.

The Go v1 suite emits 674 passing named test cases/subtests in the same audit
environment when run with `go test -count=1 -json ./...`. CI enforces the
documented >=609 deterministic floor from that stream. This count is a release
floor, not evidence for the separate native-OS, randomized crash/concurrency,
provider-canary, performance, or phase-promotion gates.

## 5. Test design by risk

### Unit and state-machine tests

Use table-driven tests for every lifecycle transition and reason code in
`lifecycle.md`. Assert invariants after both success and error paths: no
mutation before consent, no publication from weak completion evidence, digest
mismatch invalidation, distinct commit/push facts, and no destructive fallback.
Use deterministic clocks, UUIDs, filesystem identities, and retry schedules.

### Adapter and event-contract tests

Each initial adapter (Codex, Claude Code, Cursor, Gemini CLI, OpenCode, and
CommandCode) must have the same contract fixture set: valid event, malformed
JSON, unknown major, duplicate ID, ID collision, missing capabilities, unsafe
root/path, sequence gap, and client-specific result/exit mapping. Fixtures must
prove adapters report facts and never perform Git mutation themselves.

### Disposable Git and filesystem tests

Create repositories beneath a per-run temporary directory and use a separate
temporary `HOME`, `PATH`, Git config, credential helper, and provider endpoint.
Exercise existing dirty index/worktree state, isolated candidate indexes,
nested repositories, linked worktrees, submodules, LFS pointers, case-only
renames, symlinks, control characters, Unicode, spaces, path traversal,
branch/ref option-looking names, custom hooks/filters, and unusual remotes.

For every ownership case, snapshot `HEAD`, index, worktree bytes, modes, and
pre-existing paths before AutoGit and compare them after the operation. Any
unowned byte or index entry changed is a failure.

### Provider tests without production risk

The default provider fake records exact owner/name/visibility/ref/SHA calls and
can inject authentication, collision, timeout, offline, non-fast-forward,
protected-branch, secret-scanning, and false-success responses. Contract tests
must assert no force/mirror/all-ref/delete operation and must verify the remote
postcondition.

The optional GitHub canary uses a dedicated test owner, a unique
`autogit-v1-test-<run-id>` name, an explicit disposable label/description, and
an allowlisted cleanup job. Tests may create only repositories whose generated
name and owner match the run manifest. Public tests are opt-in, run against a
throwaway repository, verify private-by-default separately, and must not use a
developer token or user repository. Cleanup failures block the suite and are
reported for manual cleanup; cleanup must never use a broad delete pattern.

### Verification and security tests

Use fixtures for secrets in filenames, text, binary/encoded content, LFS,
submodules, and prior history; merge markers; oversized blobs; unsafe
symlinks; malformed paths; hostile Git config/hooks/filters; malicious
verification commands; output/log redaction; and provider size limits. Assert
that failures expose a category/path and never the secret value.

Bind every scan, verifier, message, and commit to candidate tree, base, policy,
and verifier-set digests. Mutate each input after the corresponding evidence
and assert publication is invalidated.

### Property and fuzz testing

Fuzz the JSON envelope, duplicate-key/unknown-field handling, path
canonicalization, repository/remote/name/ref validation, Conventional Commit
parser, marker/policy normalization, event ordering, and redaction. Properties
include:

1. Invalid input never mutates Git/provider state or falls back to `$PWD`.
2. Replaying an event is observationally idempotent; same ID/different digest
   is rejected.
3. No generated argument can become a shell or Git option injection.
4. A candidate digest change cannot reuse verification or commit evidence.
5. Local-only policy makes zero provider calls.
6. Push retries retain one commit SHA and never force or broaden the ref.

The release gate is at least 100,000 generated inputs per parser/property set,
with all corpus failures minimized, committed as regression fixtures, and
replayed in CI.

### Concurrency, interruption, and crash testing

Run two or more sessions against one worktree and against linked worktrees.
Inject overlap, disjoint paths, event reordering, duplicate events, lease
expiry, stale takeover, index/HEAD changes, remote ref changes, and provider
replies arriving after process termination. Kill AutoGit at every persisted
intent boundary (event receipt, candidate, verification, commit, remote create,
push, and result recording), restart, reconcile, and assert no duplicate commit,
wrong destination, lost user change, or false success.

Use a deterministic fault-injection clock and process wrapper; never use a
real user's Git lock or repository. At least 1,000 randomized schedules and
all named lifecycle boundaries are required before release.

## 6. OS and environment matrix

| Environment | Required coverage |
| --- | --- |
| Ubuntu latest, amd64 | Full smoke/core/full/extended suites |
| Debian stable, amd64 | Core, disposable Git, security, provider fake |
| macOS latest, arm64 and amd64 runner where available | Smoke/core, filesystem/path, Git/provider fake, performance |
| Windows 11, amd64 | Smoke/core, native path/locking/process cancellation, Git/provider fake |
| Windows Git Bash adapter environment | Compatibility fixtures for installed hook/adapters |
| Linux arm64 | Cross-compiled binary smoke and core contract tests |

The v1 binary must meet the supported-platform claim in `NFR-POR-001`. Tests
must use platform path APIs and avoid asserting Unix-only separators, mtime
resolution, signal names, or shell behavior. A test may be marked unsupported
only with a documented capability and safe degradation assertion.

## 7. Acceptance gates and measurable release criteria

- **GATE-001 Baseline:** all 177 prototype scenarios pass unchanged in their
  disposable environment.
- **GATE-002 Traceability:** every must-level FR and every NFR has at least one
  named acceptance case in the matrix below; no unowned requirement is marked
  complete.
- **GATE-003 Consent/ownership:** 100% of no-consent, decline, ambiguous path,
  concurrent overlap, and pre-existing-dirty fixtures produce zero unauthorized
  Git/provider mutations.
- **GATE-004 Safety:** all secret, conflict, size, path, symlink, option
  injection, hostile-command, and remote-identity fixtures block safely; zero
  blocked fixture commits or pushes.
- **GATE-005 Evidence:** every stale candidate/policy/base/verifier mutation
  invalidates evidence; no stale verification reaches commit/push.
- **GATE-006 Recovery:** 1,000 randomized crash/concurrency schedules plus all
  named fault points produce no duplicate commit/push, lost local commit, or
  false success.
- **GATE-007 Publication:** provider fake suite passes; local-only has zero
  provider calls; one isolated GitHub canary proves exact owner/name/visibility/
  ref/SHA postconditions and safe cleanup.
- **GATE-008 History/portfolio:** 100% of generated commits pass Conventional
  Commit validation, subjects are <=72 characters, summaries are meaningful,
  forbidden trailers are absent, and a public canary passes README/license/
  verification/secret/artifact checks.
- **GATE-009 Performance:** no-candidate hook p95 <150 ms; baseline/status p95
  <1 s for 100,000 tracked paths, excluding network/LFS; record CPU/memory and
  test/build durations separately.
- **GATE-010 Reliability:** core suite has >=99.5% pass rate across 20 repeated
  runs; no safety/provider test may be flaky. Any flaky test is quarantined with
  a tracked defect and cannot satisfy a release gate.
- **GATE-011 OS compatibility:** all required matrix jobs pass, or each
  unsupported capability has an explicit safe-degradation result and release
  note.
- **GATE-012 Privacy:** redaction tests find zero secret, prompt, source,
  diff, token, or remote leakage in default logs, state, messages, or results.

## 8. Requirements-to-tests traceability

The following table maps every functional requirement prefix and every NFR to
planned acceptance coverage. Individual IDs listed in a row inherit all tests
in that row; adding an ID requires extending the row before implementation is
considered complete.

| Requirement IDs | Planned acceptance coverage |
| --- | --- |
| FR-CNS-001..007 | `consent_matrix`, `policy_state_machine`, `public_canary`, `config_cli`, `local_only_network_boundary` |
| FR-REP-001..007 | `root_and_identity_matrix`, `repository_init`, `disposable_git`, `provider_contract`, `collision_and_remote_reconcile` |
| FR-SES-001..008 | `event_contract`, `session_lifecycle`, `ordering_replay`, `concurrency_crash` |
| FR-GIT-001..007 | `ownership_candidate`, `dirty_index_worktree`, `argv_ref_safety`, `git_transaction_recovery`, `offline_push` |
| FR-IGN-001..004 | `ignore_policy_matrix`, `artifact_lockfile`, `transient_state_isolation` |
| FR-VER-001..005 | `tree_digest_binding`, `verifier_policy`, `resource_limits`, `verification_failure`, `no_verifier_public_block` |
| FR-SEC-001..003 | `security_fixture_matrix`, `redaction`, `exception_scope_and_expiry`, `history_scan` |
| FR-CMT-001..005 | `message_parser`, `message_evidence`, `message_quality`, `trailer_allowlist`, `logical_task_commit` |
| FR-PUB-001..005 | `safe_branch`, `protected_branch`, `public_preflight`, `license_readiness`, `portfolio_canary` |
| FR-ADP-001..002 | `six_adapter_contracts`, `capability_degradation`, `adapter_no_mutation` |
| FR-INS-001..002 | `install_backup_merge`, `upgrade_uninstall_ownership`, `installer_idempotence` |
| FR-OPS-001..002 | `cli_status_plan_doctor_logs_retry`, `local_commit_push_notification`, `redacted_diagnostics` |
| NFR-SAF-001 | `destructive_operation_denials`, `ownership`, `argv_ref_safety`, `security_fixture_matrix` |
| NFR-REL-001 | `idempotency_replay`, `provider_retry`, `concurrency_crash`, `fault_injection` |
| NFR-PER-001..002 | `performance_benchmarks`, `large_repo_disposable` |
| NFR-SEC-001 | `redaction`, `secret_fixture_matrix`, `message_and_state_scan` |
| NFR-PRV-001 | `telemetry_off`, `log_db_redaction`, `provider_input_minimization` |
| NFR-POR-001 | `os_matrix`, `cross_compile_smoke`, `native_path_lock_process_tests` |
| NFR-COM-001 | `schema_compatibility`, `adapter_manifest`, `unknown_minor_major_cases` |
| NFR-OBS-001 | `audit_transition_contract`, `correlation_id`, `side_effect_outcome` |
| NFR-TST-001 | `fake_ports`, `deterministic_state`, `network_denied_core_suite` |

## 9. Test reporting and maintenance

Every run records suite, commit, OS/architecture, adapter/provider mode,
duration, pass/fail, retry count, and redacted failure reason. Reports must
separate product failures from environment failures and identify excluded
requirements. A production defect adds a deterministic regression fixture
before its fix is released. Flaky tests are quarantined within one business
day, tracked to resolution, and never silently retried into a green release.

Provider canary artifacts contain only run IDs, repository identity within the
allowlist, exact expected/observed postcondition categories, and cleanup
status—never tokens, prompts, source, diffs, or full remote credentials.
