#!/usr/bin/env bash
set -euo pipefail

performance_output="${AUTOGIT_PERF_OUTPUT:-}"
performance_retries="${AUTOGIT_PERF_RETRIES:-2}"
if [[ ! "$performance_retries" =~ ^[1-9][0-9]*$ ]]; then
  echo "AUTOGIT_PERF_RETRIES must be a positive integer" >&2
  exit 2
fi
if [[ -n "$performance_output" ]]; then
  : > "$performance_output"
fi

sample_p95() {
  local package=$1
  local benchmark=$2
  local limit_ns=$3
  local label=$4
  local samples count p50_index p95_index p99_index p50 p95 p99 attempt

  for ((attempt = 1; attempt <= performance_retries; attempt++)); do
    # Amortize scheduler and process-start noise so the sample measures the
    # steady-state operation described by the budget, not one cold invocation.
    samples="$(go test "$package" -run '^$' -bench "^${benchmark}$" -benchtime=100ms -count=20 2>&1 | awk '$4 == "ns/op" { print $3 }' | sort -n)"
    count="$(printf '%s\n' "$samples" | awk 'NF { n++ } END { print n + 0 }')"
    if [[ "$count" -ne 20 ]]; then
      echo "$label produced $count samples, want 20" >&2
      return 1
    fi
    p50_index=$(( (count * 50 + 99) / 100 ))
    p95_index=$(( (count * 95 + 99) / 100 ))
    p99_index=$(( (count * 99 + 99) / 100 ))
    p50="$(printf '%s\n' "$samples" | sed -n "${p50_index}p")"
    p95="$(printf '%s\n' "$samples" | sed -n "${p95_index}p")"
    p99="$(printf '%s\n' "$samples" | sed -n "${p99_index}p")"
    echo "$label p50=${p50}ns p95=${p95}ns p99=${p99}ns limit=${limit_ns}ns"
    if [[ "$p95" =~ ^[0-9]+$ ]] && (( p95 < limit_ns )); then
      if [[ -n "$performance_output" ]]; then
        printf '%s\t%s\t%s\t%s\t%s\n' "$label" "$p50" "$p95" "$p99" "$limit_ns" >> "$performance_output"
      fi
      return 0
    fi
    if (( attempt < performance_retries )); then
      echo "$label exceeded its p95 limit; retrying (${attempt}/${performance_retries})" >&2
    fi
  done
  echo "$label exceeded its p95 limit" >&2
  return 1
}

sample_p95 ./internal/app BenchmarkHookNoCandidate 150000000 no-candidate-hook
sample_p95 ./internal/repository BenchmarkCaptureBaseline1KDeletedPaths 100000000 baseline-1k-paths
sample_p95 ./internal/repository BenchmarkCaptureBaseline100KDeletedPaths 1000000000 baseline-100k-paths
sample_p95 ./internal/db BenchmarkStateInspect 150000000 state-inspect
sample_p95 ./internal/verification BenchmarkTrustedVerificationBoundary 100000000 verifier-boundary
sample_p95 ./internal/provider BenchmarkFakeProviderPush 10000000 provider-push
sample_p95 ./cmd/autogit BenchmarkCLIStartupVersion 50000000 cli-startup-version
