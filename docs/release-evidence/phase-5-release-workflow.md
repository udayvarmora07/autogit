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
| Release identity | The artifact job injects the exact tag and event SHA into all six reproducible binaries and checks the native binary's reported identity. |
| SPDX/CycloneDX SBOMs | `cmd/autogit-sbom` consumes the Go module graph, sorts module identities, records no local filesystem paths, and emits deterministic SPDX 2.3 and CycloneDX 1.5 JSON documents. A fixed creation time makes both documents reproducible. |
| Binary vulnerability evidence | Each Linux, macOS, and Windows release binary is scanned with `golang.org/x/vuln/cmd/govulncheck@v1.7.0 -mode=binary`; reports are included in the checksum manifest. |
| Signed provenance | Pinned official `actions/attest@508db95dd578ae2727ebd6217d5ba78e4fbda05d` creates a SLSA provenance attestation and a separate SBOM attestation using the checksum subjects. Required OIDC, attestation, and artifact metadata permissions exist only on the post-quality job. |
| Independent reproducibility | Ubuntu and macOS jobs rebuild the exact tag with the same source-derived `SOURCE_DATE_EPOCH`, upload separate artifact sets, and a third Ubuntu job compares every checksum and binary byte-for-byte. The attestation job depends on that comparison. |
| Consumer verification | `scripts/verify-release-artifacts.sh` rejects unsafe or incomplete manifests, verifies checksums for all six binaries, both SBOMs, and vulnerability reports, then verifies binary provenance plus SPDX and CycloneDX predicates against a release binary with the exact repository, workflow, tag, source commit, and hosted-runner requirement. The release workflow runs this verifier after all attestations and before final evidence upload. |
| Verified GitHub Release publication | A separate `release-publish` protected environment downloads only the attested bundle, rechecks `SHA256SUMS`, the embedded tag/commit identity, and all required provenance/SBOM predicates on the publication runner, then publishes the exact tag with `gh release create --verify-tag`. It has `contents: write` plus read-only attestation metadata only on this final job; package channels remain deferred until demand and native install evidence exist. |

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

## Acceptance boundary

No release tag was created during this implementation. Consequently there is
no hosted attestation, no release-environment approval record, and no consumer
verification result yet. P5-03/P5-05 remain unchecked in the tracker until an
authorized release owner runs an exact tag, verifies the independent-runner
comparison, and checks the hosted bundles.
