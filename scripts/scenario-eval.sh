#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/scenario-eval.sh --binary PATH [--trace FILE]

Run deterministic local scenarios across every registered client and grade
final repository/index/ref/state invariants. The output is a redacted
machine-readable evaluation record. --trace retains bounded diagnostics
without raw paths or source content.
EOF
}

binary=""
trace=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --binary)
      [[ "$#" -ge 2 ]] || { echo "--binary requires PATH" >&2; exit 2; }
      binary="$2"
      shift 2
      ;;
    --trace)
      [[ "$#" -ge 2 ]] || { echo "--trace requires FILE" >&2; exit 2; }
      trace="$2"
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
export GIT_CONFIG_GLOBAL=''
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

if ! "$binary" install --list >"$root/adapters.json"; then
  record "adapter-registry-discovery" false
else
  record "adapter-registry-discovery" true
fi

repo_branch="$(git -C "$repo" symbolic-ref --short HEAD)"
repo_index="$(git -C "$repo" hash-object .git/index)"
repo_unchanged() {
  local branch index ref
  branch="$(git -C "$repo" symbolic-ref --short HEAD)"
  index="$(git -C "$repo" hash-object .git/index)"
  ref="$(git -C "$repo" show-ref --verify --quiet refs/autogit/commits/scenario && echo present || true)"
  [[ "$repo_branch" == "$branch" && "$repo_index" == "$index" && -z "$ref" ]]
}

installable_clients=(codex claude-code cursor gemini-cli)
observation_clients=(opencode commandcode)
for client in "${installable_clients[@]}" "${observation_clients[@]}"; do
  expected="false"
  if [[ " ${installable_clients[*]} " == *" $client "* ]]; then
    expected="true"
  fi
  if grep -Eq '"adapter":"'"$client"'","installable":'"$expected" "$root/adapters.json"; then
    record "client-registry-$client" true
  else
    record "client-registry-$client" false
  fi
done

for client in "${installable_clients[@]}"; do
  client_root="$root/client-$client"
  config="$client_root/config.json"
  mkdir -p "$client_root"
  printf '{}\n' >"$config"
  if "$binary" install --adapter "$client" --path "$config" --root "$client_root" >"$root/$client-install.json" 2>"$root/$client-install.err" &&
    grep -Fq "autogit hook --adapter $client" "$config" &&
    first_config_hash="$(git hash-object "$config")" &&
    "$binary" install --adapter "$client" --path "$config" --root "$client_root" >"$root/$client-install-repeat.json" 2>"$root/$client-install-repeat.err" &&
    second_config_hash="$(git hash-object "$config")"; then
    if [[ "$first_config_hash" == "$second_config_hash" ]] && repo_unchanged; then
      record "client-$client-install-idempotent" true
    else
      record "client-$client-install-idempotent" false
    fi
  else
    record "client-$client-install-idempotent" false
  fi
  if "$binary" uninstall --adapter "$client" --path "$config" --root "$client_root" >"$root/$client-uninstall.json" 2>"$root/$client-uninstall.err" &&
    ! grep -Fq 'autogit hook' "$config" && repo_unchanged; then
    record "client-$client-uninstall-restores-state" true
  else
    record "client-$client-uninstall-restores-state" false
  fi
done

for client in "${observation_clients[@]}"; do
  client_root="$root/client-$client"
  config="$client_root/config.json"
  mkdir -p "$client_root"
  if "$binary" install --adapter "$client" --path "$config" --root "$client_root" >"$root/$client-install.json" 2>"$root/$client-install.err"; then
    record "client-$client-observation-only" false
  elif grep -Eq 'E_UNSUPPORTED|unsupported' "$root/$client-install.err" && [[ ! -e "$config" ]] && repo_unchanged; then
    record "client-$client-observation-only" true
  else
    record "client-$client-observation-only" false
  fi
done

printf '{"schema_version":"autogit.eval/1","scenario_count":%d,"passed_count":%d,"passed":%s,"scenarios":[%s]}\n' \
  "$scenario_count" "$passed_count" "$([[ "$scenario_count" -eq "$passed_count" ]] && echo true || echo false)" \
  "$(IFS=,; echo "${recorded[*]}")"

if [[ -n "$trace" ]]; then
  trace_dir="$(dirname -- "$trace")"
  mkdir -p "$trace_dir"
  git_trace_version="$(git --version | awk '{print $3}')"
  printf '{"schema_version":"autogit.scenario-trace/1","platform":"%s/%s","git_version":"%s","scenario_count":%d,"passed_count":%d,"scenarios":[%s]}\n' \
    "$(uname -s | tr '[:upper:]' '[:lower:]')" "$(uname -m)" "$git_trace_version" \
    "$scenario_count" "$passed_count" "$(IFS=,; echo "${recorded[*]}")" >"$trace"
fi

[[ "$scenario_count" -eq "$passed_count" ]]
