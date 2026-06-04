#!/usr/bin/env bash

set -euo pipefail

compose_file="${BENCHMARK_COMPOSE_FILE:-composes/benchmark-check/compose.yaml}"
target="${BENCHMARK_TARGET:-proxy}"
duration_seconds="${1:-60}"
interval_seconds="${2:-2}"
results_root="${3:-plan/benchmarks}"
timestamp="$(date +%Y%m%d-%H%M%S)"
results_dir="${results_root}/stats-${target}-${timestamp}"
output_file="${results_dir}/${target}-stats.csv"

require_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return
  fi
  echo "FAIL: required command not found: $1" >&2
  exit 1
}

require_cmd docker
require_cmd mkdir

if ! [[ "$duration_seconds" =~ ^[0-9]+$ ]] || ((duration_seconds <= 0)); then
  echo "FAIL: duration seconds must be a positive integer" >&2
  exit 1
fi

if ! [[ "$interval_seconds" =~ ^[0-9]+$ ]] || ((interval_seconds <= 0)); then
  echo "FAIL: interval seconds must be a positive integer" >&2
  exit 1
fi

container_id="$(docker compose -f "$compose_file" ps -q "$target")"
if [[ -z "$container_id" ]]; then
  echo "FAIL: target container is not running: $target" >&2
  exit 1
fi

mkdir -p "$results_dir"
echo "timestamp,cpu_percent,mem_usage,mem_percent,pids" >"$output_file"

end_time=$((SECONDS + duration_seconds))

while ((SECONDS < end_time)); do
  stats_line="$(docker stats --no-stream --format '{{.CPUPerc}},{{.MemUsage}},{{.MemPerc}},{{.PIDs}}' "$container_id")"
  printf '%s,%s\n' "$(date -Iseconds)" "$stats_line" >>"$output_file"
  sleep "$interval_seconds"
done

echo "saved stats to $output_file"
