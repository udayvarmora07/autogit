# Phase 4 and Phase 5 local implementation evidence

Date: 2026-09-12
Scope: P4-01 through P4-08 and P5-01 through P5-02
Status: implementation complete; exact-snapshot local and hosted quality
evidence is current; tagged-artifact, provider, signing, and named-reviewer
promotion gates remain separate

## Delivered controls

The package-by-package acceptance reconciliation is recorded in the
[Phase 4/5 acceptance matrix](phase-4-5-acceptance-matrix.md).

| ID | Evidence |
| --- | --- |
| P4-01 | `scripts/test-suites.sh` names presubmit, core, race, integration, soak, fuzz, canary, and release suites. The default process-boundary matrices use 50 representative schedules; `-tags soak` runs the retained 1,000-schedule Linux/macOS matrix. CI runs the fast path on changes and the soak/fuzz jobs only on schedule or manual dispatch. |
| P4-02 | `scripts/check-shell.sh` runs `bash -n` and ShellCheck when available/required. `scripts/shell_scripts_test.go` injects malformed suite, artifact, canary, performance, and release conditions and checks fail-closed behavior. |
| P4-03 | `scripts/artifact-smoke.sh` verifies SHA-256 manifests, checks the live system Git version against the minimum in `docs/compatibility-manifest.json`, and executes the built binary through version, doctor, explicit local init, plan, and status. Native CI jobs run this against their built artifact; the manual/scheduled native artifact matrix covers Linux amd64/arm64, macOS Intel/arm64, and Windows amd64/arm64, and now runs the raw-binary install lifecycle drill for each target; release integration tests retain cross-target build checks. |
| P4-04 | Fuzz targets now cover canonical events/JSON, adapters, provider identities/refs, Git push arguments, policy merge/validation, config paths, migration/version boundaries, state/status identities, and scanner inputs. Seed corpora are embedded in the fuzz tests and `test-suites.sh fuzz` enforces at least 100,000 generated inputs per target by default. |
| P4-05 | `internal/provider/chaos_test.go` covers network stalls, partial responses, response redaction, rate-limit metadata through the existing REST contract tests, and duplicate push replay without a second effect. `internal/securefs` injects permission loss, `internal/coordinator` advances the lease clock, and `internal/process` uses an isolated file-size ceiling as the deterministic disk-full/write-exhaustion proxy. Existing state/process recovery matrices cover signal/kill and lock contention boundaries. |
| P4-06 | `scripts/scenario-eval.sh` grades final branch, index, AutoGit ref, consent, provider-effect absence, and client configuration outcomes for all six registered clients. It records only bounded scenario IDs and pass/fail facts, never source or raw paths; `--trace` emits a bounded platform/Git/client diagnostic artifact for native CI retention. |
| P4-07 | `internal/compatibility` validates seven explicit support windows. `cmd/autogit-compat` emits machine-readable due/expired reports and a redacted issue body; the scheduled workflow creates at most one open review issue. |
| P4-08 | `cmd/autogit-evidence` and `scripts/generate-evidence.sh` bind suite results, controls, requirement IDs, test selectors, threat IDs, toolchain, platform, exact Git HEAD, optional exact tag, and SHA-256 artifact identities into JSON. The manifest includes the P1-04 isolation, P5-04 SBOM, and P5-07 install/rollback controls with their platform/workflow selectors. Dirty working trees are rejected unless `--allow-dirty` is explicit; artifact paths are reduced to names and symlinks are rejected. |
| P5-01 | Apache-2.0 license, security reporting, contribution, conduct, `.github/CODEOWNERS`, support, and changelog artifacts are present and linked from README. |
| P5-02 | Dependabot, dependency review, CodeQL, Scorecard, pinned-action enforcement, module verification, and the SPDX license policy are present. `scripts/check-dependencies.sh` is enforced locally and by the CI `dependency-policy` job. |

## Local verification

The following checks passed during this implementation slice on Linux/amd64:

```text
GOFLAGS=-mod=readonly go test -count=1 ./...
GOFLAGS=-mod=readonly go test -race -count=1 ./...
GOFLAGS=-mod=readonly go vet ./...
go test -count=1 -json ./...
bash scripts/test-suites.sh presubmit
bash scripts/test-suites.sh integration
bash scripts/test-suites.sh race
bash scripts/test-suites.sh soak              # 1,000-schedule Linux matrix
AUTOGIT_FUZZ_TIME=40s bash scripts/test-suites.sh fuzz
bash scripts/test-suites.sh release            # six cross-target artifacts + host smoke
bash scripts/check-shell.sh                   # ShellCheck 0.11.0
bash scripts/check-dependencies.sh
actionlint .github/workflows/*.yml
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go run github.com/securego/gosec/v2/cmd/gosec@v2.29.0 ./...
trace="$(mktemp "${TMPDIR:-/tmp}/autogit-scenario-trace.XXXXXX")"
bash scripts/scenario-eval.sh --binary <built artifact> --trace "$trace"
python3 -m json.tool "$trace"
go test ./cmd/autogit-evidence ./scripts
go test . -run '^TestReleaseCompatibilityManifestMatchesSupportedContracts$' -count=1
python3 -m json.tool docs/release-evidence/phase-4-quality.json
git diff --check
```

