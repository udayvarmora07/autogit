# AutoGit v1 implementation plan

Status: Execution in progress — foundation slice implemented; Phase 1 exit not claimed  
Last updated: 2026-09-01

## 1. Objective and delivery shape

Build a local-first Go modular monolith that receives versioned, at-least-once
events from supported agentic clients; independently reconciles the repository;
constructs a session-owned candidate; verifies and scans that candidate; creates
an attributable Conventional Commit; and optionally publishes an exact commit
to an explicitly approved destination.

The implementation follows the [architecture](architecture.md),
[deterministic lifecycle](lifecycle.md), [product requirements](product-requirements.md),
[threat model](threat-model.md), and [test strategy](test-strategy.md). The
installed Bash hook remains a compatibility reference until the Go engine has
passed the relevant gates; it is not the v1 security boundary.

## 2. Principles for execution

1. Prove read-only discovery and consent before any Git/provider mutation.
2. Preserve user work: candidates are session-owned and ambiguous ownership
   blocks rather than guessing.
3. Treat adapters as untrusted reporters; only core reconciliation creates
   durable facts or schedules side effects.
4. Bind safety, verification, message, commit, and push evidence to immutable
   candidate/policy/base digests.
5. Use system Git through argument-safe ports and a provider port through `gh`
   initially; keep fakes deterministic and make network optional in tests.
6. Persist intent before side effects, reconcile after crashes, and retry only
   the same idempotent job.
7. Keep private/local operation safe and useful; public publication requires a
   separate explicit choice and a visible pre-publication summary.
8. Make the smallest safe release at each phase. A local checkpoint may be
   retained when publication is blocked, but no unsafe path is an exception.

## 3. Phase status and exit model

| Phase | Status | Outcome |
| --- | --- | --- |
| Phase 0 — contract | Artifacts drafted; review/freeze pending | Agreed product, threat, event, lifecycle, architecture, ADR, and test contract |
| Phase 1 — foundation | Implementation in progress; exit not claimed | Buildable Go core with durable state and validated ingress |
| Phase 2 — safe local workflow | Implementation in progress; exit not claimed | Consent through verified local commit with session ownership |
| Phase 3 — integrations and recovery | Implementation in progress; exit not claimed | Adapters, provider jobs, retries, concurrency, and crash reconciliation |
| Phase 4 — private alpha | CI/cross-build definition evidence; native/release gates open | Dogfoodable local/private release on supported OSes |
| Phase 5 — public beta | Local public-preflight implementation; canary/beta gates open | Explicit-public, portfolio-quality, supportable beta release |

No phase is complete because its code exists. The phase exit gate requires the
listed deliverables, tests, security invariants, documentation, and review.

## 4. Work packages

Work package IDs are stable planning identifiers. A package may be split into
implementation issues, but its exit evidence must remain attached to this ID.

### Phase 0 — contract and readiness

| ID | Work | Dependencies | Deliverable and exit test |
| --- | --- | --- | --- |
| `P0-01` | Freeze terminology and requirement IDs | None | Cross-document review; all must-level FR/NFR IDs are traceable in the [test strategy](test-strategy.md) |
| `P0-02` | Freeze event, result, capability, and error semantics | `P0-01` | [`event-contract.md`](event-contract.md) and schema agree; valid/invalid/replay fixtures are defined |
| `P0-03` | Freeze lifecycle, state, and side-effect invariants | `P0-01`, `P0-02` | Lifecycle transition table, ADRs, and threat invariants have no contradiction |
| `P0-04` | Freeze test, provider-safety, and OS strategy | `P0-01..03` | Test targets, disposable provider policy, fuzz/crash plan, and release gates are approved |

Phase 0 exits when the [product requirements](product-requirements.md) exit
criteria are satisfied and Phase 1 work can start without a product-policy
decision. No provider or user-project operation is part of Phase 0.

### Phase 1 — foundation

| ID | Work | Dependencies | Deliverable and tests |
| --- | --- | --- | --- |
| `P1-01` | Go module, CLI skeleton, configuration, structured errors | `P0-02`, `P0-03` | Reproducible build; command/error contract tests; no shell execution from ingress |
| `P1-02` | SQLite schema, migrations, repositories, permissions, retention | `P0-03` | Migration/rollback, transaction, corruption, and restrictive-permission tests |
| `P1-03` | Canonical event/result types, JSON Schema validation, receipts, digesting | `P0-02`, `P1-02` | Schema/adapter contract, size, malformed input, duplicate ID, collision, and unknown-major tests |
| `P1-04` | System Git, filesystem, clock, process, and provider ports | `P0-03`, `P1-01` | Deterministic fakes; argument/path/redaction tests; network-denied core suite |
| `P1-05` | Correlation IDs, redacted audit events, doctor/status plumbing | `P1-02..04` | Audit transition and diagnostic privacy tests |

