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

## Phase 1 exit record (implementation verified; acceptance pending)

The Phase 1 implementation evidence is verified against exact source commit
`3888b570aa072317fbe132c284ed4e844092daa0` by manual workflow-dispatch run
[`35508065704`](https://github.com/udayvarmora07/autogit/actions/runs/35508065704).
The run completed successfully with 20/20 jobs, including native Linux,
macOS, and Windows core jobs, six native artifact lifecycle jobs, soak, fuzz,
performance, security, cross-build, and dependency/workflow policy checks.
The Phase 1 evidence bundles record the corresponding P1-01 through P1-10
implementation coverage and the explicit macOS process-group fallback.

The checked-out `HEAD` is `0745e0bc77a63af3cf21fa59522a387f6b533465`, a
documentation-only descendant of the tested commit. The intervening diff
contains only evidence, handoff, tracker, and release-runbook documentation;
no Go source, workflow, module, or toolchain input changed. Therefore the
matrix remains implementation evidence for this checkout, but it is not
described as a newly executed hosted run against `0745e0b`.

Decision: Phase 1 implementation status is **verified**; Phase 1 release
acceptance remains **pending**. This record does not close the Phase 1 gate.

Blockers:

- No named platform owner/reviewer acceptance for the claimed native Linux,
  macOS, and Windows Phase 1 matrix is recorded.
- The evidence reports the macOS process-group fallback and does not claim
  native macOS filesystem/network isolation; a named platform owner must
  explicitly disposition that limitation for the claimed Phase 1 scope.

Required next action: obtain and record the named platform-owner decision
against the exact evidence source and the documented macOS capability
limitation. Until then, the Phase 1 tracker checkbox stays open.

Recorded by: implementation agent

Recorded on: 2026-09-20

## Phase 2 exit record (implementation verified; acceptance pending)

The Phase 2 implementation evidence is reconciled against exact source
`3888b570aa072317fbe132c284ed4e844092daa0`. Manual matrix run
[`35508065704`](https://github.com/udayvarmora07/autogit/actions/runs/35508065704)
passed all 20 jobs. Its native Linux, macOS, and Windows core jobs run the
complete Go suite, including the fixture-backed adapter registry/translation
tests and schema-specific config install/uninstall tests in
`internal/adapters/p3_contract_matrix_test.go`,
`internal/adapters/registry_test.go`, and
`internal/install/client_install_registry_test.go`. The six native-artifact
jobs run AutoGit raw-binary smoke, scenario, and install-lifecycle drills.
These are native implementation checks; they do not establish that the real
Codex, Claude Code, Gemini CLI, or Cursor clients were installed, upgraded,
and uninstalled on every claimed native host.

The exact-source private provider canary
[`35508083681`](https://github.com/udayvarmora07/autogit/actions/runs/35508083681)
used the disposable-canary workflow and passed against the same source. It
created `udayvarmora07/autogit-v1-test-35508083681`, verified private `main`
at `8fd321c37412aa04f70df8667d4132286b40a150`, and confirmed the exact
allowlisted target was absent after cleanup. No public consent or GitHub App
permission approval is inferred from this user-token canary.

The checked-out `HEAD` is
`0745e0bc77a63af3cf21fa59522a387f6b533465`, a documentation-only descendant
of the tested source; no Go source, workflow, module, or toolchain input
changed between those commits. The evidence therefore remains applicable to
the implementation inputs in this checkout, but no hosted run against
`0745e0b` is claimed.

Decision: Phase 2 implementation status is **verified** for P2-01 through
P2-11; Phase 2 release acceptance remains **pending**. This record does not
close the Phase 2 gate.

Blockers:

- Retained native evidence covers fixture-backed adapter behavior and
  AutoGit artifact lifecycle, not actual native client install/upgrade/
  uninstall for every advertised installable adapter.
- No exact GitHub App installation permission review or approval is recorded;
  the canary evidence uses the dedicated disposable provider credential.
- No named provider/release reviewer has accepted the Phase 2 evidence, and
  the Phase 1 gate remains open.

Required next action: execute and retain the missing native adapter-client
install evidence for the claimed subset, then record the exact App permission
decision and named provider/release review against the same candidate source.

Recorded by: implementation agent

Recorded on: 2026-09-20

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

## Historical v0.1.1 P5-03/P5-04/P5-05 release review

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

## v0.1.2 P5-06 publication execution record

The exact tag `v0.1.2` resolves to source commit
`abcd3fb3b7ced0b34e33a34bfb335a800806f00b`. Release run
[`35506194835`](https://github.com/udayvarmora07/autogit/actions/runs/35506194835)
passed its quality, independent reproducibility, attestation, and consumer
verification jobs. The protected `release` environment approval produced
deployment `6552017112`, and the publication job `106068191231` created the
non-draft GitHub Release
[`v0.1.2`](https://github.com/udayvarmora07/autogit/releases/tag/v0.1.2).
The release contains six target binaries, checksums, SBOMs, vulnerability
reports, provenance evidence, and the generated Homebrew/Scoop metadata.

The published consumer bundle passed the exact repository/tag/commit verifier
and a direct `SHA256SUMS` check. A native Linux install drill also passed
initial install, upgrade, tampered-upgrade rejection, downgrade, rollback,
and uninstall against the previous binary built from immutable `v0.1.1`
source. This execution record does not claim clean-machine Homebrew/Scoop/
winget acceptance or the remaining named Phase 1/Phase 2/P5-07 release
reviews.

Recorded by: Uday Varmora (`@udayvarmora07`)

Recorded on: 2026-09-20

## v0.1.2 package-channel publication record

The demonstrated demand decision selects Homebrew for macOS and Scoop for
Windows. Winget is deferred because no generated winget manifest or separate
demand evidence exists. Exact metadata from the verified `v0.1.2` release at
source `abcd3fb3b7ced0b34e33a34bfb335a800806f00b` was published to:

- Homebrew tap repository `udayvarmora07/homebrew-autogit`, revision
  `310184dd16b4e5f5419ced6d4ab36493dcb2db79`, formula SHA-256
  `ca52c5323fb3d6417a8605291dccaeefa844500950442e8b0b5c2b84038f7d0e`.
- Scoop bucket repository `udayvarmora07/scoop-autogit`, revision
  `9d3a61ac04945f8cd4afe71dd4c141addee66567`, manifest SHA-256
  `ff3d043f68eb0a934c6eb27c91bb9ad458b9e537199eb9d4d6d8d1e04bdd8476`.

The repository files were compared byte-for-byte with the release assets.
This is implementation/publishing evidence only. P5-06 remains acceptance-
pending until native clean-machine package lifecycle evidence is recorded;
no package-channel acceptance is inferred from repository publication.

## v0.1.2 P5-07 rollback/tabletop status

The offline technical drill passed with schema
`autogit.release-rollback-drill/1`, publication state `stopped`, candidate
state `quarantined`, rollback target `previous-approved`, and all six checks
passed with `sensitive_data_recorded: false`. The redacted tabletop record is
prepared in [phase-5-rollback-drill.md](release-evidence/phase-5-rollback-drill.md)
for `v0.1.2` / source `abcd3fb3b7ced0b34e33a34bfb335a800806f00b` and protected
deployment `6552017112`.

P5-07 remains acceptance-pending until a named security/release reviewer
executes and records the tabletop decision. The prepared record is not an
approval and does not change the overall private-alpha `NO-GO` posture.

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
