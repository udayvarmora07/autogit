# AutoGit v1 implementation plan

Status: Execution in progress — local workflow and private publication slices implemented; phase exits not claimed
Last updated: 2026-09-05

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
| Phase 4 — private alpha | Native OS CI gate passed; private-alpha/release gates open | Dogfoodable local/private release on supported OSes |
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
- [x] Core-owned completion-candidate promotion requires an observed ingress
      completion claim, known queue state, and settled tool/prompt state; replay
      retries the deterministic promotion without creating duplicate facts.
- [x] Argument-safe Git/filesystem/process/provider ports and deterministic
      fakes.
- [x] Local public-preflight validation and canonical report digest.

Open gates and next priorities:

- [ ] Complete acceptance review of the Phase 0 terminology, IDs, schema,
      lifecycle, threat invariants, and test traceability matrix recorded in
      [`contract-freeze.md`](contract-freeze.md); the v1 compatibility boundary
      is now explicit, while approval and the future-major migration window
      remain policy gates.
- [x] Bridge durable session/repository observations into owned candidate
      derivation and the verified local-commit workflow for both explicit clean
      session completion and the trusted hook completion profile; implicit
      inference without an explicit trusted profile remains a separate policy
      gate.
- [x] Complete local public preflight/provider CLI publication, including
      readiness evidence and exact remote visibility postconditions; live
      canary evidence remains a separate release gate.
- [x] Observe hosted native Linux, macOS, and Windows coverage in
      [CI run 33972129362](https://github.com/udayvarmora07/autogit/actions/runs/33972129362)
      at `ad0e05d`; all native tests, builds, and p95 gates passed.
- [ ] Run an opt-in disposable GitHub canary with exact postconditions and
      allowlisted cleanup.
- [x] Recover and rerun the installed 177-case prototype regression floor.
- [x] Replace that compatibility floor with Go v1 coverage and reach the
      >=609 deterministic release-test target. CI enforces the floor by
      counting passing named Go test cases/subtests from `go test -json`.
- [ ] Complete all Phase 0 freeze, phase-exit, external-provider, release,
      canary, and beta gates before claiming promotion.

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
  worktree metadata checks; it now exposes a read-only baseline observation
  boundary for HEAD, index/status digests, changed-path rename pairs, and
  bounded in-memory file fingerprints. `internal/staging` can consume that
  observation directly, while `internal/state` persists only bounded
  source-free HMAC path/content/mode evidence and rejects changed-baseline
  replays.
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
  deletions as ambiguous, retains an explicitly observed file mode, and
  exposes a deep-copied immutable candidate snapshot directly compatible with
  `gittransaction`. It can capture explicit regular files from a canonical
  root, rejects symlinks at every path component, derives a plan from that
  capture, and binds content, mode, and deletion state into a private ownership
  digest. `workflow.RunPlan` rejects empty plans and accepts candidate bytes
  only from this ownership plan; its guard evidence binds that immutable plan
  digest. `gittransaction`
  additionally separates real-Git candidate preparation from commit/ref
  mutation with exact-tree, unchanged-HEAD/index, and idempotent recovery
  tests.
- `internal/workflow`: a composable local workflow that accepts a captured,
  caller-owned snapshot and recorded tracking policy; it scans before durable
  Git intent, prepares an isolated candidate, runs a frozen trusted verifier
  set against its exact candidate/base/policy/guard evidence, and creates only
  the verified AutoGit-owned local commit ref. It copies the input snapshot at
  entry so a later collaborator mutation cannot change the scanned/verified/
  committed bytes. It has real-Git coverage for preserving the current branch
  and shared index, plus a secret-block path that proves no durable intent or
  ref is created.
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

The following review/release gates remain open and are not represented as
completed: acceptance of the Phase 0 contract, the opt-in disposable-provider
canary, and alpha/beta promotion. Implicit
message/verifier inference remains intentionally unavailable without an
explicit trusted profile; the protected
`enable --auto-complete --verifiers FILE` profile, source-free durable
evidence, session start/complete coordinator, session-start hook wiring,
explicit `sync --complete --all-owned` resume path, deterministic task-intent
message composer, and trusted hook completion path are implemented. The
user-facing consent-gated repository-initialization command, explicit private
and evidence-gated public `publish` paths, deterministic lifecycle fact
emission, tested repository-creation/local-remote transaction package, adapter
discovery/install surface, and randomized durable-boundary recovery matrices
are implemented, but local evidence does not satisfy those external release
gates.
The prototype shell test scripts are not part of this repository, but the
installed reference checkout was rerun on 2026-09-05 and passed all 177
disposable scenarios. Native OS execution is now covered by the hosted CI
evidence recorded in section 10.19. The local test suite does not make claims
about the remaining provider or phase-promotion gates.

### 10.1 Portability evidence (local, 2026-09-01)

The workflow in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) now
defines the native matrix and cross-build smoke jobs. Fresh `go test ./...`,
`go vet ./...`, `go build ./...`, and all three requested cross-build commands
pass locally. Hosted native execution evidence is recorded in section 10.19;
the CI run also passed the deterministic test floor and native p95 gates.
Provider credentials, network publication, and user-project fixtures are not
used.

