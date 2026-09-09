# Phase 4 and Phase 5 local implementation evidence

Date: 2026-09-09
Scope: P4-01 through P4-08 and P5-01 through P5-02
Status: implementation complete; local and hosted native evidence is current;
tagged-artifact, provider, signing, and named-reviewer promotion gates remain
separate

## Delivered controls

| ID | Evidence |
| --- | --- |
| P4-01 | `scripts/test-suites.sh` names presubmit, core, race, integration, soak, fuzz, canary, and release suites. The default process-boundary matrices use 50 representative schedules; `-tags soak` runs the retained 1,000-schedule Linux/macOS matrix. CI runs the fast path on changes and the soak/fuzz jobs only on schedule or manual dispatch. |
| P4-02 | `scripts/check-shell.sh` runs `bash -n` and ShellCheck when available/required. `scripts/shell_scripts_test.go` injects malformed suite, artifact, canary, performance, and release conditions and checks fail-closed behavior. |
| P4-03 | `scripts/artifact-smoke.sh` verifies SHA-256 manifests, checks the live system Git version against the minimum in `docs/compatibility-manifest.json`, and executes the built binary through version, doctor, explicit local init, plan, and status. Native CI jobs run this against their built artifact; the manual/scheduled native artifact matrix covers Linux amd64/arm64, macOS Intel/arm64, and Windows amd64/arm64; release integration tests retain cross-target build checks. |
| P4-04 | Fuzz targets now cover canonical events/JSON, adapters, provider identities/refs, Git push arguments, policy merge/validation, config paths, migration/version boundaries, state/status identities, and scanner inputs. Seed corpora are embedded in the fuzz tests and `test-suites.sh fuzz` enforces at least 100,000 generated inputs per target by default. |
| P4-05 | `internal/provider/chaos_test.go` covers network stalls, partial responses, response redaction, rate-limit metadata through the existing REST contract tests, and duplicate push replay without a second effect. `internal/securefs` injects permission loss, `internal/coordinator` advances the lease clock, and `internal/process` uses an isolated file-size ceiling as the deterministic disk-full/write-exhaustion proxy. Existing state/process recovery matrices cover signal/kill and lock contention boundaries. |
| P4-06 | `scripts/scenario-eval.sh` grades final branch, index, AutoGit ref, consent, provider-effect absence, and client configuration outcomes for all six registered clients. It records only bounded scenario IDs and pass/fail facts, never source or raw paths; `--trace` emits a bounded platform/Git/client diagnostic artifact for native CI retention. |
| P4-07 | `internal/compatibility` validates seven explicit support windows. `cmd/autogit-compat` emits machine-readable due/expired reports and a redacted issue body; the scheduled workflow creates at most one open review issue. |
| P4-08 | `cmd/autogit-evidence` and `scripts/generate-evidence.sh` bind suite results, controls, requirement IDs, test selectors, threat IDs, toolchain, platform, exact Git HEAD, optional exact tag, and SHA-256 artifact identities into JSON. Dirty working trees are rejected unless `--allow-dirty` is explicit; artifact paths are reduced to names and symlinks are rejected. |
| P5-01 | Apache-2.0 license, security reporting, contribution, conduct, `.github/CODEOWNERS`, support, and changelog artifacts are present and linked from README. |
| P5-02 | Dependabot, dependency review, CodeQL, Scorecard, pinned-action enforcement, module verification, and the SPDX license policy are present. `scripts/check-dependencies.sh` is enforced locally and by the CI `dependency-policy` job. |

## Local verification

The following checks passed during this implementation slice on Linux/amd64:

```text
go test ./...
go vet ./...
go test -count=1 -json ./...                 # 859 deterministic test cases
bash scripts/test-suites.sh presubmit
bash scripts/test-suites.sh integration
bash scripts/test-suites.sh race
bash scripts/test-suites.sh soak              # 1,000-schedule Linux matrix
AUTOGIT_FUZZ_TIME=40s bash scripts/test-suites.sh fuzz
bash scripts/test-suites.sh release            # six cross-target artifacts + host smoke
PATH=... bash scripts/check-shell.sh           # ShellCheck 0.11.0
bash scripts/check-dependencies.sh
actionlint .github/workflows/*.yml
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
`94ecc899cc0c367fb2540f4ad9fb537b36b2480b`. Its ten local suite results use
the corrected 40-second fuzz budget for all 10 targets, leaving shutdown
headroom while retaining the 1,000-schedule
Linux soak matrix, six freshly built cross-target artifact hashes, Linux/amd64
artifact smoke, and the 20-case six-client final-state scenario matrix. The
adapter fuzz harness reuses immutable adapter setup so the 100,000-input floor
is achievable without weakening the quota. The performance gate uses one-
second steady-state benchmark windows and preserves the configured p95 limits;
Windows hosted jobs collect four independent attempts for transient scheduler
tails.

The current exact-snapshot full hosted CI dispatch `34344709357` passed all
20/20 jobs against `94ecc899cc0c367fb2540f4ad9fb537b36b2480b`:
presubmit, native Linux/macOS/Windows tests, six native artifact targets,
three soak targets, fuzz, three cross-builds, reproducible release binaries,
security analysis, and the retained scenario/performance checks. It retained
the native scenario traces and performance artifacts for the exact SHA. The
push-triggered core run `34343500284` and security run `34343500286` also passed
against the same exact SHA. `tag_verified` remains false because this is
implementation evidence, not release-tag approval. The private canary
dispatch `34324968472` was an earlier `1ec8c39` attempt and stopped at
authentication: the dedicated `AUTOGIT_CANARY_TOKEN` secret was empty, so the
canary did not run and the allowlisted repository was confirmed absent. It is
not current-HEAD canary evidence. The former `ded26c3` manifest and hosted
run `34327518122` remain historical records. These records do not claim a live
provider canary, signing, published provenance, or named release review.

The hosted compatibility-window review `34347979343` passed at the current
branch commit `211fb69b9cc2d23813c7feda5dc4831357a586ff`; all advertised
windows were current and no expiry issue was generated.

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

This bundle does not claim that native artifact execution, a live provider
canary, signed provenance, or named review has already occurred. The phase and
alpha gates in `todo.md` remain authoritative.
