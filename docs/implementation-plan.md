# AutoGit world-class implementation plan

Cross-chat continuation: read [new-chat-handoff.md](new-chat-handoff.md) for the
current exact-commit, evidence, count, and next-action snapshot. Do not use an
older evidence commit in this document as proof of current HEAD status.

Status: Active; release posture is NO-GO for private alpha
Last updated: 2026-09-11
Audited baseline: f9b692f261c22a5ca101074c644a42a33a96f9e0
Execution tracker: [todo.md](../todo.md)

Current execution slice (2026-09-07): the local implementation for P2-06
through P2-10 is present in `internal/provider/github_rest.go` with deterministic
contract tests and a retained evidence bundle. A private disposable canary
also passed through the typed REST provider using the existing `gh` keyring
token, with exact cleanup verification. This does not change the release
posture or waive the permission, native platform, and named-reviewer
requirements. The boundary now pins REST API version `2026-03-10`, exercises
durable no-duplicate reconciliation, requires explicit App-token
repository/permission scope, and runs the opt-in canary through the typed REST
provider; the exact version/Enterprise policy is in [provider support policy](provider-support.md).

The next local execution slice, P2-11 and P3-01 through P3-09, is implemented
in the pinned stdlib MCP boundary, CLI composition/presentation layers,
operation recovery UX, configuration provenance, local telemetry, performance
budgets, and operator documentation. Exact files and verification commands are
retained in [the P2-11 and Phase 3 evidence bundle](release-evidence/phase-2-p2-11-phase-3.md).
The Phase 2/3 release gates remain open until native artifact, provider,
support-owner, and signed-release evidence is separately executed.

