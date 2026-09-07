#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/scenario-eval.sh --binary PATH

Run deterministic local scenarios and grade final repository/index/ref/state
invariants. The output is a redacted machine-readable evaluation record.
EOF
}

binary=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --binary)
      [[ "$#" -ge 2 ]] || { echo "--binary requires PATH" >&2; exit 2; }
      binary="$2"
      shift 2
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

[[ -n "$binary" && -f "$binary" ]] || { echo "a built --binary is required" >&2; exit 2; }

root="$(mktemp -d "${TMPDIR:-/tmp}/autogit-scenario.XXXXXX")"
trap 'rm -rf "$root"' EXIT
state="$root/state"
repo="$root/repository"
mkdir -p "$state" "$repo"
export AUTOGIT_STATE_DIR="$state"
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_TERMINAL_PROMPT=0

scenario_count=0
passed_count=0
recorded=()
record() {
  local id=$1
  local passed=$2
  scenario_count=$((scenario_count + 1))
  if [[ "$passed" == true ]]; then
    passed_count=$((passed_count + 1))
  fi
  recorded+=("{\"id\":\"$id\",\"passed\":$passed}")
}

if "$binary" init --repo "$repo" >"$root/decline.out" 2>"$root/decline.err"; then
  record "declined-init-does-not-mutate" false
else
  if grep -Eq 'E_CONSENT|consent' "$root/decline.err" && [[ -z "$(find "$state" -mindepth 1 -print -quit)" ]]; then
    record "declined-init-does-not-mutate" true
  else
    record "declined-init-does-not-mutate" false
  fi
fi

if "$binary" init --repo "$repo" --local --branch main >"$root/init.json" && grep -Eq 'REPOSITORY_INITIALIZED' "$root/init.json"; then
  record "explicit-local-consent-initializes" true
else
  record "explicit-local-consent-initializes" false
fi

git -C "$repo" config user.email autogit-scenario@example.invalid
git -C "$repo" config user.name AutoGit-Scenario
printf '%s\n' '# scenario' >"$repo/README.md"
git -C "$repo" add -- README.md
initial_branch="$(git -C "$repo" symbolic-ref --short HEAD)"
initial_index="$(git -C "$repo" hash-object .git/index)"
if "$binary" plan --repo "$repo" >"$root/plan.json" && "$binary" status --repo "$repo" >"$root/status.json"; then
  final_branch="$(git -C "$repo" symbolic-ref --short HEAD)"
  final_index="$(git -C "$repo" hash-object .git/index)"
  if [[ "$initial_branch" == "$final_branch" && "$initial_index" == "$final_index" && -z "$(git -C "$repo" show-ref --verify --quiet refs/autogit/commits/scenario && echo present)" ]]; then
    record "read-only-inspection-preserves-final-state" true
  else
    record "read-only-inspection-preserves-final-state" false
  fi
else
  record "read-only-inspection-preserves-final-state" false
fi

printf '{"schema_version":"autogit.eval/1","scenario_count":%d,"passed_count":%d,"passed":%s,"scenarios":[%s]}\n' \
  "$scenario_count" "$passed_count" "$([[ "$scenario_count" -eq "$passed_count" ]] && echo true || echo false)" \
  "$(IFS=,; echo "${recorded[*]}")"

[[ "$scenario_count" -eq "$passed_count" ]]
