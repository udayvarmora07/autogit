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

is_windows_shell() {
  case "$(uname -s 2>/dev/null || true)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
    *) return 1 ;;
  esac
}

sha256_file() {
  if is_windows_shell; then
    local path_windows powershell
    path_windows="$(cygpath -w "$1")"
    if command -v powershell.exe >/dev/null 2>&1; then
      powershell=powershell.exe
    elif command -v pwsh >/dev/null 2>&1; then
      powershell=pwsh
    else
      echo "PowerShell is required for Windows SHA-256 verification" >&2
      exit 1
    fi
    AUTOGIT_HASH_PATH="$path_windows" "$powershell" -NoLogo -NoProfile -NonInteractive -Command \
      '$ErrorActionPreference = "Stop"; (Get-FileHash -LiteralPath $env:AUTOGIT_HASH_PATH -Algorithm SHA256).Hash.ToLowerInvariant()' | tr -d '\r\n'
    return
  fi
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

copy_windows_binary() {
  local source=$1
  local destination=$2
  local source_windows destination_windows powershell
  command -v cygpath >/dev/null 2>&1 || { echo "cygpath is required for Windows binary installation" >&2; return 1; }
  source_windows="$(cygpath -w "$source")"
  destination_windows="$(cygpath -w "$destination")"
  if command -v powershell.exe >/dev/null 2>&1; then
    powershell=powershell.exe
  elif command -v pwsh >/dev/null 2>&1; then
    powershell=pwsh
  else
    echo "PowerShell is required for Windows binary installation" >&2
    return 1
  fi
  AUTOGIT_COPY_SOURCE="$source_windows" AUTOGIT_COPY_DEST="$destination_windows" \
    "$powershell" -NoLogo -NoProfile -NonInteractive -Command \
    '$ErrorActionPreference = "Stop"; [System.IO.File]::Copy($env:AUTOGIT_COPY_SOURCE, $env:AUTOGIT_COPY_DEST, $false)'
}

move_windows_binary() {
  local source=$1
  local destination=$2
  local backup source_windows destination_windows backup_windows powershell
  backup="$destination.backup"
  [[ ! -e "$backup" && ! -L "$backup" ]] || { echo "stale Windows replacement backup exists" >&2; return 1; }
  command -v cygpath >/dev/null 2>&1 || { echo "cygpath is required for Windows binary installation" >&2; return 1; }
  source_windows="$(cygpath -w "$source")"
  destination_windows="$(cygpath -w "$destination")"
  backup_windows="$(cygpath -w "$backup")"
  if command -v powershell.exe >/dev/null 2>&1; then
    powershell=powershell.exe
  elif command -v pwsh >/dev/null 2>&1; then
    powershell=pwsh
  else
    echo "PowerShell is required for Windows binary installation" >&2
    return 1
  fi
  AUTOGIT_MOVE_SOURCE="$source_windows" AUTOGIT_MOVE_DEST="$destination_windows" AUTOGIT_MOVE_BACKUP="$backup_windows" \
    "$powershell" -NoLogo -NoProfile -NonInteractive -Command \
    '$ErrorActionPreference = "Stop"; if ([System.IO.File]::Exists($env:AUTOGIT_MOVE_DEST)) { [System.IO.File]::Replace($env:AUTOGIT_MOVE_SOURCE, $env:AUTOGIT_MOVE_DEST, $env:AUTOGIT_MOVE_BACKUP, $true); [System.IO.File]::Delete($env:AUTOGIT_MOVE_BACKUP) } else { [System.IO.File]::Move($env:AUTOGIT_MOVE_SOURCE, $env:AUTOGIT_MOVE_DEST) }'
}

install_atomic() {
  local source=$1
  local staged="$bin_dir/autogit.new"
  if is_windows_shell; then
    staged+=".exe"
  fi
  [[ ! -e "$staged" && ! -L "$staged" ]] || { echo "stale staged binary exists" >&2; return 1; }
  if is_windows_shell; then
    copy_windows_binary "$source" "$staged"
    chmod 0755 "$staged" 2>/dev/null || true
    cmp -- "$source" "$staged" || { echo "staged binary differs from source" >&2; return 1; }
    move_windows_binary "$staged" "$installed"
    cmp -- "$source" "$installed" || { echo "installed binary differs from source" >&2; return 1; }
  else
    install -m 0755 "$source" "$staged"
    chmod 0755 "$staged"
    mv "$staged" "$installed"
  fi
}

assert_installed() {
  local expected_version=$1
  local expected_checksum=$2
  local actual_checksum actual_size actual_version
  [[ -f "$installed" && ! -L "$installed" ]] || { echo "installed binary is missing or a symlink" >&2; return 1; }
  actual_checksum="$(sha256_file "$installed")"
  if [[ "$actual_checksum" != "$expected_checksum" ]]; then
    actual_size="$(wc -c < "$installed")"
    echo "installed checksum mismatch: expected=$expected_checksum actual=$actual_checksum bytes=$actual_size" >&2
    return 1
  fi
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
