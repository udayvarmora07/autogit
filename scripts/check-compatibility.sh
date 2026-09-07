#!/usr/bin/env bash
set -euo pipefail

go test . -run '^TestReleaseCompatibilityManifestMatchesSupportedContracts$' -count=1

issue_file="${AUTOGIT_COMPATIBILITY_ISSUE_FILE:-${TMPDIR:-/tmp}/autogit-compatibility-expiry.md}"
rm -f "$issue_file"
set +e
go run ./cmd/autogit-compat -manifest docs/compatibility-manifest.json --issue-body "$issue_file"
status=$?
set -e
if [[ "$status" -ne 0 && "$status" -ne 10 ]]; then
  exit "$status"
fi
if [[ -f "$issue_file" ]]; then
  echo "compatibility review issue body: $issue_file"
  if [[ "${AUTOGIT_CREATE_COMPATIBILITY_ISSUE:-0}" == "1" ]]; then
    title="Compatibility window expiry review"
    existing="$(gh issue list --state open --search \"$title in:title\" --limit 1 --json number --jq 'length')"
    if [[ "$existing" == "0" ]]; then
      gh issue create --title "$title" --body-file "$issue_file"
    fi
  fi
fi
exit "$status"
