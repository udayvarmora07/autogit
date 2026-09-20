# Phase 4/5 acceptance matrix

Snapshot: `0d40d6af1ae38618edfa904f28fd7267e41e179e`
Local manifest: [phase-4-quality.json](phase-4-quality.json)
Hosted lifecycle matrix: [run 34604368677](https://github.com/udayvarmora07/autogit/actions/runs/34604368677)
Hosted result: 20/20 jobs passed against the exact source snapshot
`0d40d6af1ae38618edfa904f28fd7267e41e179e`, including native
Linux/macOS/Windows tests, six native artifact targets with raw-binary
install drills, three soak targets, fuzz, scenario evaluation, performance
gates, reproducible builds, security, and dependency policy. The earlier
workflow-introduction run `34603093643` also passed all 20 jobs and retained
the same six lifecycle records.

Current-tree validation: manual workflow-dispatch run
[34961479624](https://github.com/udayvarmora07/autogit/actions/runs/34961479624)
passed 20/20 jobs against `a51eb9718bac0e3e514e196fa4e1156a47865ade`.
The six native artifact jobs were `104355836617` (linux/arm64),
`104355836642` (linux/amd64), `104355836704` (darwin/arm64),
`104355836707` (windows/amd64), `104355836691` (windows/arm64), and
`104355836756` (darwin/amd64); each passed built-artifact smoke and the raw
binary install lifecycle drill. This strengthens current-tree implementation
evidence but does not replace the exact stable-tag release gate.

This matrix separates implementation evidence from the tracker’s promotion
rule. A row stays unchecked when the evidence is present but the plan still
requires a release owner, provider authorization, exact tag, or named review.

For the bounded private-alpha candidate, the documented single-maintainer
exception permits named release-owner approval in place of independent review.
This is an owner decision, not independent evidence, and it does not apply to
public beta or GA.

| Package | Evidence at the exact snapshot | Acceptance status |
| --- | --- | --- |
| P4-01 | Manifest records the presubmit, core, race, integration, soak, fuzz, canary, and release tiers; the hosted matrix exercises the bounded native quality path. | Accepted for the bounded private-alpha scope by the named release owner against the exact evidence snapshot; public beta/GA review remains separate. |
| P4-02 | Shell syntax/ShellCheck and injected failure-safety tests are recorded in the manifest and passed in hosted security/policy checks. | Accepted for the bounded private-alpha scope by the named release owner against the exact evidence snapshot; public beta/GA review remains separate. |
| P4-03 | Six native artifact jobs passed in the exact-source matrix and again in current-tree run `34961479624`, with system-Git floor, built-artifact smoke checks, and six native raw-binary lifecycle drills. | Native implementation evidence complete; release acceptance remains open pending an approved exact stable tag and tag-gated hosted artifact evidence. |
| P4-04 | The hosted fuzz job passed the 100,000-input floor for each target; the local manifest records the fuzz suite. | Accepted for the bounded private-alpha scope by the named release owner against the exact evidence snapshot; public beta/GA review remains separate. |
| P4-05 | Chaos, recovery, permission-loss, clock, lock, signal, network-stall, and partial-response tests are covered by the implementation and quality suites. | Accepted for the bounded private-alpha scope by the named release owner against the exact evidence snapshot; public beta/GA review remains separate. |
| P4-06 | Native scenario jobs passed and retained bounded traces for the six registered clients. | Accepted for the bounded private-alpha scope by the named release owner against the exact evidence snapshot; public beta/GA review remains separate. |
| P4-07 | Compatibility checks and expiry automation are recorded in the manifest; the compatibility contract has no due or expired window. | Accepted for the bounded private-alpha scope by the named release owner against the exact evidence snapshot; public beta/GA review remains separate. |
| P4-08 | The manifest binds requirements, threats, controls, suites, commit, and artifact hashes. | Implementation evidence complete; release acceptance remains open because `tag_verified` is false pending an approved exact stable tag and tag-bound artifact bundle. |
| P5-01 | LICENSE, SECURITY.md, CONTRIBUTING.md, CODE_OF_CONDUCT.md, CODEOWNERS, support policy, and CHANGELOG.md are present. | Accepted for the bounded private-alpha scope by the named release owner; public beta/GA review remains separate. |
| P5-02 | Dependabot, dependency review, CodeQL, Scorecard, pinned-action enforcement, module verification, and license policy are present and passed. | Accepted for the bounded private-alpha scope by the named release owner; automated-update review and policy exceptions are recorded. Public beta/GA review remains separate. |

The following evidence is intentionally not claimed: a live provider canary
with the dedicated token, a release-environment approval, keyless hosted
attestations, independent release verification, or a named security/release
review. The current dedicated-token canary evidence is a separate prior
`36c045f` run (`34489797141`) and is not silently promoted to this snapshot.

## Exact-tag execution addendum — 2026-09-20

The exact stable tag `v0.1.1` resolves to source commit
`0743443224dd809e652ea69d5d6b275eef29a4ce`. Native artifact and full quality
validation passed in [CI run 35497942544](https://github.com/udayvarmora07/autogit/actions/runs/35497942544),
which completed all 20 jobs, including the six native artifact targets and
their raw-binary lifecycle drills. The tag-gated release run
[35496909526](https://github.com/udayvarmora07/autogit/actions/runs/35496909526)
passed its quality, independent reproducibility, artifact attestation, and
consumer-verification jobs. Its retained exact-tag manifest reports
`tag_verified: true`, a clean worktree, ten passed suites, six artifact hashes,
and thirteen controls; manifest SHA-256:
`479c818954f9198a43e7c81b709c7bf0316d06dd96e54b37433506080662310e`.

| Package | Exact-tag evidence | Acceptance status |
| --- | --- | --- |
| P4-03 | Six native artifact jobs passed in CI run `35497942544` for `v0.1.1`; local release suite also passed all six target builds and host smoke. | Accepted for bounded private-alpha scope under the owner-review exception. |
| P4-08 | The release quality job generated the retained `tag_verified: true` manifest for the exact tag and commit. | Accepted for bounded private-alpha scope under the owner-review exception. |
| P5-03 | Exact-tag quality and attestation jobs passed in release run `35496909526`; the clean-checkout download defect was fixed in `0743443`. | Accepted for bounded private-alpha scope under the owner-review exception. |
| P5-04 | The attestation job passed SBOM generation, six binary govulncheck scans, checksum signing, SLSA provenance, SPDX/CycloneDX attestations, and consumer verification. | Accepted for bounded private-alpha scope under the owner-review exception. |
| P5-05 | Ubuntu/macOS independent builds and byte-for-byte comparison passed; retained comparison reports `status=byte-identical`. | Accepted for bounded private-alpha scope under the owner-review exception. |

The final publication job failed during the separate P5-06 package-metadata
revalidation, so no GitHub Release is claimed by this addendum.

## Current exact-tag and current-HEAD addendum — 2026-09-20

The exact tag `v0.1.2` resolves to
`abcd3fb3b7ced0b34e33a34bfb335a800806f00b`. Release run
[35506194835](https://github.com/udayvarmora07/autogit/actions/runs/35506194835)
passed quality, independent reproducibility, artifact attestation, and
consumer verification; protected deployment `6552017112` then published the
verified [GitHub Release v0.1.2](https://github.com/udayvarmora07/autogit/releases/tag/v0.1.2).
The current push run
[35505877385](https://github.com/udayvarmora07/autogit/actions/runs/35505877385)
passed all push-triggered core jobs. Exact-HEAD manual matrix
[35508065704](https://github.com/udayvarmora07/autogit/actions/runs/35508065704)
also passed all 20 jobs, including native platform and six artifact lifecycle
jobs. Exact-HEAD canary run
[35508083681](https://github.com/udayvarmora07/autogit/actions/runs/35508083681)
passed with verified cleanup.

The published release bundle passed exact consumer verification and direct
checksum validation. The native Linux lifecycle and technical rollback drills
also passed. P5-06 remains open for package-repository publication and clean
native package-channel acceptance; P5-07 remains open for clean-machine
package testing and the named rollback/security tabletop.

The earlier manual matrix
[35507277048](https://github.com/udayvarmora07/autogit/actions/runs/35507277048)
is retained as a diagnostic because its Ubuntu provider-postcondition and
Windows AppContainer native tests failed. The successful exact-HEAD run above
supersedes it for implementation evidence; named Phase 1/Phase 2 review and
promotion decisions remain separate.