Phase 1 exits when the core builds on the required platforms, accepts only the
canonical contract, persists receipts transactionally, and passes all
foundation smoke/core tests without network credentials.

### Phase 2 — safe local workflow

| ID | Work | Dependencies | Deliverable and tests |
| --- | --- | --- | --- |
| `P2-01` | Canonical repository/worktree discovery and consent policy | `P1-01..05` | `enable/disable/status/plan/config explain`; root, policy precedence, consent, plan, and local-only tests |
| `P2-02` | Session/task/prompt state, completion evidence, causal buffer | `P1-02..03`, `P2-01` | Event ordering, weak-stop handling, queue-unknown, replay, and synthetic-task tests |
| `P2-03` | Baselines, ownership attribution, isolated candidate/index construction | `P1-04`, `P2-02` | Dirty-worktree, overlap, worktree, symlink, Unicode/control-path, rename/delete, and concurrent writer tests |
| `P2-04` | Candidate security policy and trusted verification runner | `P1-04`, `P2-03` | Secret/history/path/size/conflict tests; bounded command, output, cancellation, digest, and no-verifier tests |
| `P2-05` | Conventional Commit evidence/composition and local Git transaction | `P2-03..04` | Message quality/parser/trailer tests; intent-before-effect, exact tree, no-force, and crash reconciliation tests |

Phase 2 exits when a consented session can safely produce one verified local
commit containing only owned changes, while preserving unrelated user state on
all failure paths. No remote provider is required for this exit.

### Phase 3 — integrations and recovery

| ID | Work | Dependencies | Deliverable and tests |
| --- | --- | --- | --- |
| `P3-01` | Durable coordinator, leases, outbox, retries, crash reconciliation | `P1-02`, `P2-02`, `P2-05` | Fault injection at every intent boundary; duplicate/out-of-order, lease, restart, and idempotency tests |
| `P3-02` | GitHub provider port via `gh`: identity, create, remote, push, postconditions | `P2-01`, `P2-05`, `P3-01` | Fake-provider contract, collision/auth/offline/non-fast-forward/protection tests; no force/all-ref/delete |
| `P3-03` | Codex, Claude Code, Cursor, Gemini CLI, OpenCode, CommandCode adapters | `P1-03`, `P2-02` | Six adapter contract suites, capability degradation, install invocation, and no-adapter-mutation tests |
| `P3-04` | Owned adapter installation, upgrade, backup, uninstall | `P3-03`, `P1-05` | Config merge/backup/rollback/idempotence and ownership-preservation tests |
| `P3-05` | `sync`, `verify`, `retry`, `logs`, notifications, result/exit mapping | `P3-01..04` | CLI black-box tests, redacted diagnostics, local-commit/push-failure notification tests |

Phase 3 exits when supported clients produce equivalent canonical behavior,
remote jobs are exact-SHA/idempotent, and process crashes or network failures
cannot create duplicate/wrong pushes or lose a local commit.

### Phase 4 — private alpha

| ID | Work | Dependencies | Deliverable and tests |
| --- | --- | --- | --- |
| `P4-01` | Cross-platform packaging and CI | `P3-03..05` | Required Linux/macOS/Windows/ARM smoke and core matrix; signed/reproducible build evidence |
| `P4-02` | Controlled dogfood on local/private repositories | `P3-01..05`, `P4-01` | Test-only repository cohort; no user project/GitHub mutation; opt-in private publication and rollback drills |
| `P4-03` | Performance, observability, support diagnostics, documentation | `P3-05`, `P4-01` | NFR p95 benchmarks, `doctor/status/logs`, runbooks, redaction review, and defect regression fixtures |

Private alpha exits only when the full regression suite, safety gates, OS
matrix, and recovery drills pass for a bounded internal cohort. Alpha does not
authorize default public publication.

### Phase 5 — public beta

| ID | Work | Dependencies | Deliverable and tests |
| --- | --- | --- | --- |
| `P5-01` | Public preflight and portfolio readiness | `P2-04..05`, `P3-02`, `P4-03` | Explicit visibility/destination/file/scan/verification/README/license summary; placeholder/readiness checks |
| `P5-02` | Isolated public canary and beta release process | `P4-01..03`, `P5-01` | Dedicated tagged provider cohort; exact owner/name/visibility/ref/SHA postconditions and allowlisted cleanup |
| `P5-03` | Beta support, upgrade, incident, and rollback operations | `P3-04..05`, `P5-02` | Release notes, compatibility manifest, migration/rollback drills, security response and support triage |

Public beta exits when `FR-PUB-*`, `FR-CMT-*`, `FR-VER-*`, `FR-SEC-*`, and all
non-negotiable threat invariants pass the extended release suite. The beta
must preserve private-by-default behavior and must never use a developer's
personal project or token as a test fixture.

