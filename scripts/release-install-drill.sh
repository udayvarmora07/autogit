#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/release-install-drill.sh --previous PATH --candidate PATH [--output FILE]

Exercise the native raw-binary install contract in a private temporary root.
The previous and candidate binaries must report different version identities.
This is a local/native drill; package-manager and clean-machine acceptance are
separate release requirements.
EOF
}

previous=""
candidate=""
output=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --previous)
      [[ "$#" -ge 2 ]] || { echo "--previous requires PATH" >&2; exit 2; }
      previous="$2"
      shift 2
      ;;
    --candidate)
      [[ "$#" -ge 2 ]] || { echo "--candidate requires PATH" >&2; exit 2; }
      candidate="$2"
      shift 2
      ;;
    --output)
      [[ "$#" -ge 2 ]] || { echo "--output requires FILE" >&2; exit 2; }
      output="$2"
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

[[ -n "$previous" && -n "$candidate" ]] || { echo "both --previous and --candidate are required" >&2; exit 2; }
for artifact in "$previous" "$candidate"; do
  [[ -f "$artifact" && ! -L "$artifact" ]] || { echo "release binary is missing or a symlink: $artifact" >&2; exit 2; }
  [[ -x "$artifact" ]] || { echo "release binary is not executable: $artifact" >&2; exit 2; }
done

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  echo "no SHA-256 command is available" >&2
  exit 1
}

version_of() {
  local artifact=$1
  local line version
  line="$("$artifact" version 2>/dev/null)" || { echo "binary version command failed" >&2; return 1; }
  version="$(printf '%s\n' "$line" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p' | head -n 1)"
  [[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || { echo "binary reported no safe version identity" >&2; return 1; }
  printf '%s\n' "$version"
}

previous_version="$(version_of "$previous")"
candidate_version="$(version_of "$candidate")"
[[ "$previous_version" != "$candidate_version" ]] || { echo "previous and candidate versions must differ" >&2; exit 1; }
previous_checksum="$(sha256_file "$previous")"
candidate_checksum="$(sha256_file "$candidate")"

is_windows_shell() {
  case "$(uname -s 2>/dev/null || true)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
    *) return 1 ;;
  esac
}

root="$(mktemp -d "${TMPDIR:-/tmp}/autogit-install-drill.XXXXXX")"
temporary_output=""
trap 'rm -rf "$root" "$temporary_output"' EXIT
bin_dir="$root/bin"
installed="$bin_dir/autogit"
if is_windows_shell; then
  installed+=".exe"
fi

mkdir "$bin_dir"
if ! is_windows_shell; then
  chmod 0700 "$bin_dir"
fi

install_atomic() {
  local source=$1
  local staged="$bin_dir/autogit.new"
  [[ ! -e "$staged" && ! -L "$staged" ]] || { echo "stale staged binary exists" >&2; return 1; }
  if is_windows_shell; then
    dd if="$source" of="$staged" bs=4M status=none
    chmod 0755 "$staged" 2>/dev/null || true
    cmp -- "$source" "$staged" || { echo "staged binary differs from source" >&2; return 1; }
  else
    install -m 0755 "$source" "$staged"
    chmod 0755 "$staged"
  fi
  mv "$staged" "$installed"
}

assert_installed() {
  local expected_version=$1
  local expected_checksum=$2
  local actual_checksum actual_version
  [[ -f "$installed" && ! -L "$installed" ]] || { echo "installed binary is missing or a symlink" >&2; return 1; }
  actual_checksum="$(sha256_file "$installed")"
  [[ "$actual_checksum" == "$expected_checksum" ]] || { echo "installed checksum mismatch" >&2; return 1; }
  actual_version="$(version_of "$installed")"
  [[ "$actual_version" == "$expected_version" ]] || { echo "installed version mismatch" >&2; return 1; }
}

install_verified() {
  local source=$1
  local expected_version=$2
  local expected_checksum=$3
  local actual_checksum actual_version
  actual_checksum="$(sha256_file "$source")"
  [[ "$actual_checksum" == "$expected_checksum" ]] || { echo "candidate checksum mismatch" >&2; return 1; }
  actual_version="$(version_of "$source")"
  [[ "$actual_version" == "$expected_version" ]] || { echo "candidate version mismatch" >&2; return 1; }
  install_atomic "$source"
  assert_installed "$expected_version" "$expected_checksum"
}

install_verified "$previous" "$previous_version" "$previous_checksum"
initial_install="passed"

install_verified "$candidate" "$candidate_version" "$candidate_checksum"
upgrade="passed"

tampered="$root/tampered"
cp "$candidate" "$tampered"
printf 'tampered' >> "$tampered"
if install_verified "$tampered" "$candidate_version" "$candidate_checksum" 2>/dev/null; then
  echo "tampered candidate was installed" >&2
  exit 1
fi
assert_installed "$candidate_version" "$candidate_checksum"
tampered_upgrade_rejected="passed"

install_verified "$previous" "$previous_version" "$previous_checksum"
downgrade="passed"

if install_verified "$tampered" "$candidate_version" "$candidate_checksum" 2>/dev/null; then
  echo "tampered rollback candidate was installed" >&2
  exit 1
fi
assert_installed "$previous_version" "$previous_checksum"
rollback_preserved_previous="passed"

rm "$installed"
[[ ! -e "$installed" && ! -L "$installed" ]] || { echo "uninstall left the managed binary behind" >&2; exit 1; }
uninstall="passed"

evidence=$(printf '%s\n' \
  '{' \
  '  "schema_version": "autogit.release-install-drill/1",' \
  '  "status": "passed",' \
  '  "sensitive_data_recorded": false,' \
  "  \"previous_version\": \"$previous_version\"," \
  "  \"candidate_version\": \"$candidate_version\"," \
  '  "checks": {' \
  "    \"initial_install\": \"$initial_install\"," \
  "    \"upgrade\": \"$upgrade\"," \
  "    \"tampered_upgrade_rejected\": \"$tampered_upgrade_rejected\"," \
  "    \"downgrade\": \"$downgrade\"," \
  "    \"rollback_preserved_previous\": \"$rollback_preserved_previous\"," \
  "    \"uninstall\": \"$uninstall\"" \
  '  }' \
  '}')

if [[ -n "$output" ]]; then
  [[ ! -e "$output" && ! -L "$output" ]] || { echo "refusing to overwrite evidence output" >&2; exit 2; }
  temporary_output="$(mktemp "${output}.tmp.XXXXXX")"
  printf '%s\n' "$evidence" > "$temporary_output"
  mv "$temporary_output" "$output"
else
  printf '%s\n' "$evidence"
fi
