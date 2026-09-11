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
    # Request the execution floor directly instead of relying on a wall-clock
    # throughput estimate. The timeout remains a hard bound for pathological
    # inputs or a stuck fuzz target.
    fuzz_timeout="${AUTOGIT_FUZZ_TIME:-40s}"
    min_execs="${AUTOGIT_FUZZ_MIN_EXECS:-100000}"
    if [[ ! "$min_execs" =~ ^[0-9]+$ ]]; then
      echo "AUTOGIT_FUZZ_MIN_EXECS must be an unsigned integer" >&2
      exit 2
    fi
    while IFS=' ' read -r package target; do
      output="$(go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "${min_execs}x" -timeout "$fuzz_timeout" 2>&1)" || {
        printf '%s\n' "$output"
        exit 1
      }
      printf '%s\n' "$output"
      execs="$(printf '%s\n' "$output" | awk '$1 == "fuzz:" && $4 == "execs:" { gsub(",", "", $5); if ($5 + 0 > max) max = $5 + 0 } END { print max + 0 }')"
      if (( execs < min_execs )); then
        echo "${target} executed ${execs} fuzz inputs; want at least ${min_execs}" >&2
        exit 1
      fi
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
    bash scripts/release-rollback-drill.sh >/dev/null
    release_output="${AUTOGIT_RELEASE_OUTPUT:-}"
    cleanup_release_output=0
    if [[ -z "$release_output" ]]; then
      release_output="$(mktemp -d "${TMPDIR:-/tmp}/autogit-release.XXXXXX")"
      cleanup_release_output=1
    fi
    install_drill_root="$(mktemp -d "${TMPDIR:-/tmp}/autogit-install-suite.XXXXXX")"
    trap 'if [[ "$cleanup_release_output" -eq 1 ]]; then rm -rf "$release_output"; fi; rm -rf "$install_drill_root"' EXIT
    bash scripts/release-build.sh --output "$release_output"
    bash scripts/artifact-smoke.sh --directory "$release_output"
    AUTOGIT_VERSION=drill-previous AUTOGIT_COMMIT="$(git rev-parse HEAD)" \
      bash scripts/release-build.sh --target linux/amd64 --output "$install_drill_root/previous" >/dev/null
    AUTOGIT_VERSION=drill-candidate AUTOGIT_COMMIT="$(git rev-parse HEAD)" \
      bash scripts/release-build.sh --target linux/amd64 --output "$install_drill_root/candidate" >/dev/null
    bash scripts/release-install-drill.sh \
      --previous "$install_drill_root/previous/autogit-linux-amd64" \
      --candidate "$install_drill_root/candidate/autogit-linux-amd64" >/dev/null
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