## 5. Cross-package dependencies and sequencing

```text
Phase 0 contract
      |
      v
Phase 1 types/state/ports
      |
      v
Phase 2 consent -> ownership -> guards/verification -> local commit
      |
      v
Phase 3 coordinator + provider + adapters + operations
      |
      v
Phase 4 private alpha / OS and performance
      |
      v
Phase 5 public preflight -> canary -> public beta
```

Tests and fakes begin with each package and may proceed in parallel when their
ports are stable. Provider work cannot unblock local safety work. Adapter work
cannot define core semantics. Public canary work cannot begin until exact
destination, visibility, history, and cleanup postconditions are implemented.

## 6. Model allocation and work protocol

The project uses the requested two-model division:

| Model | Authorized role | Reasoning policy |
| --- | --- | --- |
| 5.6 Sol | Main planning/review model only | High reasoning for architecture, requirements trade-offs, review, risk-gate decisions, and final acceptance review; it does not perform implementation file/code/test work |
| 5.6 Luna | Actual implementation, documentation, and test work | Choose reasoning by task: medium for bounded docs/mechanical changes, high for security/state/concurrency/provider work, and focused lower effort only for mechanical validation when risk is low |

Every work package has one written objective, dependency list, acceptance
tests, and a review handoff. Luna reports exact paths, commands, results, and
unresolved risks. Sol reviews against the normative documents and may return a
package for rework; review does not silently change product policy.

## 7. Risk gates

The following gates apply throughout implementation; later phases cannot waive
an earlier gate:

- **RG-01 Consent:** no Git/provider mutation without recorded tracking
  consent; public visibility requires separate explicit consent.
- **RG-02 Ownership:** no unconditional whole-worktree staging; ambiguous or
  overlapping paths are excluded or approved explicitly.
- **RG-03 Evidence:** guards, verification, message, commit, and push evidence
  name the same candidate/base/policy digests and are invalidated on change.
- **RG-04 Execution safety:** no shell interpolation, unsafe path/ref/option
  handling, unbounded command, secret output, or implicit provider operation.
- **RG-05 Side effects:** durable intent precedes Git/provider effects; exact
  commit SHA/ref postconditions and crash reconciliation are mandatory.
- **RG-06 Recovery:** offline/auth/non-fast-forward failures retain local work,
  report incomplete publication, and retry only the same safe intent.
- **RG-07 Privacy:** no prompt, source, diff, token, credential, remote, or
  secret leakage in default logs/state/results.
- **RG-08 Provider safety:** tests use fakes or tagged disposable resources;
  cleanup is allowlisted and never broad or destructive.
- **RG-09 Release quality:** required OS/performance/reliability thresholds and
  all traceability/test-strategy gates pass before alpha or beta promotion.

## 8. Definition of Done

A work package is done only when all applicable conditions hold:

1. Its behavior is linked to stable FR/NFR IDs and does not contradict the
   event contract, lifecycle, architecture, ADRs, or threat model.
2. The implementation uses typed ports and bounded, argument-safe processes;
   no direct cross-module storage access or undocumented fallback exists.
3. Unit/component tests cover success, failure, replay, cancellation, and
   boundary cases; disposable Git/provider tests cover every external effect.
4. Tests are deterministic, isolated, network-denied unless explicitly in the
   provider canary, and safe against user-project/GitHub mutation.
5. Security findings are redacted and actionable; no secret, prompt, source,
   or credential enters default diagnostics.
6. Crash/concurrency behavior is tested for any durable intent or shared
   repository state.
7. Required documentation, CLI/result/error behavior, migrations, upgrade
   notes, and operational runbooks are updated.
8. The package passes the relevant smoke/core/full/extended tier and its
   acceptance evidence is recorded with OS, version, and command results.
9. Review confirms no force-push, destructive cleanup, unconsented public
   operation, stale verification, or unrelated user change can pass.

## 9. Implementation slices, delivered evidence, and open gates

The code currently contains implementation slices across phases. Their
presence is not phase-exit evidence and does not waive any dependency or
sequencing gate.

Delivered where section 10 provides direct evidence:

- [x] Go module, CLI/CI skeleton, and cross-build smoke definition.
- [x] SQLite/state primitives, receipt and lifecycle transactions, and
      restrictive local-state permissions.
- [x] Canonical event/result validation, receipts/deduplication, digesting,
      and stable lifecycle/audit evidence.
- [x] Argument-safe Git/filesystem/process/provider ports and deterministic
      fakes.
- [x] Local public-preflight validation and canonical report digest.

Open gates and next priorities:

- [ ] Freeze Phase 0 terminology, IDs, schema, lifecycle, threat invariants,
      and the [test traceability matrix](test-strategy.md); resolve remaining
      document status/link consistency and record the approved compatibility
      window.
