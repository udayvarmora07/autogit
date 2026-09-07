#!/usr/bin/env bash
set -euo pipefail

# Named test tiers keep the fast feedback path bounded while retaining an
# explicit, reproducible command for every expensive release matrix.
usage() {
  cat <<'EOF'
Usage: scripts/test-suites.sh SUITE

Suites:
  presubmit    short tests and static Go checks
  core         complete local, credential-free merge tests
  race         race-enabled local tests
  integration  disposable local integration tests
  soak         full 1,000-schedule crash/recovery matrices
  fuzz         bounded fuzz corpus execution
  canary       opt-in disposable provider canary
  release      release integration tests and artifact smoke
EOF
}

suite="${1:-}"
case "$suite" in
  presubmit)
    go test -short ./...
    go vet ./...
    ;;
  core)
    go test ./...
    ;;
  race)
    go test -race ./...
    ;;
  integration)
    go test -run 'Test(.*ProcessBoundary|.*Recovery|.*Repository|.*Workflow|.*Transaction)' ./...
    ;;
  soak)
    go test -tags soak ./...
    ;;
  fuzz)
    fuzz_time="${AUTOGIT_FUZZ_TIME:-30s}"
    while IFS=' ' read -r package target; do
      go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "$fuzz_time"
    done <<'EOF'
./internal/events FuzzDecodeNeverPanics
./internal/adapters FuzzP303MalformedClientFieldsNeverPanic
./internal/provider FuzzRemoteIdentityValidationNeverPanics
./internal/provider FuzzProviderRefValidationNeverPanics
./internal/gitport FuzzPushArgsNeverBuildsOptionLikeDestination
./internal/policy FuzzPolicyValidationAndMergeNeverPanics
./internal/install FuzzConfigPathScopeNeverPanics
./internal/db FuzzMigrationBoundaryNeverPanics
./internal/state FuzzStatusIdentityValidationNeverPanics
./internal/security FuzzScannerNeverPanics
EOF
    ;;
  canary)
    bash scripts/github-canary.sh
    ;;
  release)
    go test -tags release_integration ./scripts -run '^TestReleaseBuild' -count=1
    release_output="${AUTOGIT_RELEASE_OUTPUT:-}"
    if [[ -z "$release_output" ]]; then
      release_output="$(mktemp -d "${TMPDIR:-/tmp}/autogit-release.XXXXXX")"
      trap 'rm -rf "$release_output"' EXIT
    fi
    bash scripts/release-build.sh --output "$release_output"
    bash scripts/artifact-smoke.sh --directory "$release_output"
    ;;
  -h|--help)
    usage
    ;;
  *)
    echo "unknown suite: ${suite:-<missing>}" >&2
    usage >&2
    exit 2
    ;;
esac