### 10.2 Local implementation evidence (2026-09-04)

The following additional slices are implemented and covered by deterministic
tests:

- `cmd/autogit publish` accepts only a completed AutoGit commit intent for the
  discovered repository and requires an explicit remote alias, owner,
  repository, branch, and visibility. It validates tracking/provider policy
  before resolving `gh` and records one immutable push intent keyed by the
  commit ID.
- Private publication uses the provider's exact remote URL binding and exact
  commit-SHA/ref postcondition through the durable coordinator. A transient
  provider failure remains `RETRY_WAIT`; a successful retry reuses the same
  SHA and destination identity.
- Remote policy can be enabled explicitly with `--provider github`,
  `--owner`, `--destination`, and visibility; the default `enable` behavior
  remains local-only and private. Public policy and command consent are
  separate requirements.
- Public `publish` returns a bounded, lowercase-JSON preflight report before
  provider executable discovery when the required local candidate/history,
  verification, README/license, and readiness evidence is not available. When
  all explicit evidence passes, it confirms hosted owner/name/visibility before
  allowing the exact-SHA push.
- `install --list` exposes all six adapter manifests and marks observation-only
  clients as non-installable until a stable client hook contract exists; it
  does not discover implicit configuration paths or mutate state.
- The CLI trusted executable resolver rejects a final-component symlink, and
  durable push intents retain a canonical remote-destination digest.

Fresh local evidence for this slice is `go test ./...`, `go test -race ./...`,
`go vet ./...`, and `go build ./...`. The private publication test uses fake
local `git`/`gh` executables and no network credentials. This evidence does
not satisfy the live GitHub canary, prototype-regression, or release-count
gates; hosted native evidence is recorded in section 10.19.

### 10.3 Lifecycle facts and repository transaction evidence (2026-09-04)

The CLI emits deterministic core-owned domain facts after explicit local sync
completion and after each durable publication attempt. The facts bind the
candidate, base, policy, verifier, guard, message, commit, remote, ref, and
operation error category digests needed by the lifecycle projection. Replaying
the same idempotency key is safe; legacy/manual commit intents without a
projected lifecycle scope remain authoritative in durable job state.

`autogit init` now provides the user-facing repository-initialization
boundary. It resolves an explicit canonical directory, rejects protected and
nested/bare Git targets, persists the selected local/private or remote/private/public
tracking policy before invoking Git, initializes an explicit branch, and merges
bounded ecosystem-derived ignore entries without staging or committing user
files, and creates a minimal README only when none exists. Its `--dry-run` path performs the same canonical preflight without
creating state or Git metadata. Remote creation remains a separate explicit command so hosted side
effects are independently reviewable and resumable.

`autogit doctor` reports the trusted executable availability for Git and
`gh`, the adapter/installable counts, and the availability of the SQLite and
durable lease stores. Provider authentication is explicitly reported as
`not_checked` because doctor does not contact GitHub or expose credentials.

For supported known queue states, the application now promotes an accepted
ingress `task.completed` claim to a deterministic core
`task.completion_candidate` fact only after the reducer confirms that no tool
or blocking prompt remains. An ingress claim cannot directly complete a task,
and a forged domain completion still requires the recorded candidate fact;
verified candidate derivation and explicit local sync remain the mutation
boundary.

`autogit remote create` and `internal/provider.RepositoryTransaction` provide
a durable, collision-safe
hosted-repository creation boundary. It persists intent before provider
creation, refuses mismatched existing aliases, requires exact hosted
owner/name/visibility confirmation, records a hosted-created intermediate
state before local mutation, verifies the attached URL, and never deletes or
implicitly rebinds a hosted repository after failure. A created but unattached
job can be resumed by the same immutable identity; collision and identity
failures remain visible and do not attach a same-name remote.
Remote job identity is bound to the keyed repository identity (state schema
v7), so a job cannot be replayed against another repository in the shared
application state directory.

