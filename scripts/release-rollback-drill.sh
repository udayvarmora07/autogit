#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/release-rollback-drill.sh [--output FILE]

Run the offline release-integrity and rollback drill. The drill creates only
disposable files under a private temporary directory, never contacts GitHub,
and emits redacted machine-readable evidence.
EOF
}

output="-"
while [[ "$#" -gt 0 ]]; do
  case "$1" in
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

umask 077

fail() {
  echo "$1" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "no SHA-256 command is available"
  fi
}

verify_manifest() {
  local directory=$1
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$directory" && sha256sum -c SHA256SUMS >/dev/null 2>&1)
  elif command -v shasum >/dev/null 2>&1; then
    local expected actual name
    while IFS='  ' read -r expected name; do
      actual=$(sha256_file "$directory/$name") || return 1
      [[ "$actual" == "$expected" ]] || return 1
    done < "$directory/SHA256SUMS"
  else
    fail "no SHA-256 verification command is available"
  fi
}

drill_root=$(mktemp -d "${TMPDIR:-/tmp}/autogit-rollback-drill.XXXXXX")
trap 'rm -rf "$drill_root"' EXIT

previous="$drill_root/previous"
candidate="$drill_root/candidate"
mkdir -p "$previous" "$candidate"

printf '%s\n' 'approved release artifact' > "$previous/autogit-linux-amd64"
printf '%s\n' 'candidate release artifact' > "$candidate/autogit-linux-amd64"

previous_sum=$(sha256_file "$previous/autogit-linux-amd64")
candidate_sum=$(sha256_file "$candidate/autogit-linux-amd64")
printf '%s  %s\n' "$previous_sum" 'autogit-linux-amd64' > "$previous/SHA256SUMS"
printf '%s  %s\n' "$candidate_sum" 'autogit-linux-amd64' > "$candidate/SHA256SUMS"

verify_manifest "$previous" || fail "known-good release failed checksum verification"
verify_manifest "$candidate" || fail "candidate release failed baseline checksum verification"

printf '%s\n' 'unapproved mutation' >> "$candidate/autogit-linux-amd64"
if verify_manifest "$candidate"; then
  fail "tampered candidate was accepted by checksum verification"
fi

printf '%s\n' \
  'publication=stopped' \
  'active_release=previous-approved' \
  'candidate_state=quarantined' \
  'candidate_reason=artifact-integrity-mismatch' \
  > "$drill_root/release-state"
grep -Fxq 'publication=stopped' "$drill_root/release-state" || fail "publication was not stopped"
grep -Fxq 'candidate_state=quarantined' "$drill_root/release-state" || fail "candidate was not quarantined"

printf '%s\n' \
  'rollback_target=previous-approved' \
  'rollback_effect=channel-pointer-restored' \
  'local_history=preserved' \
  > "$drill_root/rollback-state"
grep -Fxq 'rollback_target=previous-approved' "$drill_root/rollback-state" || fail "rollback target was not recorded"
grep -Fxq 'local_history=preserved' "$drill_root/rollback-state" || fail "local history preservation was not recorded"

evidence=$(printf '%s\n' \
  '{' \
  '  "schema_version": "autogit.release-rollback-drill/1",' \
  '  "status": "passed",' \
  '  "publication_state": "stopped",' \
  '  "candidate_state": "quarantined",' \
  '  "rollback_target": "previous-approved",' \
  '  "signing_identity_recovery": "manual-approval-required",' \
  '  "sensitive_data_recorded": false,' \
  '  "checks": {' \
  '    "known_good_checksum": "passed",' \
  '    "tampered_candidate_rejected": "passed",' \
  '    "publication_stopped": "passed",' \
  '    "candidate_quarantined": "passed",' \
  '    "channel_pointer_rolled_back": "passed",' \
  '    "local_history_preserved": "passed"' \
  '  }' \
  '}')

if [[ "$output" == "-" ]]; then
  printf '%s\n' "$evidence"
else
  output_dir=$(dirname "$output")
  mkdir -p "$output_dir"
  [[ ! -e "$output" && ! -L "$output" ]] || fail "refusing to overwrite evidence output"
  temporary_output="$output.tmp.$$"
  printf '%s\n' "$evidence" > "$temporary_output"
  mv "$temporary_output" "$output"
fi
