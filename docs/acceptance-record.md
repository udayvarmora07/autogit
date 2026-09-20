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
scope. This approval does not accept the Phase 1 gate, the Windows AppContainer
implementation's named platform-owner acceptance, native macOS isolation,
native adapter evidence, exact-tag release evidence, signed provenance, public
beta, or GA. The Windows implementation evidence is recorded separately in
the Phase 1 P1-06–P1-10 bundle and is not a Phase 0 acceptance decision.

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

## Exact-tag P5-03/P5-04/P5-05 release review

The owner reviewed the exact-tag release execution for `v0.1.1` at source
`0743443224dd809e652ea69d5d6b275eef29a4ce` in run
[`35496909526`](https://github.com/udayvarmora07/autogit/actions/runs/35496909526).
The clean-room quality job passed exact identity, uncached/race tests, release
integration, source govulncheck, ShellCheck, dependency policy, and exact-tag
machine evidence. The independent Ubuntu and macOS builds and byte-for-byte
comparison passed. The attestation job passed SPDX and CycloneDX generation,
six binary govulncheck reports, signed checksums, SLSA provenance and both SBOM
attestations, followed by the consumer verifier bound to the exact repository,
workflow, tag, and source commit.

The initial `v0.1.0` attempt exposed a clean-checkout defect because downloaded
machine evidence was placed inside the Git worktree. Commit `0743443` moved
intermediate downloads under `$RUNNER_TEMP` and updated the regression test;
the corrected `v0.1.1` run passed the formerly failing identity check. The
later GitHub Release publication job is a separate P5-06 gate and remains open;
no release publication is claimed here.

Decision: P5-03, P5-04, and P5-05 exact-tag implementation and hosted evidence
accepted for the bounded private-alpha scope under the documented
single-maintainer owner-review exception. This does not approve public beta or
GA.

Reviewed by: Uday Varmora (`@udayvarmora07`)

Reviewed on: 2026-09-20

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

The owner reviewed P4-08 against exact tag `v0.1.1` at source
`0743443224dd809e652ea69d5d6b275eef29a4ce`. Release run
[`35496909526`](https://github.com/udayvarmora07/autogit/actions/runs/35496909526)
generated and retained `autogit.release-evidence.json` with
`tag_verified: true`, `working_tree: "clean"`, ten passed suites, six artifact
hashes, and thirteen requirement/threat/control records. The downloaded
manifest SHA-256 is
`479c818954f9198a43e7c81b709c7bf0316d06dd96e54b37433506080662310e`.

Decision: P4-08 exact-tag evidence accepted for the bounded private-alpha
scope under the documented single-maintainer owner-review exception. This does
not approve public beta or GA.

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

The owner reviewed P4-03 against exact tag `v0.1.1` at source
`0743443224dd809e652ea69d5d6b275eef29a4ce`. Exact-tag CI run
[`35497942544`](https://github.com/udayvarmora07/autogit/actions/runs/35497942544)
passed all 20 jobs, including native built-artifact smoke, system-Git floor,
scenario, and raw-binary lifecycle checks for Linux amd64/arm64, macOS
amd64/arm64, and Windows amd64/arm64. Native artifact job IDs were
`106044355136`, `106044355251`, `106044355261`, `106044355270`,
`106044355282`, and `106044355321`. The local release suite also rebuilt all
six targets and passed checksum and host smoke checks.

Decision: P4-03 exact-tag native artifact evidence accepted for the bounded
private-alpha scope under the documented single-maintainer owner-review
exception. This does not approve public beta or GA.

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