### 10.4 Cross-process ownership recovery evidence (2026-09-05)

The session boundary now encodes a bounded, deterministic, source-free
baseline manifest before recording `session.started` or an explicit `sync`
baseline. Each recorded file uses a key-bound HMAC path identifier plus
presence, executable-bit mode, and content digest; raw filenames and source
bytes are not persisted. The schema-7 migration adds this evidence to existing
session rows and validates it on replay.

`sync --complete --all-owned` can resume a hook-captured session in a fresh
process. It re-observes `HEAD`, the shared index, and current status, maps
current paths to the manifest with the repository identity key, excludes paths
unchanged from the baseline, blocks edits/deletions of pre-existing dirty
paths, and owns only newly changed paths. For clean tracked paths, it also
consults the immutable baseline tree so deletions and renames become explicit
delete/add entries rather than silently losing the deletion. The explicit
`--path` mode remains available for a narrower caller-selected candidate.
Tests cover clean and dirty cross-process resumes, pre-existing-work
exclusion, changed-baseline blocking, clean tracked rename/delete handling,
key binding, malformed evidence, and absence of raw path/source leakage.

`doctor` is read-only even before initialization: it reports unavailable
state/lease stores without creating the state directory, database, or identity
key. Duplicate completion ingress retries the deterministic core candidate
promotion, while the core still requires an ingress completion claim and
settled tool/prompt/queue state.

### 10.5 Read-only plan evidence (2026-09-05)

`autogit plan --repo DIR` now performs a bounded repository observation and
returns an actionable JSON summary containing `HEAD`, shared-index/status/path
digests, changed-path count, and tracking/local/provider/public-consent checks.
`autogit status --repo DIR` exposes the same repository summary alongside
lifecycle state. Both commands use the read-only repository runner; real
repository tests snapshot `HEAD` and the shared index before and after the
operations. Neither command stages files, creates commit intents, moves refs,
or contacts a provider, and `plan` does not initialize AutoGit state.
`config explain` likewise validates optional verifier configuration without
creating AutoGit state.

### 10.6 Read-only verification recovery (2026-09-05)

`autogit verify --all-owned` now reconstructs a hook-captured session from its
source-free durable baseline manifest, observes current ownership, and runs
the trusted verifier set without requiring raw paths from the original
process. It is intentionally read-only: verification does not create a
commit intent, move an AutoGit ref, or alter the shared index. Explicit
`--path` verification remains available for callers that want a narrower
candidate scope.

### 10.7 Consistent filesystem baseline capture (2026-09-05)

Baseline capture now re-observes `HEAD`, the Git index identity/content, and
porcelain status after reading the selected files. Any repository change
during that window fails closed instead of recording a mixed-time baseline.
The capture boundary also retains the existing race-substitution checks and
supports linked worktrees, Unicode paths, rename/delete status records, and
Git-ignore/control-path validation. Real tests cover a concurrent status
change, replacement during a read, linked-worktree index resolution, and a
Unicode candidate path.

### 10.8 Durable intent fault evidence (2026-09-05)

Coordinator tests now inject failures at the initial commit and push intent
write boundaries and assert that no Git or provider effect is invoked. They
also inject commit-result persistence failure after the Git effect and verify
that restart-style evidence reconciliation records the result without
repeating the commit. Provider transaction tests inject initial remote-intent
persistence failure and a post-hosted-create result persistence failure; retry
confirms the exact hosted identity before local attachment and does not
recreate the repository. This is deterministic boundary coverage; the
required 1,000 randomized crash/concurrency schedules and every external
release gate remain open. Real SQLite lease tests run concurrent identical
commit and hosted-create requests and verify that only one external effect
occurs. Lease reacquisition now fails for every active owner, and release is
serialized with acquisition, preventing same-process overlap and stale-owner
release races. Commit processing rechecks the durable intent after waiting for
the lease, so a contended retry observes a completed job instead of issuing a
second Git effect.

The local Git transaction tests also inject failure at commit-intent
persistence and assert that `commit-tree` and AutoGit refs remain untouched.
They inject commit-result persistence failure after ref creation and verify a
retry recovers the existing ref without creating a second commit. Lifecycle
completion now validates that the loader handoff matches the ingress session,
repository, client, and required ephemeral trusted root before running the
workflow.

