#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/generate-package-metadata.sh --version TAG --repo OWNER/REPO \
  --directory DIST --output DIRECTORY

Generate deterministic Homebrew and Scoop metadata for an exact AutoGit
release. The release directory must contain all six binaries and SHA256SUMS.
The output directory must not already exist.
EOF
}

version_tag=""
repository=""
release_directory=""
output_directory=""

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --version)
      [[ "$#" -ge 2 ]] || { echo "--version requires TAG" >&2; exit 2; }
      version_tag="$2"
      shift 2
      ;;
    --repo)
      [[ "$#" -ge 2 ]] || { echo "--repo requires OWNER/REPO" >&2; exit 2; }
      repository="$2"
      shift 2
      ;;
    --directory)
      [[ "$#" -ge 2 ]] || { echo "--directory requires DIST" >&2; exit 2; }
      release_directory="$2"
      shift 2
      ;;
    --output)
      [[ "$#" -ge 2 ]] || { echo "--output requires DIRECTORY" >&2; exit 2; }
      output_directory="$2"
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

[[ "$version_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "version must be an exact vMAJOR.MINOR.PATCH tag" >&2
  exit 2
}
[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || {
  echo "repository must be OWNER/REPO" >&2
  exit 2
}
[[ -n "$release_directory" ]] || { echo "--directory is required" >&2; exit 2; }
[[ -n "$output_directory" ]] || { echo "--output is required" >&2; exit 2; }
[[ -d "$release_directory" && ! -L "$release_directory" ]] || {
  echo "release directory is missing or a symlink: $release_directory" >&2
  exit 2
}
[[ ! -e "$output_directory" && ! -L "$output_directory" ]] || {
  echo "package metadata output already exists: $output_directory" >&2
  exit 2
}

release_names=(
  "autogit-linux-amd64"
  "autogit-linux-arm64"
  "autogit-darwin-amd64"
  "autogit-darwin-arm64"
  "autogit-windows-amd64.exe"
  "autogit-windows-arm64.exe"
)

manifest="$release_directory/SHA256SUMS"
[[ -f "$manifest" && ! -L "$manifest" ]] || {
  echo "release checksum manifest is missing or a symlink: $manifest" >&2
  exit 2
}

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

manifest_hash_for() {
  local name=$1
  local count hash
  count="$(awk -v expected="$name" '$2 == expected { count++ } END { print count + 0 }' "$manifest")"
  [[ "$count" == "1" ]] || {
    echo "checksum manifest must contain exactly one entry for $name" >&2
    return 1
  }
  hash="$(awk -v expected="$name" '$2 == expected { print $1 }' "$manifest")"
  [[ "$hash" =~ ^[0-9a-fA-F]{64}$ ]] || {
    echo "checksum manifest has an invalid SHA-256 for $name" >&2
    return 1
  }
  printf '%s\n' "$hash" | tr '[:upper:]' '[:lower:]'
}

for name in "${release_names[@]}"; do
  artifact="$release_directory/$name"
  [[ -f "$artifact" && ! -L "$artifact" && -s "$artifact" ]] || {
    echo "release binary is missing, empty, or a symlink: $artifact" >&2
    exit 2
  }
  expected_hash="$(manifest_hash_for "$name")"
  actual_hash="$(sha256_file "$artifact")"
  [[ "$actual_hash" == "$expected_hash" ]] || {
    echo "release checksum mismatch for $name" >&2
    exit 1
  }
  case "$name" in
    autogit-linux-amd64) linux_amd64_checksum="$expected_hash" ;;
    autogit-linux-arm64) linux_arm64_checksum="$expected_hash" ;;
    autogit-darwin-amd64) darwin_amd64_checksum="$expected_hash" ;;
    autogit-darwin-arm64) darwin_arm64_checksum="$expected_hash" ;;
    autogit-windows-amd64.exe) windows_amd64_checksum="$expected_hash" ;;
    autogit-windows-arm64.exe) windows_arm64_checksum="$expected_hash" ;;
  esac
done

output_parent="$(dirname "$output_directory")"
mkdir -p "$output_parent"
temporary_output="$(mktemp -d "${output_directory}.tmp.XXXXXX")"
cleanup() {
  if [[ -n "$temporary_output" ]]; then
    rm -rf -- "$temporary_output"
  fi
}
trap cleanup EXIT

version="${version_tag#v}"
base_url="https://github.com/$repository/releases/download/$version_tag"

cat > "$temporary_output/autogit.rb" <<EOF
class Autogit < Formula
  desc "Consent-based Git automation tool for AI-assisted development"
  homepage "https://github.com/$repository"
  version "$version"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "$base_url/autogit-darwin-arm64"
      sha256 "$darwin_arm64_checksum"
    else
      url "$base_url/autogit-darwin-amd64"
      sha256 "$darwin_amd64_checksum"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "$base_url/autogit-linux-arm64"
      sha256 "$linux_arm64_checksum"
    else
      url "$base_url/autogit-linux-amd64"
      sha256 "$linux_amd64_checksum"
    end
  end

  def install
    if OS.mac?
      if Hardware::CPU.arm?
        bin.install "autogit-darwin-arm64" => "autogit"
      else
        bin.install "autogit-darwin-amd64" => "autogit"
      end
    elsif OS.linux?
      if Hardware::CPU.arm?
        bin.install "autogit-linux-arm64" => "autogit"
      else
        bin.install "autogit-linux-amd64" => "autogit"
      end
    end
  end
end
EOF

cat > "$temporary_output/autogit.json" <<EOF
{
  "version": "$version",
  "description": "Consent-based Git automation tool for AI-assisted development",
  "homepage": "https://github.com/$repository",
  "license": "Apache-2.0",
  "architecture": {
    "64bit": {
      "url": "$base_url/autogit-windows-amd64.exe",
      "hash": "$windows_amd64_checksum",
      "bin": [["autogit-windows-amd64.exe", "autogit"]]
    },
    "arm64": {
      "url": "$base_url/autogit-windows-arm64.exe",
      "hash": "$windows_arm64_checksum",
      "bin": [["autogit-windows-arm64.exe", "autogit"]]
    }
  }
}
EOF

chmod 0644 "$temporary_output/autogit.rb" "$temporary_output/autogit.json"
if [[ -e "$output_directory" || -L "$output_directory" ]]; then
  echo "package metadata output appeared during generation: $output_directory" >&2
  exit 1
fi
mv "$temporary_output" "$output_directory"
temporary_output=""
