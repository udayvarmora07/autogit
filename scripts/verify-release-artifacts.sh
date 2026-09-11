#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/verify-release-artifacts.sh --directory DIR --repo OWNER/REPO \
  --tag vMAJOR.MINOR.PATCH --commit SHA

Verify a downloaded AutoGit release bundle's checksums and GitHub provenance.
The verifier requires the exact repository, release workflow, tag, and source
commit so an artifact from a different build cannot satisfy the check.
EOF
}

directory=""
repo=""
tag=""
commit=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --directory)
      [[ "$#" -ge 2 ]] || { echo "--directory requires DIR" >&2; exit 2; }
      directory="$2"
      shift 2
      ;;
    --repo)
      [[ "$#" -ge 2 ]] || { echo "--repo requires OWNER/REPO" >&2; exit 2; }
      repo="$2"
      shift 2
      ;;
    --tag)
      [[ "$#" -ge 2 ]] || { echo "--tag requires TAG" >&2; exit 2; }
      tag="$2"
      shift 2
      ;;
    --commit)
      [[ "$#" -ge 2 ]] || { echo "--commit requires SHA" >&2; exit 2; }
      commit="$2"
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

[[ -d "$directory" && ! -L "$directory" ]] || { echo "release directory is missing or a symlink: $directory" >&2; exit 2; }
[[ -f "$directory/SHA256SUMS" && ! -L "$directory/SHA256SUMS" ]] || { echo "release checksum manifest is missing" >&2; exit 1; }
[[ "$repo" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$ ]] || { echo "repository must be OWNER/REPO" >&2; exit 2; }
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "tag must be vMAJOR.MINOR.PATCH" >&2; exit 2; }
[[ "$commit" =~ ^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$ ]] || { echo "commit must be a full Git SHA" >&2; exit 2; }

release_binaries=(
  autogit-linux-amd64
  autogit-linux-arm64
  autogit-darwin-amd64
  autogit-darwin-arm64
  autogit-windows-amd64.exe
  autogit-windows-arm64.exe
)
expected_names=("${release_binaries[@]}" autogit.spdx.json autogit.cyclonedx.json autogit.release-evidence.json)
for binary in "${release_binaries[@]}"; do
  expected_names+=("$binary.govulncheck.txt")
done

is_expected_name() {
  local candidate=$1
  local name
  for name in "${expected_names[@]}"; do
    if [[ "$name" == "$candidate" ]]; then
      return 0
    fi
  done
  return 1
}

# A downloaded release directory is a closed bundle. Do not let a consumer
# silently verify the expected files while an unlisted file or directory is
# carried alongside them. The explicit glob forms include dotfiles without
# relying on Bash 4 features or non-portable find predicates.
for entry in "$directory"/* "$directory"/.[!.]* "$directory"/..?*; do
  [[ -e "$entry" || -L "$entry" ]] || continue
  name=${entry##*/}
  if [[ "$name" != "SHA256SUMS" ]] && ! is_expected_name "$name"; then
    echo "release directory contains an unexpected file: $name" >&2
    exit 1
  fi
done

manifest_lines=0
seen_names=()
manifest_pattern='^([0-9a-f]{64})  ([^[:space:]]+)$'
while IFS= read -r line || [[ -n "$line" ]]; do
  manifest_lines=$((manifest_lines + 1))
  if [[ ! "$line" =~ $manifest_pattern ]]; then
    echo "checksum manifest contains an invalid line" >&2
    exit 1
  fi
  name="${BASH_REMATCH[2]}"
  if ! is_expected_name "$name"; then
    echo "checksum manifest contains an unexpected file: $name" >&2
    exit 1
  fi
  seen_names+=("$name")
done < "$directory/SHA256SUMS"

if [[ "$manifest_lines" -ne "${#expected_names[@]}" ]]; then
  echo "checksum manifest contains $manifest_lines files; want ${#expected_names[@]}" >&2
  exit 1
fi
for name in "${expected_names[@]}"; do
  seen_count=0
  for seen_name in "${seen_names[@]}"; do
    if [[ "$seen_name" == "$name" ]]; then
      seen_count=$((seen_count + 1))
    fi
  done
  if [[ "$seen_count" -ne 1 || ! -f "$directory/$name" || -L "$directory/$name" ]]; then
    echo "release evidence file is missing, duplicated, or a symlink: $name" >&2
    exit 1
  fi
done

verify_checksums() {
  if command -v sha256sum >/dev/null 2>&1; then
    (cd -- "$directory" && sha256sum -c SHA256SUMS)
  elif command -v shasum >/dev/null 2>&1; then
    (cd -- "$directory" && shasum -a 256 -c SHA256SUMS)
  else
    echo "no SHA-256 verification command is available" >&2
    return 1
  fi
}
if ! verify_checksums; then
  echo "release checksum verification failed" >&2
  exit 1
fi

command -v gh >/dev/null 2>&1 || { echo "GitHub CLI is required for provenance verification" >&2; exit 2; }
if ! gh attestation verify --help >/dev/null 2>&1; then
  echo "installed GitHub CLI has no attestation verifier" >&2
  exit 2
fi

signer_workflow="$repo/.github/workflows/release.yml"
attestation_args=(
  --repo "$repo"
  --signer-workflow "$signer_workflow"
  --source-ref "refs/tags/$tag"
  --source-digest "$commit"
  --deny-self-hosted-runners
)
for name in "${release_binaries[@]}"; do
  gh attestation verify "$directory/$name" \
    "${attestation_args[@]}" \
    --predicate-type 'https://slsa.dev/provenance/v1'
done
for predicate_type in 'https://spdx.dev/Document/v2.3' 'https://cyclonedx.org/bom'; do
  gh attestation verify "$directory/${release_binaries[0]}" \
    "${attestation_args[@]}" \
    --predicate-type "$predicate_type"
done

echo "release checksums and provenance verified: $repo $tag $commit"