Independent state-store handles now exercise concurrent commit and hosted-create
requests as process-boundary tests, including reopening SQLite after a lost
commit, push, or hosted-create result. Receipt/projection and durable state
transactions use SQLite immediate writer locking with a bounded busy timeout;
this prevents deferred read-to-write upgrades from returning `SQLITE_BUSY`
while preserving transactional rollback and idempotent replay. Concurrent
duplicate lifecycle completion ingress is verified to converge on one AutoGit
ref and one durable task completion fact. A local remote-attachment response
loss is also retried from the exact durable hosted identity without recreating
or reattaching the remote twice.

The four legacy compatibility suites were rerun from the installed reference
checkout on 2026-09-05 and passed all 177 disposable scenarios (16, 53, 105,
and 3). They remain regression-floor evidence only; they exercise the Bash
hook and do not replace the Go v1 suite or its enforced `>=609` floor.

### 10.11 Deterministic commit fault schedule evidence (2026-09-05)

`internal/coordinator` now runs seeded 1,000-schedule commit and 1,000-schedule
push matrices plus 1,000 concurrent multi-store commit schedules. They cover
clean operation, durable intent/result-write failure, transient retry, lease
serialization, and idempotent recovery. Each schedule retries or reconciles
the same immutable request and asserts exactly one Git/provider effect. This
strengthens deterministic intent recovery evidence but does not satisfy the
remaining randomized concurrent process schedules, canary, or
phase-promotion gates.

### 10.12 Subprocess recovery evidence (2026-09-05)

`internal/coordinator/process_recovery_test.go` now runs real child-process
crash schedules after durable commit and push intent, after the Git/provider
effect, and after result persistence, then reopens SQLite and proves one exact
recovered effect. It also runs two independent child processes through the
same durable writer lease and proves one commit effect. This is stronger
process-boundary evidence for commit and push, but it does not close the
required 1,000 randomized schedules across every durable intent boundary.

`internal/gittransaction/process_recovery_test.go` adds real child-process
crash coverage after the local Git ref update and after commit-result
persistence, plus the pre-effect intent case, and proves restart recovery or
fail-closed reconciliation without creating a second commit object.
`internal/provider/process_recovery_test.go` does the same for hosted
repository creation and local attachment, proving that a lost hosted-create
or attach result is recovered from the exact durable identity. These tests
strengthen the process-boundary evidence, but do not close the required 1,000
randomized schedules across every durable intent boundary.

### 10.13 Fail-closed hosted intent reads (2026-09-05)

The hosted-repository transaction now propagates a durable intent read error
after the initial intent write instead of treating an unavailable record as an
empty request. A regression test proves that no hosted create call occurs when
that read fails, preventing a transient state-store failure from issuing a
duplicate provider operation.

It also rejects `REMOTE_CREATED` and `REMOTE_ATTACHED` records that lack the
exact hosted identity required for recovery. This prevents an incomplete
record from being interpreted as permission to create the destination again.

The CLI publication and retry paths now propagate push-job read failures
before emitting lifecycle facts. A closed or unavailable state store therefore
returns an explicit state error instead of silently reporting a publication
without its durable projection attempt.

### 10.14 Seeded randomized subprocess schedules (2026-09-05)

The release test suite now runs three reproducible 1,000-schedule subprocess
matrices: coordinator commit/push intent/effect/result boundaries, local Git
transaction intent/ref/result boundaries, and hosted repository intent/create/
created/attached boundaries. Each matrix asserts one effect, exact durable
completion, and coverage of every named boundary. This closes the randomized
process evidence for those side-effect protocols; the remaining persistence
boundaries are recorded below.

### 10.15 Randomized persistence-boundary schedules (2026-09-05)

The release suite now also runs reproducible 1,000-schedule subprocess
matrices for event receipt acceptance, session-baseline persistence, and
candidate/verification persistence. Each matrix covers pre-write, post-write,
normal, and concurrent process schedules, then reopens the durable store and
asserts one exact receipt or immutable evidence record. Candidate and
verification records now have typed restart reads, digest/state validation, and
same-ID immutable identity-conflict rejection.

The state opener establishes a new SQLite file with mode `0600` before
migration and retries bounded `SQLITE_BUSY` migration failures. This closes the
randomized local crash/concurrency evidence for every implemented durable
intent boundary: event receipt, baseline, candidate/verification, local Git
transaction, hosted create/attach, and coordinator commit/push. Native OS
execution, the disposable provider canary, and alpha/beta promotion remain
external release gates. The non-race release stream runs all 1,000 schedules;
race-enabled CI samples 50 schedules per newly added persistence matrix so
the instrumented subprocess tests stay within Go's per-package timeout.

