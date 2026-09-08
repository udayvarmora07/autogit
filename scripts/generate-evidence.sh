#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/generate-evidence.sh [--output FILE] [--allow-dirty]
       [--tag TAG] [--require-tag] [--artifact PATH]...
       [--full] [--canary]

Run the safe local evidence suites and publish a redacted machine-readable
manifest. Release evidence requires a clean exact commit; --allow-dirty is
only for local development records.
EOF
}

output="docs/release-evidence/phase-4-quality.json"
allow_dirty=0
expected_tag=""
require_tag=0
artifacts=()
full=0
canary=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --output)
      [[ "$#" -ge 2 ]] || { echo "--output requires FILE" >&2; exit 2; }
      output="$2"
      shift 2
      ;;
    --allow-dirty)
      allow_dirty=1
      shift
      ;;
    --tag)
      [[ "$#" -ge 2 ]] || { echo "--tag requires TAG" >&2; exit 2; }
      expected_tag="$2"
      shift 2
      ;;
    --require-tag)
      require_tag=1
      shift
      ;;
    --artifact)
      [[ "$#" -ge 2 ]] || { echo "--artifact requires PATH" >&2; exit 2; }
      artifacts+=("$2")
      shift 2
      ;;
    --full)
      full=1
      shift
      ;;
    --canary)
      canary=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

results_dir="$(mktemp -d "${TMPDIR:-/tmp}/autogit-evidence.XXXXXX")"
trap 'rm -rf "$results_dir"' EXIT
status=0
run_suite() {
  local name=$1
  shift
  if "$@" >"$results_dir/$name.log" 2>&1; then
    printf 'passed\n' >"$results_dir/$name.status"
  else
    printf 'failed\n' >"$results_dir/$name.status"
    status=1
    sed -n '1,80p' "$results_dir/$name.log" >&2
  fi
}

run_suite presubmit bash scripts/test-suites.sh presubmit
run_suite shell bash scripts/check-shell.sh
run_suite compatibility bash scripts/check-compatibility.sh
scenario_binary="$results_dir/autogit"
if go build -trimpath -o "$scenario_binary" ./cmd/autogit; then
  run_suite scenario bash scripts/scenario-eval.sh --binary "$scenario_binary"
else
  printf 'failed\n' >"$results_dir/scenario.status"
  status=1
  echo "could not build the scenario binary" >&2
fi

if [[ "$full" -eq 1 ]]; then
  run_suite core bash scripts/test-suites.sh core
  run_suite integration bash scripts/test-suites.sh integration
  run_suite race bash scripts/test-suites.sh race
  run_suite soak bash scripts/test-suites.sh soak
  run_suite fuzz bash scripts/test-suites.sh fuzz
  release_dir="$results_dir/release"
  AUTOGIT_RELEASE_OUTPUT="$release_dir" run_suite release bash scripts/test-suites.sh release
  if [[ -d "$release_dir" ]]; then
    while IFS= read -r artifact; do
      artifacts+=("$artifact")
    done < <(find "$release_dir" -maxdepth 1 -type f -name 'autogit-*' -print | LC_ALL=C sort)
  fi
fi

if [[ "$canary" -eq 1 ]]; then
  run_suite canary bash scripts/test-suites.sh canary
fi

if [[ "$status" -ne 0 ]]; then
  exit "$status"
fi

args=(--results-dir "$results_dir" --output "$output")
if [[ "$allow_dirty" -eq 1 ]]; then
  args+=(--allow-dirty)
fi
if [[ -n "$expected_tag" ]]; then
  args+=(--tag "$expected_tag")
fi
if [[ "$require_tag" -eq 1 ]]; then
  args+=(--require-tag)
fi
for artifact in "${artifacts[@]}"; do
  args+=(--artifact "$artifact")
done
go run ./cmd/autogit-evidence "${args[@]}"
