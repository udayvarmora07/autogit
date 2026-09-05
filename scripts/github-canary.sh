#!/usr/bin/env bash
set -euo pipefail

: "${AUTOGIT_CANARY_OWNER:?AUTOGIT_CANARY_OWNER is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"

visibility="${AUTOGIT_CANARY_VISIBILITY:-private}"
public_consent="${AUTOGIT_CANARY_PUBLIC_CONSENT:-0}"
run_id="${AUTOGIT_CANARY_RUN_ID:-${GITHUB_RUN_ID:-}}"

if [[ ! "$AUTOGIT_CANARY_OWNER" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$ ]]; then
  echo "canary owner is outside the identity allowlist" >&2
  exit 2
fi
if [[ "$visibility" != private && "$visibility" != public ]]; then
  echo "canary visibility must be private or public" >&2
  exit 2
fi
if [[ "$visibility" == public && "$public_consent" != 1 ]]; then
  echo "public canary requires AUTOGIT_CANARY_PUBLIC_CONSENT=1" >&2
  exit 2
fi
if [[ ! "$run_id" =~ ^[0-9]+$ ]]; then
  echo "canary run id must be numeric" >&2
  exit 2
fi

name="autogit-v1-test-${run_id}"
if [[ ! "$name" =~ ^autogit-v1-test-[0-9]+$ ]]; then
  echo "generated canary name is outside the cleanup allowlist" >&2
  exit 2
fi

cleanup() {
  local test_status=$1
  local cleanup_status=0
  local resource="repos/${AUTOGIT_CANARY_OWNER}/${name}"
  local response

  if response="$(gh api "$resource" 2>&1)"; then
    if ! gh repo delete "${AUTOGIT_CANARY_OWNER}/${name}" --yes; then
      echo "canary cleanup failed for the exact allowlisted repository" >&2
      cleanup_status=1
    fi
  elif [[ "$response" != *404* && "$response" != *"Not Found"* ]]; then
    echo "could not determine whether the exact canary repository exists" >&2
    cleanup_status=1
  fi
  if [[ "$cleanup_status" -ne 0 ]]; then
    return "$cleanup_status"
  fi
  return "$test_status"
}

trap 'status=$?; trap - EXIT; cleanup "$status"' EXIT

export AUTOGIT_GITHUB_CANARY=1
export AUTOGIT_CANARY_NAME="$name"
go test -tags github_canary ./internal/provider -run '^TestGitHubCanary$' -count=1 -timeout=10m