The 2026-09-08/10 execution slice adds the named Phase 4 verification tiers,
artifact and final-state scenario evaluators, compatibility-window expiry
automation, machine-readable evidence generation, and the Phase 5 governance,
dependency, CodeQL, and Scorecard controls. The corresponding evidence is in
[the Phase 4/5 bundle](release-evidence/phase-4-quality.md). Artifact smoke now
checks the live system Git against the advertised minimum, and the scenario
evaluator covers all six registered clients with bounded native platform
traces. The latest hosted evidence snapshot `16c6325` passed the full 20-job
hosted matrix in run `34600488282`, including fuzz, soak, native artifact,
scenario, p95, reproducible-build, security, and dependency-policy jobs. The
machine manifest was regenerated at evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`, where it records a clean 10/10
local suite collection and six artifact hashes. The push-triggered core run
`34600109706` and security run `34600109672` also passed against that exact
SHA. No exact release tag exists yet. Live provider
execution, signing, and named review remain release evidence rather than
claims made by this slice.

The 2026-09-11 PR-011/PR-012 slice hardens maintenance publication and
interruption recovery: Unix directory synchronization follows atomic backup
and restore renames, stale private maintenance artifacts are recovered only
after a conservative age check, and subprocess crash tests cover backup and
migration boundaries. Restore tests include durable jobs, receipts, pending
events, and outbox rows. The verifier process-bounded capability now probes
the native supervisor before advertising it; Windows Job Objects remain
explicitly distinct from the still-unimplemented AppContainer boundary, and
stronger Windows/macOS tiers remain fail-closed.

The follow-up 2026-09-11 isolation/release slice adds a trusted Linux
bubblewrap launcher contract: only fixed root-owned, non-writable system
paths are accepted, nested user namespaces are disabled and asserted, and a
native regression test proves an inner `unshare` request is rejected. The
capability report records that lockout while continuing to distinguish the
Landlock ABI probe from an in-process Landlock ruleset. The native artifact
workflow now runs the raw-binary install lifecycle drill for all six claimed
OS/architecture targets; exact-source hosted run `34604368677` passed all 20
jobs and retained one redacted lifecycle record per target. The current implementation
evidence snapshot is `0d40d6af1ae38618edfa904f28fd7267e41e179e`; no exact
release tag or protected release approval exists yet.

## 1. Executive decision

AutoGit already has an unusually strong safety-oriented foundation: explicit
consent, immutable candidate construction, fail-closed ownership attribution,
durable intent records, exact-SHA publication, redacted state, deterministic
recovery tests, and native CI. It is not yet ready for alpha distribution.

The original baseline review identified nine technical gaps. The local
implementation work in PR-001 through PR-012 and the linked Phase 1–4 bundles
addresses the SQLite, filesystem, Git, lifecycle, deadline, adapter/provider,
static-analysis, test-tier, and governance implementation findings. The
current release-blocking gaps are:

1. Phase 0 and Phase 1 exit evidence is not accepted by named product/security
   owners; the required native hostile, crash, cancellation, privacy,
   ownership, and backup/restore matrices are not yet a release-approved
   record across every claimed platform.
2. The advertised verifier baseline remains process-bounded. The local P1-04
   capability report now distinguishes the achieved Linux
   bubblewrap namespace primitive, a detected-but-not-enforced Landlock ABI,
   Windows job-object controls without AppContainer, and the macOS
   process-group fallback; trusted verifier evidence binds those observations
   into its digest. Native tests for those implemented primitives now pass in
   exact-source hosted run `34604368677`; AppContainer-specific enforcement,
   an in-process Landlock ruleset, independent attestation, and named
   platform-owner acceptance remain incomplete.
3. Phase 2 still needs adapter-native install/upgrade/uninstall evidence and
   named review for the exact alpha-supported client/provider subset. The
   historical provider canary is retained, but the current dedicated-token
   canary path has not completed for the evidence snapshot.
4. P4-08 has implementation evidence but no exact release tag: the checked-in
   manifest deliberately has `tag_verified: false` and is snapshot-bound. The
   tag-gated workflow now generates a separate exact-tag manifest and carries
   it into the attested release bundle; that path remains unexecuted until an
   approved stable tag exists.
5. P5-03 through P5-05 now have a local implementation in
   `.github/workflows/release.yml` and `cmd/autogit-sbom` (including SPDX and
   CycloneDX SBOM generation); the workflow also
   contains a separately protected GitHub Release publication job for the
   locally actionable part of P5-06. The publication runner re-verifies the
   downloaded bundle's checksum, exact identity, and attestation predicates
   before it can create the release. `scripts/release-rollback-drill.sh`
   covers the offline technical part of P5-07, while `SECURITY.md` now records
   explicit severity-based containment and patched-release targets. Formal acceptance still
   requires an exact tagged hosted run, release-environment review, consumer
   verification of the resulting attestations, retained independent-runner
   comparison evidence, package-channel demand/native install evidence, and a
   named rollback/disclosure review. The tag-gated path now derives deterministic
   Homebrew and Scoop metadata from the exact six-binary checksum manifest and
   the publication runner regenerates and byte-compares both files before
   attaching them to the GitHub Release. This prepares the channel artifacts;
   it does not replace package-repository publication or native clean-machine
   checks. The raw-binary lifecycle drill is retained in
   `phase-5-install-drill.md` but does not replace those native package checks.
6. P5-01 and P5-02 still require explicit product/maintainer review, including
   the security contact/support decision, automated dependency-update review,
   and documented approval of any policy exceptions.

The correct strategy is to harden the trusted local core first, productize
integrations second, then earn alpha, beta, and GA through evidence. Feature
breadth must not outrun the safety contract.

## 2. What was reviewed

This plan replaces the previous chronological implementation log. Completed
history remains available in Git; this document describes the current system,
target architecture, remaining work, dependencies, and release gates.

The 2026-09-06 review is the historical baseline review. It covered:

- The complete repository structure, approximately 34,682 lines, all Go
  packages, CLI entry points, scripts, workflows, product/security/architecture
  documents, dependency graph, and release artifacts.
- Fresh local validation for that baseline: go test -count=1 ./... passed, go
  vet ./... passed, go build ./... passed, go mod verify passed, actionlint
  passed, shell syntax checks passed, and the performance gate passed.
- The full test suite took roughly four minutes locally because 1,000-schedule
  subprocess matrices run in the default test path.
- govulncheck found no reachable vulnerability in application code. It noted
  two module-level vulnerabilities that were not reached by the baseline code.
- staticcheck found unused code, overwritten/ignored error values, an ignored
  Errorf result in the Git transaction path, and style/API findings; these were
  addressed in the subsequent PR-008 implementation and verification bundle.
- gosec 2.29 found three high-confidence, high-severity G703 path-taint
  findings; the current configuration uses narrow, documented suppressions
  and the current security workflow passes.
- 25 research waves and 92 focused searches across current primary sources.
  About 50 unique official sources were inspected and 37 decision-relevant
  sources retained. Research stopped when new waves repeated established
  conclusions. The source set is intentionally deduplicated rather than padded
  to an arbitrary citation count.

The current evidence refresh is recorded in the Phase 4/5 bundle. Hosted
behavior, commercial feature availability, and fast-moving client hook
contracts must still be verified with pinned fixtures and live disposable
tests for each release candidate.

Key repository evidence is directly traceable:

| Finding | Evidence |
| --- | --- |
| Old SQLite plus WAL and multiple openers | [go.mod](../go.mod#L7-L10), [state opener and WAL migration](../internal/state/state.go#L172-L218), [event-store opener and schema](../internal/events/events.go#L534-L560) |
| Session baseline before receipt dedupe | [application ingress](../internal/app/app.go#L133-L163) |
| Weaker Git runner | [Git runner](../internal/gitport/gitport.go#L24-L41) and the init call in [main](../cmd/autogit/main.go#L770-L802) |
| Verifier timeout without an isolation boundary | [trusted verifier execution](../internal/verification/policy.go#L302-L361) |
| Missing root operation deadlines | Repeated context.Background calls in [CLI composition](../cmd/autogit/main.go) |
| Release output reuse and missing build identity | [release build](../scripts/release-build.sh#L51-L97) |
| Cross-builds without native artifact smoke and no tag release | [CI workflow](../.github/workflows/ci.yml#L64-L113) |

## 3. Product north star

AutoGit should be the safest local control plane between coding agents and
Git/provider side effects:

- Local-first: useful without a server, account, network, or telemetry.
- Consent-bound: every mutation is derived from explicit, durable, scoped
  consent; public visibility is a separate decision.
- Ownership-safe: no unrelated, ambiguous, or pre-existing user work is
  committed.
- Evidence-bound: scan, verification, message, commit, ref, and publication
  all name the same immutable candidate, base, policy, and destination.
- Crash-safe: every external effect is preceded by durable intent and can be
  reconciled without duplicating or broadening the effect.
- Hostile-input-safe: repositories, hook payloads, config, paths, Git config,
  verifiers, provider responses, and environment variables are untrusted.
- Explainable: users can see what AutoGit knows, why it blocked, and what exact
  reversible action comes next.
- Portable and honest: capabilities are reported per platform/client/version;
  unavailable isolation is never described as sandboxed.
- Supply-chain verifiable: every published binary has version identity,
  checksums, SBOM, signed provenance, and a documented source-to-binary path.
- Privacy-preserving: source, prompts, diffs, credentials, URLs, raw paths, and
  stable repository/session identifiers do not leave the machine by default.

## 4. Current-state scorecard

| Area | Existing strength | Material gap | Release posture |
| --- | --- | --- | --- |
| Product contract | Detailed requirements, lifecycle, threat model, ADRs, and traceability test | Documents remain proposed and acceptance has no owner/date | Block alpha |
| Git safety | Isolated index/tree, exact SHA/ref, controlled Git environment, hardened init/worktree discovery, and HEAD/index rechecks | Native hostile-repository and differential matrices still need Phase 1 exit evidence and named acceptance | Block alpha |
| Ownership | Source-free baseline evidence, replay deduplication, race checks, rename/delete handling, and fail-closed ambiguity | Native recovery/ownership matrix and release-owner acceptance remain open | Block alpha |
| Durability | Intent-before-effect, leases, restart reconciliation, randomized subprocess schedules, and supported backup/restore/integrity/retention APIs | Native backup/restore/retention runtime matrix and remaining Phase 1 recovery drills | Block alpha |
| Verification | Frozen executable/config digests, timeout/output bounds, process-group/job cleanup, explicit tier evidence, Linux resource ceilings | Filesystem/network sandbox tiers and native platform resource/isolation validation remain unavailable | Block public use |
| Security scanning | Candidate and bounded history checks; pinned offline interface, exact-blob scope, coverage/limit evidence, redacted fingerprints | Detection engine breadth and provider-side push protection remain separately scoped; native security-tool matrix remains | Block public use |
| Adapters | Versioned six-entry registry, sanitized versioned fixtures, canonical translation, bounded probes, and four schema-specific installers | Native all-OS/client-version installation matrix and upstream drift automation remain; OpenCode/CommandCode are intentionally observation-only | Block compatibility claim |
| GitHub provider | Exact destination/SHA/ref checks, typed versioned REST transport, durable reconciliation, and retained historical private canary | Current dedicated-token canary, App permission review, native artifact execution for the exact release snapshot, and named provider review remain | Block alpha |
| CLI and operations | Read-only status/plan/doctor/log commands, structured output, version/help contracts, operation recovery UX, and bounded local telemetry | Support-owner acceptance and native install/upgrade/rollback evidence remain | Block supportability |
| Tests and CI | Layered presubmit/core/race/integration/soak/fuzz/release suites, shell checks, scenario evaluation, native artifact smoke, and hosted performance/security evidence | Exact release-tag binding and release-owner acceptance remain; current provider canary is not green | Needs release evidence |
| Release and governance | Deterministic cross-build script, snapshot-bound evidence manifest, license/support files, dependency policy, Dependabot, CodeQL, and Scorecard | Tag-gated signed release, SBOM/provenance, independent reproducibility, package channels, and native install/rollback drills remain | Block any release |

## 5. Non-negotiable invariants

These remain true through every phase and cannot be waived by a later release
decision:

1. No Git or provider mutation without valid, scoped consent.
2. No public operation without a separate, visible destination and visibility
   confirmation.
3. No unconditional whole-worktree staging and no inferred ownership of
   ambiguous paths.
4. Candidate, guards, scans, verification, message, commit, and publication
   evidence must bind to the same immutable digests.
5. Adapters report observations; they do not authorize or perform Git/provider
   mutation.
6. Durable immutable intent precedes every externally visible effect.
7. Retry reconciles the same identity and effect; it never creates a broader
   replacement operation.
8. AutoGit does not force-push, delete user refs, delete arbitrary hosted
   repositories, or silently repair destructive state.
9. Untrusted input never reaches a shell interpreter. Process arguments,
   environment, output, duration, descendants, filesystem, and network access
   are bounded according to the declared capability tier.
10. Default state and diagnostics contain no source, prompt, diff, secret,
    credential, remote URL, raw path, or stable cross-repository tracking ID.
11. A read-only command does not create, migrate, repair, or contact a provider
    unless the user explicitly requests that separate action.
12. A release claim is backed by reproducible evidence from the exact tagged
    commit and published artifacts.

## 6. Target architecture

    Agent clients / CLI / optional MCP
                |
                v
    Versioned capability adapters
    - immutable input fixtures
    - runtime version/capability probes
    - observation only
                |
                v
    Bounded ingress and receipt gate
    - validate -> dedupe -> persist -> project
                |
                v
    Lifecycle and consent domain
                |
                v
    Repository observation and ownership
    - one hardened Git boundary
    - traversal-resistant filesystem root
                |
                v
    Immutable candidate
       |                    |
       v                    v
    Secret/history scan   Isolation-tiered verification
       |                    |
       +---------+----------+
                 v
    Durable state gateway and operation journal
    - one connection policy
    - one migration owner
    - backup / integrity / retention
                 |
          +------+------+
          v             v
    Local Git txn    Provider txn
    exact tree/ref   versioned REST / gh bootstrap
          |             |
          +------+------+
                 v
    Redacted result, explanation, and opt-in telemetry

The CLI remains a modular monolith. A daemon, hosted service, microservices,
and distributed queue are out of scope until measured workloads prove a local
process cannot meet reliability or latency requirements.

## 7. Architecture decisions to record

| ADR | Decision |
| --- | --- |
| ADR-008 | One state gateway owns SQLite open options, migrations, connection policy, integrity checks, backup, and retention. Events and jobs use repositories over that gateway. |
| ADR-009 | Trust-boundary filesystem access uses a canonical owned root plus traversal-resistant relative operations; path-string check-then-open is not sufficient. |
| ADR-010 | Verification evidence records an achieved isolation tier: none, process-bounded, filesystem-isolated, filesystem-and-network-isolated, or remote-hermetic. |
| ADR-011 | gh remains an optional local authentication/bootstrap adapter. A typed, version-pinned GitHub REST client is the production provider boundary; GitHub App installation tokens are preferred for automation. |
| ADR-012 | Client compatibility is an independently versioned capability registry backed by immutable fixtures and runtime probes, not hard-coded marketing names. |
| ADR-013 | MCP is an optional explicit control surface, read-only by default. MCP annotations and task state are untrusted and never replace AutoGit consent or durable truth. ACP remains an evaluated future adapter. |
| ADR-014 | Observability is local-first and opt-in for export, with an allowlisted versioned AutoGit schema and cardinality/privacy budgets. |
| ADR-015 | Releases target signed checksums, SBOM attestations, hosted build provenance, artifact verification, and independent reproducibility evidence. |

## 8. Delivery strategy

Priority definitions:

- P0: exploitable integrity risk, data-loss risk, or prerequisite to trustworthy
  alpha testing.
- P1: required for private alpha quality and safe operation.
- P2: required for public beta or a credible compatibility promise.
- P3: valuable after the beta contract is stable.

Effort is relative: S is a bounded change, M is a multi-package slice, L is a
cross-platform or architecture change, and XL is a program requiring staged
delivery. Estimates are planning aids, not calendar commitments.

### Phase 0 — release safety reset

Goal: eliminate known correctness and integrity hazards before any live canary
or alpha cohort.

| ID | Pri | Effort | Work | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| P0-01 | P0 | M | Upgrade modernc.org/sqlite and matching dependencies to a release containing SQLite 3.51.3 or later; prefer current stable | None | Exact embedded SQLite version asserted; full native migration, recovery, race, and WAL tests pass |
| P0-02 | P0 | L | Consolidate events and job state behind one DB opener, migration coordinator, pragma contract, and connection policy | P0-01 | One schema owner; concurrent old/new-schema and interrupted-migration tests; no package opens the shared DB independently |
| P0-03 | P0 | M | Harden state root, DB, WAL, SHM, identity-key, policy, and temp-file access against symlinks, traversal, replacement, wrong ownership, and permissive modes | None | os.Root or equivalent no-follow boundary; adversarial component/final symlink and swap tests pass on native OSes |
| P0-04 | P0 | M | Route init and linked-worktree discovery through the hardened bounded Git runner | None | Hostile global/system/repository Git configuration cannot execute hooks, helpers, filters, SSH, or prompts |
| P0-05 | P0 | M | Add root operation budgets, per-effect deadlines, cancellation, and process-tree termination | P0-04 | Hung Git/gh/verifier/database simulations return stable timeout errors and leave no descendants or partial unrecorded effect |
| P0-06 | P0 | S | Dedupe session.started before baseline capture or make capture plus receipt acceptance atomic | P0-02 | Replay after repository mutation is an exact no-op and returns the original receipt result |
| P0-07 | P0 | M | Make initialization consent transactional; make policy corruption visible and writes locked, no-follow, atomic, fsynced, and revision-aware | P0-03 | Failed init leaves no active consent; torn/corrupt/concurrent writes fail visibly and recover safely |
| P0-08 | P0 | M | Resolve every staticcheck finding and triage all gosec rules; replace broad exclusions with local, documented suppressions | P0-03, P0-04 | staticcheck and full gosec pass; each suppression has owner, rationale, linked test, and expiry/review trigger |
| P0-09 | P0 | S | Reconcile and accept requirements, lifecycle, event contract, threat model, compatibility policy, and release terminology | Findings above | Named approver and date; no document claims a phase the evidence has not earned |
| P0-10 | P0 | M | Fix release artifact directory reuse and add version/commit/date identity to binaries, version, and doctor | None | Reused output cannot include stale artifacts; exact binary identity is reported without local paths or nondeterminism |

Phase 0 exit:

- P0-01 through P0-10 complete.
- go test, race, vet, staticcheck, gosec, govulncheck, actionlint, shellcheck,
  build, and performance gates pass on the required native matrix.
- No unresolved critical/high correctness or security finding.
- No live provider mutation is needed to complete this phase.

### Phase 1 — trusted local core

Goal: make local commit production-grade even when the repository, verifier,
filesystem, and process environment are hostile.

| ID | Pri | Effort | Work | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| P1-01 | P1 | M | Assert WAL, synchronous, foreign keys, busy timeout, checkpoint, and connection settings on every open; define local-filesystem support and safe fallback | P0-01, P0-02 | Runtime pragma audit; network/unreliable filesystem behavior is detected and fail-closed or uses documented safe mode |
| P1-02 | P1 | L | Add SQLite online backup or VACUUM INTO, restore, integrity_check, foreign_key_check, migration rollback, and repair/export commands | P1-01 | Kill-during-backup/migration tests; restored state produces the same durable jobs and receipts |
| P1-03 | P1 | M | Implement audit/event retention, pruning, compaction, and privacy-budget enforcement | P0-02 | Bounded state over simulated long use; active recovery evidence is never pruned; raw sensitive fields remain absent |
| P1-04 | P1 | XL | Implement verifier isolation tiers and attest achieved capability | P0-05 | Linux Landlock plus namespace policy prototype; Windows AppContainer/job controls; explicit macOS fallback; filesystem/network/child/resource adversarial tests |
| P1-05 | P1 | M | Replace regex-only secret checks with a scanner interface over exact candidate blobs and reachable history | None | Pinned offline scanner, redacted findings, coverage/limit evidence, hostile-config tests; online validation remains separately consented |
| P1-06 | P1 | L | Differentially test candidate trees against Git for filters, attributes, LFS, sparse checkout/index, submodules, worktrees, unusual modes, Unicode, newline and option-like paths | P0-04 | Expected tree equivalence or explicit fail-closed policy for every matrix row |
| P1-07 | P1 | M | Add SHA-1 and SHA-256 repository fixtures and remove algorithm-length assumptions | P1-06 | Local lifecycle and transaction suites pass for both object formats; provider limitations are reported as capabilities |
| P1-08 | P1 | M | Close verifier/config executable TOCTOU windows with handle/identity binding where supported and explicit residual-risk reporting elsewhere | P0-03, P1-04 | Replacement between validation and execution cannot run an unapproved binary |
| P1-09 | P1 | S | Normalize policy semantics and remove or define the dead public tracking value | P0-09 | Property/table tests cover every valid policy state and merge |
| P1-10 | P1 | M | Split genuinely read-only commands from state creation/migration/repair paths | P0-02 | status, plan, config explain, and non-repair doctor produce zero filesystem/provider mutations |

Phase 1 exit:

- A consented local workflow preserves user branch, index, unrelated work, and
  refs across every success, failure, cancellation, crash, and replay matrix.
- Verification output truthfully names its isolation tier.
- Backup/restore and corruption drills succeed on Linux, macOS, and Windows.
- Security scan coverage and limitations are visible in machine and human
  output.

### Phase 2 — integrations and provider productization

Goal: make external compatibility explicit, testable, least-privileged, and
resilient to upstream change.

| ID | Pri | Effort | Work | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| P2-01 | P1 | L | Build a versioned adapter capability registry with immutable sanitized payload fixtures and unknown-field/event behavior | P0-09 | Fixture corpus is pinned to client versions; compatibility report is generated from tests |
| P2-02 | P1 | M | Add install-time and doctor-time client version/capability probes with degraded/unsupported states | P2-01 | Missing/changed hooks never imply completion or silently enable mutation |
| P2-03 | P1 | L | Refresh Codex, Claude Code, Gemini CLI, and Cursor codecs/installers from current official contracts | P2-01, P2-02 | Native install/upgrade/uninstall and real-payload contract tests; Cursor no longer falsely reported as hookless |
| P2-04 | P2 | M | Research and implement safe OpenCode and CommandCode integrations only where stable native contracts exist | P2-01 | Unsupported capabilities remain observation-only with a precise reason; no guessed lifecycle mapping |
| P2-05 | P1 | M | Make adapter configuration edits ownership-aware, schema-specific, atomic, backed up, and reversible | P0-03, P2-03 | Preserve unrelated user settings/comments where format permits; rollback and concurrent-edit tests pass |
| P2-06 | P1 | XL | Implement a typed GitHub REST transport with explicit API version, bounded/redacted responses, pagination, request IDs, rate-limit handling, and reconciliation | P0-05 | Contract tests plus disposable GitHub tests for create/read/push-related orchestration and error taxonomy |
| P2-07 | P1 | M | Bind provider identity explicitly and neutralize ambient GH_TOKEN/GITHUB_TOKEN/account selection hazards | P2-06 | Multiple account, expired/revoked token, wrong host/owner, and enterprise-host tests fail closed |
| P2-08 | P2 | L | Add GitHub App installation-token authentication with least repository and permission scope; retain gh as bootstrap/local-user mode | P2-06, P2-07 | One-hour refresh, revocation, narrowed repository/permission, and no-secret-at-rest tests |
| P2-09 | P2 | M | Add GitHub Enterprise Server capability/version negotiation and documented support policy | P2-06 | Version matrix and disposable enterprise fixture or contract emulator evidence |
| P2-10 | P2 | M | Optionally publish bounded verification evidence as a Check Run tied to head SHA and AutoGit evidence ID | P2-06 | Redacted bounded annotations; Check Run is a projection, never durable source of truth |
| P2-11 | P2 | L | Offer an optional MCP server for explicit read-only status, plan, explain, and logs; gate verify/publish as separately consented tools | P0-09, P1-10 | Protocol revision pinned; annotations treated as untrusted; MCP cannot bypass domain policy or durable state |

Phase 2 exit:

- Each supported client has a tested version range, fixture provenance,
  installer behavior, capability probe, downgrade story, and fail-safe unknown
  behavior.
- Provider effects use explicit identity, API version, least privilege,
  rate-limit handling, durable reconciliation, and exact postconditions.
- The disposable private GitHub canary has passed and cleanup evidence exists.

### Phase 3 — product UX and operations

Goal: make the safety model understandable and operable without reading source
or internal documents.

| ID | Pri | Effort | Work | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| P3-01 | P1 | L | Split cmd/autogit/main.go into command parsers, application services, presenters, and composition root without changing domain behavior | P0 complete | Golden/black-box CLI compatibility tests; package boundaries and dependency rules enforced |
| P3-02 | P1 | M | Add version, structured per-command help, examples, shell completions, man pages, and an end-to-end quickstart | P0-10, P3-01 | First-use test from clean machine to safe local commit; help snapshot tests |
| P3-03 | P1 | M | Define stdout/stderr, human/JSON, exit-code, error-cause, remediation, and stability contracts | P3-01 | No duplicate error codes; machine output is schema-tested; secrets and paths are redacted |
| P3-04 | P1 | M | Expand doctor into a capability report with versions, state health, isolation, adapters, provider mode, and safe suggested actions | P1, P2-02 | Offline by default; --json schema; no state creation unless --repair is explicit |
| P3-05 | P2 | M | Add operation status, explain, resume, cancel, runlog, and bounded undo for AutoGit-owned effects | P0-02 | Crash-restart UX tests; undo never rewrites/deletes user-owned history |
| P3-06 | P2 | M | Add atomic config migration, diff/preview, backup, rollback, and provenance of each owned config fragment | P2-05 | Upgrade/downgrade across every supported manifest version |
| P3-07 | P2 | L | Add local structured traces/logs/metrics and optional OpenTelemetry export with strict allowlists and cardinality budgets | P1-03 | Telemetry-off proves zero outbound traffic; no paths, URLs, prompts, source, session/repo IDs, or secrets in exported attributes/baggage |
| P3-08 | P2 | M | Establish performance budgets for hook, 1k/100k paths, DB growth, startup, verifier overhead, and provider operations | All prior | Native p50/p95/p99 trend artifacts and regression thresholds; no test-count proxy for quality |
| P3-09 | P2 | M | Rewrite README and operator docs for prerequisites, trust model, limitations, install, upgrade, uninstall, recovery, exit codes, and examples | P3-02..08 | Documentation tests and release-review checklist |

### Phase 4 — verification, evals, and continuous quality

Goal: turn safety claims into fast, reproducible, layered evidence.

| ID | Pri | Effort | Work | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| P4-01 | P1 | M | Separate fast presubmit, race, integration, 1,000-schedule soak, fuzz, canary, and release suites | None | Presubmit target under five minutes; long matrices scheduled/nightly and manually reproducible |
| P4-02 | P1 | M | Add shellcheck and direct tests for canary/performance/release scripts | None | Scripts pass syntax, static analysis, failure injection, and safe cleanup tests |
| P4-03 | P1 | L | Add native artifact smoke tests for every claimed OS/arch and system Git range | P0-10 | Built artifact, not go run, executes version/doctor/local workflow on required native hosts |
| P4-04 | P1 | L | Expand fuzz/property/differential testing at JSON, path, ref, Git status, policy, migration, and provider response boundaries | P1, P2 | Seed corpus retained; failures minimize and become deterministic regressions |
| P4-05 | P1 | L | Add crash/chaos tests for disk full, permission loss, clock change, lock contention, signal/kill, network stall, rate limit, and partial provider responses | P0-05, P1-02, P2-06 | No duplicated effect, false success, lost local commit, or unrecoverable state |
| P4-06 | P2 | L | Build scenario evals across clients and OSes that grade final Git/index/ref/provider state, not agent narration | P2 | Zero wrong-repo/ref/visibility effects in release corpus; diagnostic trace retained locally |
| P4-07 | P2 | M | Add compatibility-contract tests and automated expiry issues for client, Git, Go, SQLite, GitHub API, MCP, and schema windows | P2 | Every advertised version is exercised or removed before release |
| P4-08 | P2 | M | Publish machine-readable test evidence and requirement/risk/control traceability for the tagged commit | All prior | Release bundle maps requirement and threat IDs to exact test artifacts |

### Phase 5 — supply chain, governance, and distribution

Goal: let users verify what they install and know how the project is governed.

| ID | Pri | Effort | Work | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- | --- |
| P5-01 | P1 | S | Decide and add LICENSE; add SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CODEOWNERS, support policy, and CHANGELOG | Product owner | Repository and package metadata agree; security contact and supported versions are explicit |
| P5-02 | P1 | M | Add dependency update policy, Dependabot/Renovate equivalent, license policy, dependency review, CodeQL, and Scorecard | P5-01 | Least CI permissions; pinned actions; reviewed automated update flow; justified exceptions |
| P5-03 | P1 | L | Build a tag-gated release workflow in a clean environment | P0-10, P4-03 | Tag/commit/version match; dirty or reused output rejected; artifacts uploaded only after all gates |
| P5-04 | P1 | L | Generate SPDX and CycloneDX SBOMs, binary govulncheck results, signed checksums, and hosted SLSA provenance/attestations | P5-03 | Consumers can verify artifact digest, signature/identity, SBOM, provenance, and source commit |
| P5-05 | P1 | M | Verify reproducibility using independent build paths/runners and publish comparison evidence | P5-03 | Matching digests or documented normalized differences; same-run double build is not sole evidence |
| P5-06 | P2 | L | Add GitHub Releases and maintained package channels such as Homebrew and winget/Scoop after platform demand is validated | P5-03..05 | Install/upgrade/downgrade/uninstall tests and checksums on native clean machines |
| P5-07 | P2 | M | Define vulnerability disclosure, patch SLAs, keyless signing identity recovery, release rollback, and compromised-release drill | P5-01, P5-04 | Tabletop and technical rollback drill with redacted evidence |

### Phase 6 — alpha, beta, and GA

| Stage | Entry | Required evidence | Exit |
| --- | --- | --- | --- |
| Private alpha | Phases 0 and 1 complete; supported adapters/provider subset from Phase 2 complete; signed private artifacts | Bounded cohort, private repositories only, live private canary, support owner, recovery/backup drill, zero severity-1 safety incidents | Two consecutive release candidates meet all alpha SLOs and every issue has triage |
| Public beta | Alpha exit; Phases 2 through 5 complete for advertised scope | Explicit public preflight, public disposable canary, signed provenance, package install tests, incident/rollback exercise | Defined beta observation window with no wrong-repo/ref/visibility or unrecoverable-state event |
| GA | Beta exit; compatibility and support windows frozen | Independent security review, threat-model refresh, restore drill, release reproducibility, accessibility review for docs/CLI, support and deprecation policy | Named release owner signs the evidence manifest for the exact tag |

## 9. First implementation sequence

Keep the first changes small and independently reviewable:

1. PR-001: SQLite dependency upgrade, embedded-version assertion, and focused
   WAL/migration/native tests.
2. PR-002: one SQLite open/pragma/migration gateway; migrate events and state
   repositories without schema changes.
3. PR-003: traversal-resistant state root and sensitive-file API with
   adversarial symlink/replacement tests.
4. PR-004: hardened Git runner for init and linked-worktree discovery.
5. PR-005: command budgets, process groups/job objects, cancellation taxonomy,
   and descendant-cleanup tests.
6. PR-006: duplicate session.started idempotency and atomic/first-acceptance
   baseline semantics.
7. PR-007: transactional initialization consent and hardened policy storage.
8. PR-008: staticcheck cleanup and full gosec triage with narrow suppressions.
9. PR-009: binary build identity and clean release-output contract.
10. PR-010: accepted contract/status refresh and ADR-008 through ADR-010.
11. PR-011: DB backup/restore/integrity/retention operations.
12. PR-012: verifier isolation capability model and process-bounded baseline.

Do not combine the live canary with these hardening changes. Run it only after
the exact artifact and provider boundary intended for alpha has passed the
local and native gates.

## 10. Release gates

| Gate | Alpha | Public beta | GA |
| --- | --- | --- | --- |
| Accepted product/security contract | Required | Required | Required |
| Known critical/high findings | Zero unresolved | Zero unresolved | Zero unresolved |
| Staticcheck, gosec, govulncheck | Clean or narrowly documented false positive | Required | Required |
| Native OS/artifact matrix | Advertised alpha subset | All advertised | All advertised plus supported upgrade paths |
| SQLite migration/backup/restore/kill tests | Required | Required | Required |
| Local ownership/Git adversarial matrix | Required | Required | Required |
| Verification isolation | Process-bounded minimum, clearly reported | Required tier enforced by publication policy | Platform claim independently reviewed |
| Adapter fixture/runtime compatibility | Advertised subset | All advertised clients | Supported-window automation |
| Disposable provider canary | Private | Private and public | Per release candidate |
| Signed checksums/SBOM/provenance | Required | Required | Required |
| Reproducibility | Evidence collected | Published | Independently verified |
| Incident and rollback drill | Tabletop plus local | Live disposable | Periodic |
| Release approval | Named owner | Named owner | Evidence manifest signature |

## 11. Quality objectives and service indicators

Targets are measured over the release corpus and opt-in cohort; they are not
claims about all possible failures.

| Indicator | Target |
| --- | --- |
| Wrong repository, owner, ref, SHA, or visibility mutation | 0 |
| Unconsented Git/provider mutation | 0 |
| Unrelated user change included in a candidate | 0 |
| Duplicate external effect after replay/crash | 0 |
| Successful recovery of retained local commit after provider failure | 100% in release matrix |
| Backup restore and schema migration integrity | 100% in release matrix |
| Secret/path/prompt/source leakage in default state/logs/telemetry | 0 known cases; automated negative tests |
| No-candidate hook latency | p95 below 150 ms on supported native baseline |
| 100,000-path observation | p95 below 1 s on supported native baseline |
| Presubmit feedback | below 5 minutes at p95; long schedules moved to explicit tiers |
| Adapter advertised-version fixture pass rate | 100% |
| Provider transient recovery convergence | 100% in deterministic and chaos matrices |
| Accessibility of CLI output/docs | Color-independent, keyboard/screen-reader friendly text, plain fallback, stable JSON |

## 12. Risk register

| Risk | Probability | Impact | Treatment |
| --- | --- | --- | --- |
| SQLite concurrency corruption or partial migration | Medium until upgrade | Critical | P0-01/P0-02, native kill/concurrency tests, backups |
| Symlink/TOCTOU redirect of trusted state or executable | Medium | Critical | P0-03/P1-08, traversal-resistant handles and identity binding |
| Verifier escapes or leaves descendants | High in current design | High | P0-05/P1-04, capability tiers and publish policy |
| Agent hook drift creates false completion evidence | High | High | P2-01..04, runtime probes and fail-safe degradation |
| Ambient provider credential targets wrong identity | Medium | Critical | P2-06..08, explicit identity binding and App tokens |
| Secret scanner false negative | Medium | High | Exact candidate/history layers, mature offline scanner, provider push protection |
| Slow default suite reduces developer feedback and encourages bypass | High | Medium | P4-01 tiering and published evidence |
| Platform sandbox claims exceed actual primitive | Medium | High | Achieved capability attestation and native adversarial tests |
| Release artifact cannot be attributed or rolled back | High today | High | P0-10/P5, signed provenance and drills |
| Scope expansion delays a trustworthy v1 | Medium | High | Phase exits, advertised-subset release, deferred list below |

## 13. Explicitly deferred

- A hosted AutoGit control plane, multi-tenant service, or central source
  storage.
- Automatic public publication or public-by-default policy.
- Autonomous conflict resolution, force push, history rewrite, branch deletion,
  or broad hosted cleanup.
- Persisting prompts, transcripts, diffs, source, or line-level authorship.
- Inferring authorship cryptographically from agent-reported events.
- Building a daemon, microservices, Kafka, or distributed locks without
  measured need.
- Depending on MCP roots, sampling, protocol logging, or remote task state for
  security decisions.
- ACP as a required v1 integration while its remote/permission model evolves.
- A GUI before the CLI contracts, telemetry privacy, and safety model are
  stable.

## 14. Research-derived technical decisions

- SQLite documents a WAL-reset corruption bug affecting versions through
  3.51.2 in concurrent multi-connection/process use; fixed versions include
  3.51.3. AutoGit's WAL design makes the upgrade a release blocker.
  [SQLite WAL](https://www.sqlite.org/wal.html),
  [modernc SQLite changelog](https://gitlab.com/cznic/sqlite/-/blob/master/CHANGELOG.md)
- Live database copies are unsafe. Backup/restore must use SQLite's supported
  backup facilities and separate integrity and foreign-key checks.
  [SQLite backup API](https://sqlite.org/backup.html),
  [SQLite corruption guidance](https://www.sqlite.org/howtocorrupt.html),
  [SQLite PRAGMA reference](https://www.sqlite.org/pragma.html)
- Git plumbing supports isolated tree construction, but filters, attributes,
  path formats, and object algorithms must be explicit and tested.
  [git-write-tree](https://git-scm.com/docs/git-write-tree.html),
  [git-update-index](https://git-scm.com/docs/git-update-index/2.38.0),
  [git-hash-object](https://git-scm.com/docs/git-hash-object),
  [Git hash transition](https://git-scm.com/docs/hash-function-transition/2.23.0.html)
- Go's traversal-resistant root APIs are the preferred direction for
  untrusted relative paths where platform support permits.
  [Go os.Root](https://go.dev/blog/osroot)
- GitHub Apps provide narrower repository permissions and short-lived
  installation tokens. REST requests should pin the current API contract and
  obey rate-limit/retry guidance.
  [GitHub App differences](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/differences-between-github-apps-and-oauth-apps),
  [GitHub REST API versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions),
  [GitHub REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)
- GH_TOKEN and GITHUB_TOKEN can override stored gh credentials; provider
  identity must therefore be bound explicitly.
  [GitHub CLI environment](https://cli.github.com/manual/gh_help_environment)
- Client hooks are distinct, evolving contracts. Current official references
  include materially different lifecycle and decision semantics.
  [Claude Code hooks](https://code.claude.com/docs/en/hooks),
  [Gemini CLI hooks](https://github.com/google-gemini/gemini-cli/blob/main/docs/hooks/reference.md),
  [Cursor hooks](https://prod.cursor.com/docs/hooks),
  [Codex configuration](https://github.com/openai/codex/blob/main/docs/config.md)
- MCP security relies on user control and consent; tool annotations are not an
  authorization boundary, and several earlier primitives have been deprecated.
  [MCP tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools),
  [MCP security principles](https://modelcontextprotocol.io/specification/2025-03-26/index),
  [MCP deprecations](https://modelcontextprotocol.io/seps/2577-deprecate-roots-sampling-and-logging)
- Local sandbox capabilities differ significantly by OS and must be reported,
  not collapsed to a Boolean.
  [Linux Landlock](https://www.kernel.org/doc/html/latest/userspace-api/landlock.html),
  [bubblewrap](https://github.com/containers/bubblewrap),
  [Windows AppContainer](https://learn.microsoft.com/en-us/windows/win32/secauthz/appcontainer-isolation),
  [Apple App Sandbox](https://developer.apple.com/documentation/Security/app-sandbox)
- Release provenance, SBOMs, pinned CI dependencies, and least privileges are
  complementary controls.
  [SLSA build track](https://slsa.dev/spec/v1.2/build-track-basics),
  [GitHub artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations),
  [GitHub Actions secure use](https://docs.github.com/en/actions/reference/security/secure-use),
  [OpenSSF Scorecard](https://github.com/ossf/scorecard/blob/main/docs/checks.md)
- Secret scanning remains layered and cannot prove absence. Offline candidate
  and history scans should be mandatory; network validation must be separately
  consented.
  [Gitleaks](https://github.com/gitleaks/gitleaks),
  [TruffleHog](https://github.com/trufflesecurity/trufflehog/blob/main/README.md),
  [GitHub push protection](https://docs.github.com/en/code-security/concepts/secret-security/push-protection)
- Exported telemetry must be opt-in and tightly allowlisted because baggage
  propagates and high-cardinality dimensions can create privacy and reliability
  problems.
  [OpenTelemetry signals](https://opentelemetry.io/docs/concepts/signals/),
  [OpenTelemetry baggage](https://opentelemetry.io/docs/concepts/signals/baggage/),
  [OpenTelemetry metrics](https://opentelemetry.io/docs/concepts/signals/metrics/)
- Agent evals should grade final environment state. For AutoGit, deterministic
  Git/provider invariants are stronger evidence than model narration.
  [Anthropic agent eval guidance](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)

## 15. Comparable-product lessons

| Project | Useful lesson for AutoGit | Boundary to preserve |
| --- | --- | --- |
| [Aider](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/git.md) | Fast automatic commits, separate handling of existing dirty files, undo, and generated commit messages make Git automation approachable | AutoGit should differentiate on explicit consent, immutable verification, and never guessing ownership |
| [GitButler](https://github.com/gitbutlerapp/gitbutler/blob/master/README.md) | Hunk assignment and an operation timeline make complex Git state reviewable | Do not import implicit history rewriting or weaken AutoGit's exact-effect rules |
| [Entire](https://github.com/entireio/cli) | Per-worktree checkpoints and rewind improve recovery | Metadata branches and transcripts can expose source/context; keep AutoGit metadata minimal and local |
| [Git AI](https://github.com/git-ai-project/git-ai/blob/main/README.md) | Git Notes can support optional ecosystem attribution | Agent-reported attribution is evidence, not cryptographic proof, and must remain opt-in |
| [Git Town](https://github.com/git-town/git-town) | Continue, status, runlog, offline, and undo are excellent recovery UX patterns | Undo must remain bounded to AutoGit-owned effects |
| [OpenHands](https://github.com/OpenHands/docs/blob/main/openhands/usage/architecture/runtime.mdx) | Secure-by-default container execution and explicit warnings for unisolated local execution set an honest precedent | A container name alone is not a security proof; advertise achieved filesystem/network/process controls |

## 16. Plan maintenance

- This file is the architecture and release source of truth.
- todo.md is the executable checklist and must use the same IDs.
- Every completed item links to a PR/commit, exact commands, native platforms,
  and retained evidence. A checked box alone is not release evidence.
- Re-run dependency, provider API, client hook, MCP, Git, Go, and sandbox
  research before each beta/GA release candidate.
- Reassess priority immediately after any data-loss, wrong-target, privacy, or
  supply-chain incident.
- Update the compatibility manifest from tested facts; never edit it to match a
  desired marketing claim.
