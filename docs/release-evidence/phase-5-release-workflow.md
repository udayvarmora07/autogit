# Phase 5 release workflow implementation evidence

Date: 2026-09-09  
Scope: P5-03 and P5-04 local implementation  
Status: implementation complete; exact-tag hosted acceptance remains open

## Delivered controls

| Control | Implementation |
| --- | --- |
| Exact tag/commit identity | `.github/workflows/release.yml` accepts `v*` pushes, then requires `vMAJOR.MINOR.PATCH`, the peeled tag commit, `git describe --exact-match`, and `github.sha` to agree. |
| Clean, non-reused build | Both jobs reject a dirty checkout; the artifact job requires that `dist` does not exist before invoking `scripts/release-build.sh`. |
| Complete quality gate | The read-only job runs uncached tests, race tests, vet, build, release integration/artifact smoke, source govulncheck, ShellCheck/syntax, and dependency/workflow policy checks. |
| Release identity | The artifact job injects the exact tag and event SHA into all six reproducible binaries and checks the native binary's reported identity. |
| SPDX SBOM | `cmd/autogit-sbom` consumes the Go module graph, sorts module identities, records no local filesystem paths, and emits SPDX 2.3 JSON. A fixed creation time makes the document reproducible. |
| Binary vulnerability evidence | Each Linux, macOS, and Windows release binary is scanned with `golang.org/x/vuln/cmd/govulncheck@v1.7.0 -mode=binary`; reports are included in the checksum manifest. |
| Signed provenance | Pinned official `actions/attest@508db95dd578ae2727ebd6217d5ba78e4fbda05d` creates a SLSA provenance attestation and a separate SBOM attestation using the checksum subjects. Required OIDC, attestation, and artifact metadata permissions exist only on the post-quality job. |

## Local verification

The following checks passed for this implementation:

```text
go test ./cmd/autogit-sbom ./scripts
actionlint .github/workflows/*.yml
bash -n scripts/*.sh
shellcheck --shell=bash --severity=warning scripts/*.sh
bash scripts/check-dependencies.sh
git diff --check
```

The SBOM generator was also run against the repository's complete Go module
graph. It emitted a valid SPDX-shaped JSON document with 29 module packages,
stable ordering, and no `/home/` or `/tmp/` path disclosure.

## Acceptance boundary

No release tag was created during this implementation. Consequently there is
no hosted attestation, no release-environment approval record, and no consumer
verification result yet. P5-03/P5-04 remain unchecked in the tracker until an
authorized release owner runs an exact tag and verifies the published bundles.
