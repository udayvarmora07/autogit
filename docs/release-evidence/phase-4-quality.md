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
| P4-03 | `scripts/artifact-smoke.sh` verifies SHA-256 manifests and executes the built binary through version, doctor, explicit local init, plan, and status. Native CI jobs run this against their built artifact; release integration tests retain cross-target build checks. |
| P4-04 | Fuzz targets now cover canonical events/JSON, adapters, provider identities/refs, Git push arguments, policy merge/validation, config paths, migration/version boundaries, state/status identities, and scanner inputs. Seed corpora are embedded in the fuzz tests and `test-suites.sh fuzz` enforces at least 100,000 generated inputs per target by default. |
| P4-05 | `internal/provider/chaos_test.go` covers network stalls, partial responses, response redaction, rate-limit metadata through the existing REST contract tests, and duplicate push replay without a second effect. Existing state/process recovery matrices cover signal/kill and lock contention boundaries. |
| P4-06 | `scripts/scenario-eval.sh` grades final branch, index, AutoGit ref, consent, and read-only state outcomes. It records only bounded scenario IDs and pass/fail facts, never source or raw paths. |
| P4-07 | `internal/compatibility` validates seven explicit support windows. `cmd/autogit-compat` emits machine-readable due/expired reports and a redacted issue body; the scheduled workflow creates at most one open review issue. |
| P4-08 | `cmd/autogit-evidence` and `scripts/generate-evidence.sh` bind suite results, controls, requirement IDs, test selectors, threat IDs, toolchain, platform, exact Git HEAD, optional exact tag, and SHA-256 artifact identities into JSON. Dirty working trees are rejected unless `--allow-dirty` is explicit; artifact paths are reduced to names and symlinks are rejected. |
| P5-01 | Apache-2.0 license, security reporting, contribution, conduct, CODEOWNERS, support, and changelog artifacts are present and linked from README. |
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

The checked-in JSON is a fresh local full-suite record for commit
`b9f4d55c7b2ed9385e46751512c7c98db2fa5e40`; it deliberately says
`working_tree: dirty` and `tag_verified: false` because this implementation
slice has not been committed or tagged. It contains six cross-target artifact
hashes, but only the Linux/amd64 artifact was executed in this environment.

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
