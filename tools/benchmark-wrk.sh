#!/usr/bin/env bash

set -euo pipefail

compose_file="${BENCHMARK_COMPOSE_FILE:-composes/benchmark-check/compose.yaml}"
network="${BENCHMARK_NETWORK:-benchmark-check_default}"
target="${BENCHMARK_TARGET:-proxy}"
url="${BENCHMARK_URL:-http://${target}:8080/api/info}"
host_header="${BENCHMARK_HOST_HEADER:-benchmark.localtest.me}"
duration="${1:-30s}"
threads="${2:-4}"
connections_csv="${3:-50,100,200,400}"
results_root="${4:-plan/benchmarks}"
timestamp="$(date +%Y%m%d-%H%M%S)"
results_dir="${results_root}/wrk-${target}-${timestamp}"

require_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return
  fi
  echo "FAIL: required command not found: $1" >&2
  exit 1
}

split_csv() {
  printf '%s\n' "$1" | tr ',' '\n'
}

require_cmd docker
require_cmd mkdir

mkdir -p "$results_dir"
docker compose -f "$compose_file" ps "$target" >/dev/null

echo "wrk benchmark"
echo "- target: $target"
echo "- compose file: $compose_file"
echo "- network: $network"
echo "- url: $url"
echo "- host header: $host_header"
echo "- duration: $duration"
echo "- threads: $threads"
echo "- connections: $connections_csv"
echo "- results dir: $results_dir"

while IFS= read -r connections; do
  if [[ -z "$connections" ]]; then
    continue
  fi

  output_file="${results_dir}/wrk-c${connections}.txt"
  echo "running wrk with ${connections} connections"
  docker run --rm \
    --network "$network" \
    williamyeh/wrk \
    -t"$threads" \
    -c"$connections" \
    -d"$duration" \
    -H "Host: ${host_header}" \
    "$url" | tee "$output_file"
done < <(split_csv "$connections_csv")

echo "saved wrk results to $results_dir"
