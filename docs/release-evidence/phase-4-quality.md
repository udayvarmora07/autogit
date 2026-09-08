# Phase 4 and Phase 5 local implementation evidence

Date: 2026-09-08
Scope: P4-01 through P4-08 and P5-01 through P5-02
Status: local implementation complete; native, tagged-artifact, provider, and
named-reviewer promotion gates remain separate

## Delivered controls

| ID | Evidence |
| --- | --- |
| P4-01 | `scripts/test-suites.sh` names presubmit, core, race, integration, soak, fuzz, canary, and release suites. The default process-boundary matrices use 50 representative schedules; `-tags soak` runs the retained 1,000-schedule Linux/macOS matrix. CI runs the fast path on changes and the soak/fuzz jobs only on schedule or manual dispatch. |
| P4-02 | `scripts/check-shell.sh` runs `bash -n` and ShellCheck when available/required. `scripts/shell_scripts_test.go` injects malformed suite, artifact, canary, performance, and release conditions and checks fail-closed behavior. |
| P4-03 | `scripts/artifact-smoke.sh` verifies SHA-256 manifests and executes the built binary through version, doctor, explicit local init, plan, and status. Native CI jobs run this against their built artifact; the manual/scheduled native artifact matrix covers Linux amd64/arm64, macOS Intel/arm64, and Windows amd64/arm64; release integration tests retain cross-target build checks. |
| P4-04 | Fuzz targets now cover canonical events/JSON, adapters, provider identities/refs, Git push arguments, policy merge/validation, config paths, migration/version boundaries, state/status identities, and scanner inputs. Seed corpora are embedded in the fuzz tests and `test-suites.sh fuzz` enforces at least 100,000 generated inputs per target by default. |
| P4-05 | `internal/provider/chaos_test.go` covers network stalls, partial responses, response redaction, rate-limit metadata through the existing REST contract tests, and duplicate push replay without a second effect. `internal/securefs` injects permission loss, `internal/coordinator` advances the lease clock, and `internal/process` uses an isolated file-size ceiling as the deterministic disk-full/write-exhaustion proxy. Existing state/process recovery matrices cover signal/kill and lock contention boundaries. |
| P4-06 | `scripts/scenario-eval.sh` grades final branch, index, AutoGit ref, consent, and read-only state outcomes. It records only bounded scenario IDs and pass/fail facts, never source or raw paths. |
| P4-07 | `internal/compatibility` validates seven explicit support windows. `cmd/autogit-compat` emits machine-readable due/expired reports and a redacted issue body; the scheduled workflow creates at most one open review issue. |
| P4-08 | `cmd/autogit-evidence` and `scripts/generate-evidence.sh` bind suite results, controls, requirement IDs, test selectors, threat IDs, toolchain, platform, exact Git HEAD, optional exact tag, and SHA-256 artifact identities into JSON. Dirty working trees are rejected unless `--allow-dirty` is explicit; artifact paths are reduced to names and symlinks are rejected. |
| P5-01 | Apache-2.0 license, security reporting, contribution, conduct, `.github/CODEOWNERS`, support, and changelog artifacts are present and linked from README. |
| P5-02 | Dependabot, dependency review, CodeQL, Scorecard, pinned-action enforcement, module verification, and the SPDX license policy are present. `scripts/check-dependencies.sh` is the local gate. |

## Local verification

The following checks passed during this implementation slice on Linux/amd64:

```text
go test ./...
go vet ./...
go test -count=1 -json ./...                 # 853 deterministic test cases
bash scripts/test-suites.sh presubmit
bash scripts/test-suites.sh integration
bash scripts/test-suites.sh race
bash scripts/test-suites.sh soak              # 1,000-schedule Linux matrix
AUTOGIT_FUZZ_TIME=60s bash scripts/test-suites.sh fuzz
bash scripts/test-suites.sh release            # six cross-target artifacts + host smoke
PATH=... bash scripts/check-shell.sh           # ShellCheck 0.11.0
bash scripts/check-dependencies.sh
actionlint .github/workflows/*.yml
bash scripts/scenario-eval.sh --binary <built artifact>
go test ./cmd/autogit-evidence ./scripts
go test . -run '^TestReleaseCompatibilityManifestMatchesSupportedContracts$' -count=1
python3 -m json.tool docs/release-evidence/phase-4-quality.json
git diff --check
```

The checked-in JSON is a clean manifest for commit
`e6ed519c01cdefa2db5e6579290995e21ddd35eb`. Its ten local suite results include
the 60-second fuzz budget for every target, the 1,000-schedule Linux soak
matrix, six freshly built cross-target artifact hashes, and Linux/amd64
artifact smoke. The adapter fuzz harness reuses immutable adapter setup so the
100,000-input floor is achievable without weakening the quota. Hosted
performance gates retry transient high-tail samples while preserving the
configured p95 limits.

The exact-commit manual CI run `34216754925` passed presubmit, all ten fuzz
targets, native Linux/macOS/Windows tests and performance gates, the six-entry
native artifact matrix, 1,000-schedule soak on all three platforms, all three
cross-builds, reproducible release binaries, and security analysis. Its
Windows ARM artifact job also passed the hardened Git preflight and artifact
smoke. Push CI run `34216741860` and the separate security workflow
`34216741816` also passed for the same commit. `tag_verified` remains false
because this is implementation evidence, not a release-tag approval. These CI
records do not claim a live provider canary, signing, provenance, or named
release review.

Use the exact commands below to create a release-bound record after the
working tree is clean and tagged:

```text
bash scripts/test-suites.sh presubmit
bash scripts/test-suites.sh core
bash scripts/test-suites.sh race
bash scripts/test-suites.sh soak
AUTOGIT_FUZZ_TIME=60s bash scripts/generate-evidence.sh \
  --full --tag vX.Y.Z --require-tag \
  --output docs/release-evidence/phase-4-quality.json
```

This bundle does not claim that native artifact execution, a live provider
canary, signed provenance, or named review has already occurred. The phase and
alpha gates in `todo.md` remain authoritative.
