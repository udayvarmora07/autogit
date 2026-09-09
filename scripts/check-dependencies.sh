#!/usr/bin/env bash
set -euo pipefail

go mod verify
go list -m all >/dev/null

forbidden_dependency_reference=false
for manifest in go.mod go.sum; do
  if grep -nI '@latest' "$manifest"; then
    forbidden_dependency_reference=true
  fi
done
while IFS= read -r repository_file; do
  if grep -nI '@latest' "$repository_file"; then
    forbidden_dependency_reference=true
  fi
done < <(find .github -type f ! -name '*.json' -print)
if grep -nE '^replace[[:space:]]' go.mod; then
  forbidden_dependency_reference=true
fi
if [[ "$forbidden_dependency_reference" == true ]]; then
  echo "floating or replacement dependencies are not allowed" >&2
  exit 1
fi

action_refs="$(
  while IFS= read -r workflow_file; do
    grep -oE 'uses: [^@[:space:]]+@[[:alnum:]_.-]+' "$workflow_file" || true
  done < <(find .github -type f \( -name '*.yml' -o -name '*.yaml' \) -print)
)"
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
