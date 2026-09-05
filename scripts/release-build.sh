#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/release-build.sh [--target GOOS/GOARCH]... [--output DIR]

Build reproducible AutoGit release binaries. With no --target arguments, the
supported Linux, macOS, and Windows amd64/arm64 binaries are produced.
EOF
}

output_dir="dist"
targets=()

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 ]] || { echo "--target requires GOOS/GOARCH" >&2; exit 2; }
      targets+=("$2")
      shift 2
      ;;
    --output)
      [[ "$#" -ge 2 ]] || { echo "--output requires a directory" >&2; exit 2; }
      output_dir="$2"
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

if [[ "${#targets[@]}" -eq 0 ]]; then
  targets=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
  )
fi

mkdir -p "$output_dir"
artifacts=()

for target in "${targets[@]}"; do
  IFS=/ read -r goos goarch remainder <<<"$target"
  if [[ -n "${remainder:-}" || -z "${goos:-}" || -z "${goarch:-}" ]]; then
    echo "target must be GOOS/GOARCH: $target" >&2
    exit 2
  fi
  case "$goos/$goarch" in
    linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64|windows/arm64) ;;
    *)
      echo "unsupported release target: $target" >&2
      exit 2
      ;;
  esac

  extension=""
  if [[ "$goos" == "windows" ]]; then
    extension=".exe"
  fi
  artifact="$output_dir/autogit-$goos-$goarch$extension"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -buildvcs=false -ldflags='-buildid=' -o "$artifact" ./cmd/autogit
  artifacts+=("$artifact")
done

sha256() {
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

{
  for artifact in "${artifacts[@]}"; do
    basename "$artifact"
  done | LC_ALL=C sort | while IFS= read -r name; do
    printf '%s  %s\n' "$(sha256 "$output_dir/$name")" "$name"
  done
} > "$output_dir/SHA256SUMS"
