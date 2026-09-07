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

if [[ -e "$output_dir" && ! -d "$output_dir" ]]; then
  echo "release output path is not a directory: $output_dir" >&2
  exit 2
fi
if [[ -d "$output_dir" ]]; then
  existing_entry="$(find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit)"
  if [[ -n "$existing_entry" ]]; then
    echo "output directory must be empty: $output_dir" >&2
    exit 2
  fi
else
  mkdir -p "$output_dir"
fi

build_version="${AUTOGIT_VERSION:-}"
if [[ -z "$build_version" ]]; then
  build_version="$(git describe --tags --always --match 'v[0-9]*' 2>/dev/null || true)"
fi
if [[ -z "$build_version" ]]; then
  build_version="dev"
fi
build_commit="${AUTOGIT_COMMIT:-}"
if [[ -z "$build_commit" ]]; then
  build_commit="$(git rev-parse --verify HEAD 2>/dev/null || true)"
fi
if [[ -z "$build_commit" ]]; then
  build_commit="unknown"
fi
build_date="${AUTOGIT_BUILD_DATE:-}"
if [[ -z "$build_date" && -n "${SOURCE_DATE_EPOCH:-}" ]]; then
  if [[ ! "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]]; then
    echo "SOURCE_DATE_EPOCH must be an unsigned integer" >&2
    exit 2
  fi
  if build_date="$(date -u -d "@$SOURCE_DATE_EPOCH" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null)"; then
    :
  elif build_date="$(date -u -r "$SOURCE_DATE_EPOCH" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null)"; then
    :
  else
    echo "cannot format SOURCE_DATE_EPOCH" >&2
    exit 2
  fi
fi
if [[ -z "$build_date" ]]; then
  build_date="unknown"
fi
build_compatibility="${AUTOGIT_COMPATIBILITY:-autogit.compatibility/1}"
for identity in "$build_version" "$build_commit" "$build_date" "$build_compatibility"; do
  if [[ "$identity" == *$'\n'* || "$identity" == *$'\r'* || "$identity" == *' '* ]]; then
    echo "release identity contains unsupported whitespace" >&2
    exit 2
  fi
done

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
  ldflags="-buildid= -X main.buildVersion=$build_version -X main.buildCommit=$build_commit -X main.buildDate=$build_date -X main.buildCompatibility=$build_compatibility"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$artifact" ./cmd/autogit
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
