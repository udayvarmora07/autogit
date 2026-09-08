#!/usr/bin/env bash
set -euo pipefail

go mod verify
go list -m all >/dev/null

if rg -n '@latest' go.mod go.sum .github --glob '!*.json' || rg -n '^replace[[:space:]]' go.mod; then
  echo "floating or replacement dependencies are not allowed" >&2
  exit 1
fi

action_refs="$(rg -o 'uses: [^@[:space:]]+@[[:alnum:]_.-]+' .github --glob '*.yml' --glob '*.yaml' || true)"
if [[ -z "$action_refs" ]]; then
  echo "no GitHub Actions were found" >&2
  exit 1
fi
while IFS= read -r action; do
  ref="${action##*@}"
  if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
    echo "GitHub Action is not pinned to a full SHA: $action" >&2
    exit 1
  fi
done <<<"$action_refs"

test -f LICENSE
test -f SECURITY.md
test -f CONTRIBUTING.md
test -f CODE_OF_CONDUCT.md
test -f .github/CODEOWNERS
test -f docs/support-policy.md
test -f CHANGELOG.md
test -f docs/dependency-policy.md
test -f docs/dependency-licenses.json
echo "dependency and workflow policy checks passed"
