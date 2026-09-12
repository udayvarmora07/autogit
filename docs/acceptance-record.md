# Phase 0 contract acceptance record

Status: Accepted for the implementation baseline; release posture remains
NO-GO until the remaining safety, native-matrix, and release gates pass.

Accepted by: Uday Varmora
Accepted on: 2026-09-07

## Scope

This record accepts the normative implementation baseline and terminology used
by the following documents. It does not approve a private alpha, public beta,
live provider canary, or release artifact.

| Accepted area | Normative document | Evidence boundary |
| --- | --- | --- |
| Product requirements | [product requirements](product-requirements.md) | Must-level requirements and traceability checks |
| Lifecycle state and transitions | [lifecycle](lifecycle.md) | Lifecycle reducer and replay tests |
| Wire/event contract | [event contract](event-contract.md) | JSON schema, decode, adapter, receipt, and causal replay tests |
| Threat model and invariants | [threat model](threat-model.md) | Security, ownership, process, and provider-boundary tests |
| Compatibility window | [contract freeze](contract-freeze.md) and [compatibility manifest](compatibility-manifest.json) | Versioned event/result/state compatibility checks |
| Release terminology | [contract freeze](contract-freeze.md) and [release runbook](release-runbook.md) | Evidence/status language and explicit NO-GO gates |

The acceptance is limited to the stated baseline. A document or command must
not infer phase promotion from this record; promotion still requires the
phase-specific evidence listed in [todo.md](../todo.md).

## Phase 0 exit acceptance