The checked-in JSON manifest records a clean collection performed at evidence
snapshot commit
`06ba87508ff53abc8b0001885e8341b6c07ad27a`. Its ten local suite results use
the deterministic 100,000-input fuzz floor for all 10 targets while retaining
the 1,000-schedule
Linux soak matrix, six freshly built cross-target artifact hashes, Linux/amd64
artifact smoke, and the 20-case six-client final-state scenario matrix. The
adapter fuzz harness reuses immutable adapter setup so the 100,000-input floor
is achievable without weakening the quota. The performance gate uses one-
second steady-state benchmark windows and preserves the configured p95 limits;
Windows hosted jobs collect four independent attempts for transient scheduler
tails.

The follow-up implementation commit
`06ba87508ff53abc8b0001885e8341b6c07ad27a` fixed Windows Git Bash
package-metadata hashing by using `cygpath` and PowerShell `Get-FileHash`; a
controlled-shim regression test covers that branch. Core run `34610742466`
and security run `34610742472` passed against that exact SHA, including the
native Windows package-metadata test. The regenerated manifest remains
implementation evidence with `tag_verified: false`; it does not satisfy the exact-tag,
attestation, independent-runner, package-channel, native clean-machine, or
named-review gates.

The subsequent manual full matrix `34627202246` passed all 20/20 jobs against
then-current HEAD `a07fd84cb4b345e4dccdd3f950bd82dd662f5d87`, a
documentation-only descendant of the evidence snapshot. It retained native Linux/macOS/Windows
checks, all six native artifact lifecycle drills, fuzz, soak, reproducibility,
security, and dependency-policy results. This strengthens the current-tree
validation for the Phase 0, Phase 1, and P4-03 gate reviews without changing
the generated manifest identity or the remaining acceptance boundary.

The earlier exact-snapshot full hosted CI dispatch `34600488282` passed all
20/20 jobs against `16c6325952b7937cdf8333b12cc7de0189b91f2b`. The follow-up
full dispatch `34603093643` passed all 20/20 jobs against
`c0822118b04743406b5fb88b3ed30d79e4a5486d`, including the six-target native
install lifecycle drill. The current-source full dispatch `34604368677`
also passed all 20/20 jobs against
`0d40d6af1ae38618edfa904f28fd7267e41e179e`:
presubmit, native Linux/macOS/Windows tests, six native artifact targets,
three soak targets, fuzz, three cross-builds, reproducible release binaries,
security analysis, and the retained scenario/performance checks. It retained
the native scenario traces, performance artifacts, and six redacted install
drill records for the exact SHA. The current-source push run `34604333600`
also passed its native Linux/macOS/Windows core checks, and the security run
`34604333592` passed against the same source SHA. The hosted
compatibility-window review
`34590952233` remains historical evidence for the preceding snapshot; the
current local compatibility suite passed in the manifest.
`tag_verified` remains false because this is implementation evidence, not
release-tag approval. The private canary
dispatch `34324968472` was an earlier `1ec8c39` attempt and stopped at
authentication: the dedicated `AUTOGIT_CANARY_TOKEN` secret was empty, so the
canary did not run and the allowlisted repository was confirmed absent. It is
not current-HEAD canary evidence. The former `ded26c3` manifest and hosted
run `34327518122` remain historical records. These records do not claim a live
provider canary, signing, published provenance, or named release review.

The current exact-SHA manual full matrix `34682716776` passed all 20/20 jobs
against `1406352345886c116ba7750369fc3a14df960b63`, including native
Linux/macOS/Windows tests, all six native artifact lifecycle drills, three
soak targets, fuzz, reproducible release binaries, security analysis,
dependency policy, and the scenario/performance checks. These fresh records
strengthen implementation evidence but do not satisfy the exact-tag,
signing/provenance, AppContainer, adapter-native, or named acceptance gates.

Use the exact commands below to create a release-bound record after the
working tree is clean and tagged:

```text
bash scripts/test-suites.sh presubmit
bash scripts/test-suites.sh core
bash scripts/test-suites.sh race
bash scripts/test-suites.sh soak
AUTOGIT_FUZZ_TIME=40s bash scripts/generate-evidence.sh \
  --full --tag vX.Y.Z --require-tag \
  --output docs/release-evidence/phase-4-quality.json
```

This bundle records native artifact execution and a live private provider
canary for the exact source SHA. It does not claim tag-bound signed
provenance or named review; the phase and alpha gates in `todo.md` remain
authoritative.
