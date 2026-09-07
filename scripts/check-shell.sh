#!/usr/bin/env bash
set -euo pipefail

scripts=(scripts/*.sh)
if [[ "${#scripts[@]}" -eq 0 ]]; then
  echo "no shell scripts found" >&2
  exit 1
fi

bash -n "${scripts[@]}"
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck --shell=bash --severity=warning "${scripts[@]}"
elif [[ "${CI:-}" == "true" || "${AUTOGIT_REQUIRE_SHELLCHECK:-0}" == "1" ]]; then
  echo "shellcheck is required in CI" >&2
  exit 1
else
  echo "shellcheck unavailable; bash syntax checks passed" >&2
fi
