#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/artifact-smoke.sh --binary PATH
   or: scripts/artifact-smoke.sh --directory DIR

Run the built AutoGit binary through version, doctor, init, plan, and status
using a disposable local repository and state directory. The host Git version
must satisfy the minimum declared in docs/compatibility-manifest.json.
EOF
}

binary=""
directory=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --binary)
      [[ "$#" -ge 2 ]] || { echo "--binary requires PATH" >&2; exit 2; }
      binary="$2"
      shift 2
      ;;
    --directory)
      [[ "$#" -ge 2 ]] || { echo "--directory requires DIR" >&2; exit 2; }
      directory="$2"
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

if [[ -n "$binary" && -n "$directory" ]] || [[ -z "$binary" && -z "$directory" ]]; then
  echo "provide exactly one of --binary or --directory" >&2
  exit 2
fi

if [[ -n "$directory" ]]; then
  [[ -d "$directory" ]] || { echo "artifact directory is not a directory: $directory" >&2; exit 2; }
  [[ -f "$directory/SHA256SUMS" ]] || { echo "artifact checksums are missing" >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$directory" && sha256sum -c SHA256SUMS)
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$directory" && shasum -a 256 -c SHA256SUMS)
  else
    echo "no SHA-256 verification command is available" >&2
    exit 1
  fi
  host_os="$(go env GOOS)"
  host_arch="$(go env GOARCH)"
  extension=""
  if [[ "$host_os" == "windows" ]]; then
    extension=".exe"
  fi
  binary="$directory/autogit-${host_os}-${host_arch}${extension}"
fi

[[ -f "$binary" ]] || { echo "artifact binary is missing: $binary" >&2; exit 1; }
[[ -x "$binary" || "$(go env GOOS)" == "windows" ]] || { echo "artifact is not executable: $binary" >&2; exit 1; }

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
manifest="${AUTOGIT_COMPATIBILITY_MANIFEST:-$script_dir/../docs/compatibility-manifest.json}"
[[ -f "$manifest" ]] || { echo "compatibility manifest is missing: $manifest" >&2; exit 1; }

# Keep artifact smoke independent of jq. The compatibility manifest is a
# reviewed, repository-owned JSON document and the Git minimum is a scalar on
# its git support-window row.
git_minimum="$(sed -n 's/.*"git"[[:space:]]*:[[:space:]]*{[^}]*"minimum"[[:space:]]*:[[:space:]]*"\([0-9][0-9.]*\)".*/\1/p' "$manifest" | head -n 1)"
[[ "$git_minimum" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "compatibility manifest has no valid Git minimum" >&2
  exit 1
}
git_version_line="$(git --version)"
git_version="$(printf '%s\n' "$git_version_line" | sed -En 's/^git version ([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p')"
[[ "$git_version" =~ ^[0-9]+\.[0-9]+([.][0-9]+)?$ ]] || {
  echo "could not parse system Git version" >&2
  exit 1
}

version_at_least() {
  local got=$1 want=$2
  local got_major got_minor got_patch want_major want_minor want_patch
  IFS=. read -r got_major got_minor got_patch <<<"$got"
  IFS=. read -r want_major want_minor want_patch <<<"$want"
  got_patch="${got_patch:-0}"
  want_patch="${want_patch:-0}"
  (( got_major > want_major ||
     (got_major == want_major && got_minor > want_minor) ||
     (got_major == want_major && got_minor == want_minor && got_patch >= want_patch) ))
}

if ! version_at_least "$git_version" "$git_minimum"; then
  echo "system Git $git_version is below supported minimum $git_minimum" >&2
  exit 1
fi

root="$(mktemp -d "${TMPDIR:-/tmp}/autogit-artifact-smoke.XXXXXX")"
trap 'rm -rf "$root"' EXIT
state="$root/state"
repo="$root/repository"
mkdir -p "$state" "$repo"

export AUTOGIT_STATE_DIR="$state"
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=''
export GIT_TERMINAL_PROMPT=0

version_output="$root/version.json"
doctor_output="$root/doctor.json"
"$binary" version >"$version_output"
grep -Eq '"schema_version":"autogit.result/1"' "$version_output"
grep -Eq '"version":"[^"]+"' "$version_output"
"$binary" doctor >"$doctor_output"
grep -Eq '"schema_version":"autogit.result/1"' "$doctor_output"

"$binary" init --repo "$repo" --local --branch main >"$root/init.json"
git -C "$repo" config user.email autogit-smoke@example.invalid
git -C "$repo" config user.name AutoGit-Smoke
printf '%s\n' '# artifact smoke' >"$repo/README.md"
grep -Eq '"reason_code":"REPOSITORY_INITIALIZED"' "$root/init.json"
git -C "$repo" add -- README.md
"$binary" plan --repo "$repo" >"$root/plan.json"
"$binary" status --repo "$repo" >"$root/status.json"
grep -Eq '"reason_code":"READ_ONLY_PLAN"' "$root/plan.json"
grep -Eq '"reason_code":"STATUS"' "$root/status.json"

echo "artifact smoke passed: $binary (git $(git --version | awk '{print $3}'))"
