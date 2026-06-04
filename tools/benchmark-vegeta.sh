#!/usr/bin/env bash

set -euo pipefail

compose_file="${BENCHMARK_COMPOSE_FILE:-composes/benchmark-check/compose.yaml}"
network="${BENCHMARK_NETWORK:-benchmark-check_default}"
target="${BENCHMARK_TARGET:-proxy}"
url="${BENCHMARK_URL:-http://${target}:8080/api/info}"
host_header="${BENCHMARK_HOST_HEADER:-benchmark.localtest.me}"
rates_csv="${1:-300,600,900,1200}"
duration="${2:-60s}"
results_root="${3:-plan/benchmarks}"
timestamp="$(date +%Y%m%d-%H%M%S)"
results_dir="${results_root}/vegeta-${target}-${timestamp}"
targets_file="$(mktemp /tmp/benchmark-vegeta-targets.XXXX.txt)"
trap 'rm -f "$targets_file"' EXIT

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

write_targets() {
  cat <<EOF >"$targets_file"
GET ${url}
Host: ${host_header}
EOF
}

require_cmd docker
require_cmd mkdir
require_cmd realpath
mkdir -p "$results_dir"
docker compose -f "$compose_file" ps "$target" >/dev/null
write_targets
results_abs="$(realpath "$results_dir")"

echo "vegeta benchmark"
echo "- target: $target"
echo "- compose file: $compose_file"
echo "- network: $network"
echo "- url: $url"
echo "- host header: $host_header"
echo "- duration: $duration"
echo "- rates: $rates_csv"
echo "- results dir: $results_dir"

while IFS= read -r rate; do
  if [[ -z "$rate" ]]; then
    continue
  fi

  attack_file="${results_dir}/vegeta-r${rate}.bin"
  report_file="${results_dir}/vegeta-r${rate}.txt"
  json_file="${results_dir}/vegeta-r${rate}.json"

  echo "running vegeta at ${rate} rps"
  docker run --rm \
    --network "$network" \
    -v "$targets_file:/targets.txt:ro" \
    -v "$results_abs:/results" \
    peterevans/vegeta:latest \
    vegeta \
    attack \
    -duration="$duration" \
    -rate="$rate" \
    -targets=/targets.txt \
    -output="/results/$(basename "$attack_file")"

  docker run --rm \
    -v "$results_abs:/results" \
    peterevans/vegeta:latest \
    vegeta \
    report \
    -type=text \
    "/results/$(basename "$attack_file")" | tee "$report_file"

  docker run --rm \
    -v "$results_abs:/results" \
    peterevans/vegeta:latest \
    vegeta \
    report \
    -type=json \
    "/results/$(basename "$attack_file")" >"$json_file"
done < <(split_csv "$rates_csv")

echo "saved vegeta results to $results_dir"
