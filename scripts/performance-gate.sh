#!/usr/bin/env bash
set -euo pipefail

sample_p95() {
  local package=$1
  local benchmark=$2
  local limit_ns=$3
  local label=$4
  local samples count index p95

  samples="$(go test "$package" -run '^$' -bench "^${benchmark}$" -benchtime=1x -count=20 2>&1 | awk '$4 == "ns/op" { print $3 }' | sort -n)"
  count="$(printf '%s\n' "$samples" | awk 'NF { n++ } END { print n + 0 }')"
  if [[ "$count" -ne 20 ]]; then
    echo "$label produced $count samples, want 20" >&2
    return 1
  fi
  index=$(( (count * 95 + 99) / 100 ))
  p95="$(printf '%s\n' "$samples" | sed -n "${index}p")"
  echo "$label p95=${p95}ns limit=${limit_ns}ns"
  if [[ ! "$p95" =~ ^[0-9]+$ ]] || (( p95 >= limit_ns )); then
    echo "$label exceeded its p95 limit" >&2
    return 1
  fi
}

sample_p95 ./internal/app BenchmarkHookNoCandidate 150000000 no-candidate-hook
sample_p95 ./internal/repository BenchmarkCaptureBaseline100KDeletedPaths 1000000000 baseline-100k-paths