The owner reviewed the Phase 0 exit criteria against implementation source
`1406352345886c116ba7750369fc3a14df960b63`. The exact-source hosted matrix
[`34682716776`](https://github.com/udayvarmora07/autogit/actions/runs/34682716776)
passed all 20 jobs: native Linux, macOS, and Windows checks; six native
artifact jobs; soak; fuzz; reproducible release binaries; security analysis;
dependency/workflow policy; and scenario/performance checks. The security
analysis job passed with no unresolved finding, and the local Phase 0 bundles
record the static-analysis closure and adversarial test coverage. The current
`main` descendant `2700db2` contains documentation-only changes after that
source snapshot; fresh `go test ./...`, `go vet ./...`, and `git diff --check`
also pass.

Decision: Phase 0 exit accepted for the bounded private-alpha implementation
scope. This approval does not accept the Phase 1 gate, AppContainer or other
unimplemented isolation capabilities, native adapter evidence, exact-tag
release evidence, signed provenance, public beta, or GA.

Accepted by: Uday Varmora (`@udayvarmora07`)

Accepted on: 2026-09-12

## Private-alpha release-owner exception

For this single-maintainer repository, the owner may serve as the named
release reviewer for the bounded private-alpha candidate. This is an explicit
owner approval and must not be described as independent review. It does not
waive the exact-tag, provider-canary, signed-provenance, reproducibility, or
consumer-verification requirements, and it does not apply to public beta or
GA. Public beta and GA still require independent security/release review.

Decision owner: Uday Varmora (`@udayvarmora07`)

Decision date: 2026-09-10

## License decision

The project license is Apache License 2.0. The repository `LICENSE` file and
the license statement in `README.md` agree on that decision.

Accepted by: Uday Varmora (`@udayvarmora07`)

Accepted on: 2026-09-10

## Governance and security-contact review

The owner reviewed the repository governance baseline for the private-alpha
scope. `SECURITY.md` selects private GitHub Security Advisories as the security
contact, `docs/support-policy.md` defines supported versions and response
handling, `.github/CODEOWNERS` names the maintainer, and the contribution,
conduct, and changelog documents are present and linked from the README.

Decision: accepted for the bounded private-alpha scope. This decision does not
approve public beta or GA and does not replace the exact-tag release evidence.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10

## P4-07 compatibility-window review

The owner reviewed P4-07 against the clean implementation evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The compatibility contract
validates seven advertised windows covering Go, Git, SQLite, GitHub REST, MCP,
and the event/result schema majors. Fresh `scripts/check-compatibility.sh`
verification passed; the machine-readable report returned `due=false` and
`expired=false` for every window, and no expiry issue body was generated. The
focused expiry/manifest tests and the release compatibility-contract test also
passed. Hosted compatibility review
[`34590952233`](https://github.com/udayvarmora07/autogit/actions/runs/34590952233)
is retained as historical evidence for the preceding `97ea27f` snapshot. The
current full hosted matrix
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
passed at the current snapshot, while the current compatibility result is
bound by the local manifest.

The post-snapshot history contains documentation and evidence-record updates
only; no source, workflow, or toolchain input changed after the reviewed
snapshot.

Decision: accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not approve public beta
or GA and does not waive future expiry review, exact-tag, or
release-provenance requirements.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-11

## P4-08 evidence-manifest review

The owner reviewed P4-08 against the clean implementation evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The retained machine manifest
records the exact evidence commit, clean collection state, ten suite results,
six artifact hashes, and ten controls with requirement, test, command, and
threat mappings. Fresh evidence generation and schema validation also passed;
the generated manifest retained `working_tree: "clean"`, all four executed
suites passed, and P4-08 exposed its three evidence tests and traceability
fields without recording local paths.

Acceptance remains pending because P4-08 requires the evidence to be bound to
an approved exact stable `vMAJOR.MINOR.PATCH` tag and its release artifacts.
The retained manifest correctly records `tag_verified: false`, and no approved
exact release tag or tag-bound artifact bundle currently exists. The
single-maintainer owner-review exception does not waive that release identity
requirement.

Decision: implementation evidence accepted as verified; release acceptance is
blocked on the approved release-version decision and regenerated tag-bound
evidence manifest. This does not approve private-alpha release artifacts,
public beta, or GA.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-11

## P4-06 scenario-evaluation review

The owner reviewed P4-06 against the clean implementation evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The scenario evaluator grades
final repository branch, index, AutoGit ref, consent, provider-effect, and
client-configuration state rather than relying on command narration. The
fresh built-artifact run produced a redacted trace with 20/20 scenarios
passing across all six registered clients, including install/uninstall
idempotency and observation-only behavior. Focused test
`TestScenarioEvaluationGradesFinalRepositoryState` also passed. Hosted matrix
run
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
passed the native scenario jobs and retained bounded traces for the exact
snapshot.

The post-snapshot history contains documentation and evidence-record updates
only; no source, workflow, or toolchain input changed after the reviewed
snapshot.

Decision: accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not approve public beta
or GA and does not waive exact-tag, native-release, or release-provenance
requirements.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-11

## P4-05 chaos and recovery review

The owner reviewed P4-05 against the clean implementation evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The retained coverage exercises
network stalls and typed timeouts, partial provider responses with redaction,
safe rate-limit metadata, replay without a duplicate provider effect,
permission-loss fail-closed behavior, forward-clock lease expiry, process and
repository transaction boundaries, output/write exhaustion limits, and
crash/recovery and lock-contention schedules. Hosted matrix run
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
passed the relevant quality and recovery jobs. Fresh verification passed the
targeted provider, secure-filesystem, coordinator, transaction, and recovery
tests, the integration tier, and the retained 1,000-schedule soak tier.

The post-snapshot history contains documentation and evidence-record updates
only; no source, workflow, or toolchain input changed after the reviewed
snapshot.

Decision: accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not approve public beta
or GA and does not waive exact-tag, provider-canary, or release-provenance
requirements.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-11

## P4-04 fuzz-floor review

The owner reviewed P4-04 against the clean evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The fuzz suite covers the
canonical event/JSON, adapter, provider identity/ref, Git push-argument,
policy, configuration-path, migration, state/status, and scanner boundaries;
each target retains an embedded seed corpus. The fresh local command
`AUTOGIT_FUZZ_TIME=40s bash scripts/test-suites.sh fuzz` passed all ten targets
at the required 100,000 executions per target, including the security target.
The hosted matrix run
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
also passed its fuzz job, and the full Go test suite passed.

The post-snapshot history contains documentation and evidence-record updates
only; no source, workflow, or toolchain input changed after the reviewed
snapshot.

Decision: accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not approve public beta
or GA and does not waive exact-tag or release-provenance requirements.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10

## P4-03 native-artifact review

The owner reviewed P4-03 against the clean evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The machine manifest records six
artifact hashes and the hosted run
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
passed all six native-artifact jobs: Linux amd64/arm64, macOS amd64/arm64,
and Windows amd64/arm64. The fresh local release suite rebuilt all six
targets, verified `SHA256SUMS`, and passed host artifact smoke on system Git
2.43.0. The full Go test suite also passed.

Acceptance remains pending because the release plan requires the native
artifacts to be bound to an approved exact stable `vMAJOR.MINOR.PATCH` tag and
to a tag-gated hosted release run. No local tag, GitHub release, or
`release.yml` run currently exists, and the manifest correctly records
`tag_verified: false`.

Decision: implementation evidence accepted as verified; release acceptance is
blocked on the approved release-version decision and its resulting
tag-bound hosted artifact evidence. The owner-review exception does not waive
that release identity requirement.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10

## P4-02 shell-safety review

The owner reviewed P4-02 against the clean evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. `scripts/check-shell.sh`
validates every repository shell script with `bash -n` and ShellCheck, and
the direct tests cover malformed suite dispatch, missing artifact rejection,
canary precondition failure, performance sample-count failure, transient
performance retry behavior, release output-directory safety, checksum
mismatch rejection, attestation identity binding, and stock macOS Bash
compatibility.

The canary cleanup trap is restricted to the generated allowlisted repository,
the artifact smoke path fails before creating state when its binary is absent,
and the performance and release scripts fail closed on incomplete evidence.
Hosted run
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
passed the security, policy, and presubmit jobs. Fresh current-worktree
verification passed with required ShellCheck, the focused shell tests, and
the full Go test suite; post-snapshot changes remain documentation-only.

Decision: accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not approve public beta
or GA and does not waive exact-tag or release-provenance requirements.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10

## P4-01 quality-tier review

The owner reviewed P4-01 against the clean evidence snapshot
`16c6325952b7937cdf8333b12cc7de0189b91f2b`. The manifest records separate
presubmit, core, race, integration, soak, fuzz, canary, and release commands;
all 10 recorded suite results are passed. Hosted run
[`34600488282`](https://github.com/udayvarmora07/autogit/actions/runs/34600488282)
passed all 20 jobs, including the bounded presubmit path, scheduled/manual
long-running matrices, native quality jobs, and the p95 performance gates.

The current exact-snapshot hosted run is recorded above; the post-snapshot
history after that evidence collection contains documentation and
evidence-record updates only. Fresh current-worktree checks also passed for
the presubmit and integration tiers and for `go test -count=1 ./...`.

Decision: accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not claim an exact release
tag or public beta/GA approval.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10

## Dependency and workflow policy review

The owner reviewed the dependency and workflow controls for the bounded
private-alpha scope. The review covered `docs/dependency-policy.md`,
`docs/dependency-licenses.json`, `.github/dependabot.yml`, all five files in
`.github/workflows/`, `scripts/check-dependencies.sh`, and the
[`gosec` suppression register](release-evidence/gosec-suppressions.md).

The reviewed baseline has a documented SPDX allowlist, direct license
inventory, `go mod verify`, weekly Dependabot proposals capped at five open
pull requests per ecosystem, high-severity dependency-review blocking,
CodeQL and OpenSSF Scorecard jobs, least-privilege permissions, and full-SHA
action pinning. Open Dependabot proposals remain subject to the policy job and
deliberate maintainer review; no repository workflow performs automatic
merges. The current action-update proposals are tracked in pull requests
[#2](https://github.com/udayvarmora07/autogit/pull/2),
[#3](https://github.com/udayvarmora07/autogit/pull/3),
[#4](https://github.com/udayvarmora07/autogit/pull/4), and
[#5](https://github.com/udayvarmora07/autogit/pull/5).

The policy exceptions are justified and bounded: the 28 `gosec` suppressions
are line-scoped false positives at documented compatibility or security
boundaries, each has linked tests, and the register defines review triggers.
The workflow security review found no exploitable external-attacker path in
the reviewed triggers, permissions, secrets, or action references.

Decision: accepted for the bounded private-alpha scope. This decision does
not approve public beta or GA and does not permit unreviewed dependency or
workflow updates.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-10
