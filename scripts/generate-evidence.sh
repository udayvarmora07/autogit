#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/generate-evidence.sh [--output FILE] [--allow-dirty]

Run the safe local evidence suites and publish a redacted machine-readable
manifest. Release evidence requires a clean exact commit; --allow-dirty is
only for local development records.
EOF
}

output="docs/release-evidence/phase-4-quality.json"
allow_dirty=0
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

if [[ "$status" -ne 0 ]]; then
  exit "$status"
fi

args=(--results-dir "$results_dir" --output "$output")
if [[ "$allow_dirty" -eq 1 ]]; then
  args+=(--allow-dirty)
fi
go run ./cmd/autogit-evidence "${args[@]}"