- [ ] Bridge durable session/repository observations into owned candidate
      derivation and the verified local-commit workflow.
- [ ] Wire provider intent plus `verify`/`sync`/`retry` CLI behavior.
- [ ] Run native hosted macOS and Windows coverage.
- [ ] Run an opt-in disposable GitHub canary with exact postconditions and
      allowlisted cleanup.
- [ ] Recover or replace the absent 177-case prototype suite and reach the
      >=609 release-test target.
- [ ] Complete all Phase 0 freeze, phase-exit, external-provider, native-OS,
      release, canary, and beta gates before claiming promotion.

## 10. Local implementation evidence (2026-09-01)

The first implementation slice is intentionally limited to contracts that can
be exercised without a user repository or network credentials:

- `internal/events`: bounded UTF-8 JSON envelope decoding, duplicate-key and
  trailing-input rejection, event-class/producer/type and scope validation,
  canonical SHA-256 payload digests, SQLite receipt transactions, replay and
  identity-conflict detection, causal pending records, lifecycle projection
  transactions, audit metadata, and restrictive state permissions.
- `internal/policy`: explicit policy merge semantics, local-only provider
  prohibition, and separate public-consent requirement.
- `internal/repository`: canonical repository/worktree discovery and keyed
  non-reversible identities, including nested working directories and linked
  worktree metadata checks.
- `internal/gitport`: argument-array execution with bounded output and exact
  SHA-to-`refs/heads/<ref>` push construction.
- `internal/historyscan`: bounded, read-only exact-candidate-SHA history
  scanning with policy/scanner-bound evidence and redacted findings.
- `internal/commit` and `internal/security`: Conventional Commit validation,
  trailer/content checks, secret/conflict findings, and redaction.
- `internal/app` and `cmd/autogit`: stable JSON hook results, optional adapter
  translation, keyed project resolution, lifecycle status projection, redacted
  diagnostics, and local policy operations with private/local defaults.
- `internal/state`, `internal/staging`, `internal/verification`,
  `internal/coordinator`, `internal/gittransaction`, `internal/provider`,
  `internal/adapters`, and `internal/install`: tested durable job/outbox/lease,
  ownership/index, bounded verifier, durable git intent/reconciliation,
  exact-provider, six-adapter contract matrix, and owned-config primitives;
  `staging` now excludes unchanged baseline paths, blocks baseline edits and
  deletions as ambiguous, and exposes a deep-copied immutable candidate
  snapshot directly compatible with `gittransaction`; `gittransaction`
  additionally separates real-Git candidate preparation from commit/ref
  mutation with exact-tree, unchanged-HEAD/index, and idempotent recovery
  tests.
- `internal/workflow`: a composable local workflow that accepts a captured,
  caller-owned snapshot and recorded tracking policy; it scans before durable
  Git intent, prepares an isolated candidate, runs a frozen trusted verifier
  set against its exact candidate/base/policy/guard evidence, and creates only
  the verified AutoGit-owned local commit ref. It has real-Git coverage for
  preserving the current branch and shared index, plus a secret-block path
  that proves no durable intent or ref is created.
- `internal/publication`: a pure local public-preflight package with no Git,
  provider, or network dependency; it validates consent, destination identity,
  candidate/history scans, verification evidence, file metadata, README/license
  readiness, and produces a canonical report digest.
- `internal/provider`: provider push binding that accepts only a validated
  remote alias whose resolved URL matches the canonical GitHub destination;
  `internal/provider.SystemRunner` (also exposed as `CommandRunner`) is the
  production bounded process runner for Git/`gh` argv, with direct execution,
  cancellation, controlled environment, and capped output. The separate
  `internal/verification.ExecRunner` provides the corresponding bounded
  process boundary for trusted verifier argv.

The following planned gates remain open and are not represented as completed:
baseline/ownership derivation from session state, full trusted verification
policy configuration wiring, complete adapter discovery/installation and
workflow orchestration in the CLI,
the complete CLI publication/provider workflow, provider intent wiring from the
CLI, the >=609 release-test target, and the opt-in disposable-provider canary.
The prototype shell test scripts described by the test strategy are not present
in this checkout, so their 177-case baseline has not been rerun. Native macOS
and Windows CI has not yet been observed; the workflow definition is evidence
of planned coverage only. The local test suite does not make claims about those
external or release-gate behaviors.

### 10.1 Portability evidence (local, 2026-09-01)

The workflow in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) now
defines the intended native matrix and cross-build smoke jobs. Fresh `go test
./...`, `go vet ./...`, `go build ./...`, and all three requested cross-build
commands pass locally. These are cross-build smoke checks, not native OS
execution evidence; native macOS and Windows CI has not been observed.
Provider credentials, network publication, and user-project fixtures are not
used.