### 10.16 Local performance and traceability evidence (2026-09-05)

The repository now contains 15 Go benchmarks covering the no-candidate core
hook, canonical event build
and decode, adapter digesting/manifests, repository path digests at 1,000 and
100,000 paths, actual 100,000-path baseline capture, durable baseline encoding,
commit messages, policy merge, security scanning, publication preflight, and
lifecycle reduction. The
benchmark suite runs with `go test -run '^$' -bench '^Benchmark' ./...`.
This satisfies the local benchmark-suite artifact requirement. Hosted p95
latency evidence is recorded in section 10.19; performance and phase
promotion remain release gates.
`scripts/performance-gate.sh` runs 20 samples and enforces the documented
150-ms no-candidate-hook and 1-second 100,000-path baseline p95 limits. The
native CI matrix now emits five benchmark samples and runs those p95 gates per
supported runner; the hosted evidence in run 33972129362 passed them on Linux,
macOS, and Windows. This closes the native-OS gate only; it does not promote a
release phase.

`TestMustLevelRequirementsHaveTraceabilityRows` parses the product
requirements and fails when a functional or non-functional requirement is
missing from the test-strategy traceability matrix or appears more than once.
It validates matrix maintenance locally without treating planned external
canary or phase-promotion evidence as complete.

### 10.17 Disposable provider-canary harness (2026-09-05)

The opt-in `github_canary` provider test and
[`scripts/github-canary.sh`](../scripts/github-canary.sh) now exercise a real
GitHub repository only when manually dispatched with a dedicated owner and the
`AUTOGIT_CANARY_TOKEN` secret. The run creates the exact
`autogit-v1-test-<run-id>` identity, verifies
owner, full name, visibility, branch, and commit SHA, and deletes only that
validated repository in an exit trap. Public visibility additionally requires
the explicit `PUBLIC` dispatch confirmation. The workflow is
[`github-canary.yml`](../.github/workflows/github-canary.yml); live canary
execution and cleanup evidence remain release gates.

### 10.18 Release support and rollback artifact (2026-09-05)

[`release-runbook.md`](release-runbook.md) records the private-alpha/public-beta
evidence checklist, private-first rollout, incident metadata, adapter rollback,
durable-intent reconciliation, compatibility migration, and disposable-resource
cleanup procedure. It intentionally leaves approval, the live canary, and
phase-promotion decisions with the release owner.

### 10.19 Hosted native CI evidence (2026-09-05)

[CI run 33972129362](https://github.com/udayvarmora07/autogit/actions/runs/33972129362)
completed successfully for commit `ad0e05d79c6eda3b602ca5f98e55841683e1b3e6`.
All seven jobs passed: native Ubuntu, macOS, and Windows; Linux arm64,
Darwin arm64, and Windows amd64 cross-builds; and security analysis. Each
native runner passed formatting, tests, the `>=609` deterministic test floor,
build, benchmark sampling, and the p95 performance gates; Linux and macOS also
passed race tests. This closes the native-OS gate. It does not close Phase 0
acceptance, the disposable provider canary, or private-alpha/public-beta
promotion.

### 10.20 Gate audit (2026-09-05)

- **Phase 0 acceptance — open.** The contract-freeze record says
  `product acceptance review pending`; the product requirements, threat model,
  event contract, lifecycle, architecture, and test strategy still carry
  draft/proposed acceptance status. The requirements-to-test traceability test
  passes, and the frozen terminology/invariants are recorded, but no product
  approver and acceptance date are recorded. This is evidence for review, not
  a Phase 0 exit.
- **Disposable GitHub canary — open.** The tagged harness and allowlisted
  cleanup path are implemented, and the local tagged token-boundary test
  passes. The shell entrypoint requires `AUTOGIT_CANARY_TOKEN` and overwrites
  `GH_TOKEN` from that dedicated value. No live run or cleanup artifact exists;
  no canary was attempted because the dedicated token is not present in this
  environment.
- **Private alpha — open.** The native OS CI gate, deterministic floor,
  security analysis, cross-builds, p95 gates, local regression floor, and
  documented local recovery evidence are present. The bounded private cohort,
  release-owner review, and remaining acceptance/provider evidence are not
  recorded, so alpha is not promoted.
- **Public beta — open.** Public preflight and the release runbook exist, but
  beta depends on the unresolved Phase 0 and private-alpha decisions and the
  live canary/public release evidence. No beta promotion is claimed.
