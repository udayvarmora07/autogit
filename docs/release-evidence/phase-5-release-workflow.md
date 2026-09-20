# Phase 5 release workflow implementation evidence

Date: 2026-09-11
Scope: P5-03 through P5-06 local implementation slices
Status: local implementation slices present; exact-tag hosted acceptance remains open

## Delivered controls

| Control | Implementation |
| --- | --- |
| Exact tag/commit identity | `.github/workflows/release.yml` accepts `v*` pushes, then requires `vMAJOR.MINOR.PATCH`, the peeled tag commit, `git describe --exact-match`, and `github.sha` to agree. |
| Clean, non-reused build | Both jobs reject a dirty checkout; the artifact job requires that `dist` does not exist before invoking `scripts/release-build.sh`. |
| Complete quality gate | The read-only job runs uncached tests, race tests, vet, build, release integration/artifact smoke, source govulncheck, ShellCheck/syntax, and dependency/workflow policy checks. |
| Exact-tag machine evidence | The quality job runs `scripts/generate-evidence.sh --full --tag TAG --require-tag` in a clean checkout, retains the redacted `autogit.release-evidence.json`, and carries it into the attested release bundle. |
| Release identity | The artifact job injects the exact tag and event SHA into all six reproducible binaries and checks the native binary's reported identity. |
| SPDX/CycloneDX SBOMs | `cmd/autogit-sbom` consumes the Go module graph, sorts module identities, records no local filesystem paths, and emits deterministic SPDX 2.3 and CycloneDX 1.5 JSON documents. A fixed creation time makes both documents reproducible. |
| Binary vulnerability evidence | Each Linux, macOS, and Windows release binary is scanned with `golang.org/x/vuln/cmd/govulncheck@v1.7.0 -mode=binary`; reports are included in the checksum manifest. |
| Signed provenance | Pinned official `actions/attest@508db95dd578ae2727ebd6217d5ba78e4fbda05d` creates a SLSA provenance attestation and a separate SBOM attestation using the checksum subjects. Required OIDC, attestation, and artifact metadata permissions exist only on the post-quality job. |
| Independent reproducibility | Ubuntu and macOS jobs rebuild the exact tag with the same source-derived `SOURCE_DATE_EPOCH`, upload separate artifact sets, and a third Ubuntu job compares every checksum and binary byte-for-byte. The attestation job depends on that comparison. |
| Consumer verification | `scripts/verify-release-artifacts.sh` rejects unsafe, incomplete, or unexpectedly expanded bundles, verifies checksums for all six binaries, both SBOMs, the exact-tag machine-evidence manifest, and vulnerability reports, then verifies binary provenance plus SPDX and CycloneDX predicates against a release binary with the exact repository, workflow, tag, source commit, and hosted-runner requirement. The release workflow runs this verifier after all attestations and before final evidence upload. |
| Package-channel metadata | `scripts/generate-package-metadata.sh` validates the exact semver tag, six binary files, and their entries in `SHA256SUMS`, then emits deterministic Homebrew and Scoop metadata with release URLs and digests. The publication runner regenerates both files from the downloaded bundle and requires byte-for-byte equality before attaching them to the release. Package-repository publication and native clean-machine package tests remain open. |
| Verified GitHub Release publication | A separate `release-publish` protected environment downloads only the attested bundle, rechecks `SHA256SUMS`, the embedded tag/commit identity, all required provenance/SBOM predicates, and package metadata on the publication runner, then publishes the exact tag with `gh release create --verify-tag`. It has `contents: write` plus read-only attestation metadata only on this final job; package channels remain deferred until demand and native install evidence exist. |

## Local verification

The following checks passed for this implementation:

```text
go test ./cmd/autogit-sbom ./scripts
actionlint .github/workflows/*.yml
bash -n scripts/*.sh
shellcheck --shell=bash --severity=warning scripts/*.sh
bash scripts/check-dependencies.sh
go test ./scripts -run TestReleaseWorkflowRevalidatesAttestedBundleBeforePublication
git diff --check
```

The package metadata generator is covered by `scripts` tests using a complete
synthetic six-binary release directory. The test parses the generated Scoop
manifest, checks Homebrew URLs and digests, rejects invalid tags, and rejects
output-directory reuse without contacting GitHub or a package repository.

After an authorized hosted release, consumers can run:

```text
bash scripts/verify-release-artifacts.sh --directory DIST \
  --repo udayvarmora07/autogit --tag vMAJOR.MINOR.PATCH --commit FULL_SHA
```

The SBOM generator was also run against the repository's complete Go module
graph. It emitted valid SPDX 2.3 and CycloneDX 1.5 documents with 29 module
packages, stable ordering, and no `/home/` or `/tmp/` path disclosure.

The existing local release integration suite also builds the supported target
set twice with separate output directories and verifies matching bytes. The
cross-runner comparison is intentionally hosted-only; it cannot be claimed
from this Linux workstation. The final GitHub Release publication job also
cannot be claimed as executed until an approved exact tag reaches the hosted
workflow and the protected `release-publish` environment is approved.

On 2026-09-15, current-tree core workflow dispatch
[34961479624](https://github.com/udayvarmora07/autogit/actions/runs/34961479624)
passed all 20 quality jobs against
`a51eb9718bac0e3e514e196fa4e1156a47865ade`, including the six native artifact
smoke/lifecycle jobs, reproducible-build check, security checks, fuzz, soak,
and policy checks. This validates the current implementation path; it is not
an execution of the exact-tag `release.yml` workflow and does not create
attestations or a release.

On 2026-09-20, the local clean-room implementation checks passed on the
current checkout: `go test -count=1 ./scripts -run 'TestRelease'`,
`bash scripts/test-suites.sh release`, `bash scripts/check-shell.sh`, and
`git diff --check`. The release suite built and checksum-verified all six
supported targets, ran artifact smoke, and completed the raw-binary install
drill.

The exact-tag hosted execution for `v0.1.1` at commit
`0743443224dd809e652ea69d5d6b275eef29a4ce` passed in
[run 35496909526](https://github.com/udayvarmora07/autogit/actions/runs/35496909526):
the quality job generated a clean `tag_verified: true` evidence manifest, the
Ubuntu/macOS independent builds compared byte-for-byte, and the attestation
job generated SPDX/CycloneDX SBOMs, six binary govulncheck reports, signed
checksums, SLSA provenance, SBOM attestations, and a passing consumer
verification. The retained machine manifest SHA-256 is
`479c818954f9198a43e7c81b709c7bf0316d06dd96e54b37433506080662310e`.

The initial `v0.1.0` run failed at the attestation job's clean-checkout check
because downloaded evidence was placed in the worktree. Commit `0743443`
moved those downloads under `$RUNNER_TEMP` and updated the workflow regression
test; the corrected `v0.1.1` quality and attestation jobs passed. The later
publication job is P5-06 scope and did not publish a GitHub Release; its
package-metadata revalidation remains a separate follow-up.

## Acceptance boundary

P5-03, P5-04, and P5-05 are accepted for the bounded private-alpha scope by
the exact-tag evidence above. No GitHub Release or package-repository
publication is claimed: those are P5-06 gates and still require the protected
publication path, package-channel demand, and native clean-machine install
evidence.
