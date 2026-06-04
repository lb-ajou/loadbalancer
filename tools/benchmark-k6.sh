#!/usr/bin/env bash

set -euo pipefail

compose_file="${BENCHMARK_COMPOSE_FILE:-composes/benchmark-check/compose.yaml}"
network="${BENCHMARK_NETWORK:-benchmark-check_default}"
target="${BENCHMARK_TARGET:-proxy}"
url="${BENCHMARK_URL:-http://${target}:8080/api/info}"
host_header="${BENCHMARK_HOST_HEADER:-benchmark.localtest.me}"
ramp_up="${1:-30s}"
steady="${2:-60s}"
spike="${3:-30s}"
peak_rate="${4:-1200}"
results_root="${5:-plan/benchmarks}"
timestamp="$(date +%Y%m%d-%H%M%S)"
results_dir="${results_root}/k6-${target}-${timestamp}"
script_file="$(mktemp /tmp/benchmark-k6-script.XXXX.js)"
trap 'rm -f "$script_file"' EXIT

require_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return
  fi
  echo "FAIL: required command not found: $1" >&2
  exit 1
}

write_script() {
  cat <<'EOF' >"$script_file"
import http from 'k6/http';
import { check } from 'k6';

const targetUrl = __ENV.BENCHMARK_URL;
const hostHeader = __ENV.BENCHMARK_HOST_HEADER;
const steadyRate = Number(__ENV.STEADY_RATE || '0');
const peakRate = Number(__ENV.PEAK_RATE || '0');
const rampUp = __ENV.RAMP_UP || '30s';
const steady = __ENV.STEADY || '60s';
const spike = __ENV.SPIKE || '30s';

export const options = {
  scenarios: {
    benchmark: {
      executor: 'ramping-arrival-rate',
      startRate: Math.max(1, Math.floor(steadyRate / 4)),
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 1000,
      stages: [
        { target: steadyRate, duration: rampUp },
        { target: steadyRate, duration: steady },
        { target: peakRate, duration: spike },
        { target: Math.max(1, Math.floor(steadyRate / 2)), duration: rampUp }
      ]
    }
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<150', 'p(99)<300']
  }
};

export default function () {
  const response = http.get(targetUrl, {
    headers: { Host: hostHeader }
  });

  check(response, {
    'status is 200': (res) => res.status === 200
  });
}

export function handleSummary(data) {
  return {
    stdout: JSON.stringify(data, null, 2)
  };
}
EOF
}

require_cmd docker
require_cmd mkdir
require_cmd realpath
mkdir -p "$results_dir"
docker compose -f "$compose_file" ps "$target" >/dev/null
write_script
results_abs="$(realpath "$results_dir")"

steady_rate=$((peak_rate / 2))
if ((steady_rate < 1)); then
  steady_rate=1
fi

echo "k6 benchmark"
echo "- target: $target"
echo "- compose file: $compose_file"
echo "- network: $network"
echo "- url: $url"
echo "- host header: $host_header"
echo "- ramp up: $ramp_up"
echo "- steady: $steady"
echo "- spike: $spike"
echo "- steady rate: $steady_rate"
echo "- peak rate: $peak_rate"
echo "- results dir: $results_dir"

docker run --rm \
  --network "$network" \
  --user root \
  -e BENCHMARK_URL="$url" \
  -e BENCHMARK_HOST_HEADER="$host_header" \
  -e RAMP_UP="$ramp_up" \
  -e STEADY="$steady" \
  -e SPIKE="$spike" \
  -e STEADY_RATE="$steady_rate" \
  -e PEAK_RATE="$peak_rate" \
  -v "$script_file:/scripts/benchmark.js:ro" \
  -v "$results_abs:/results" \
  grafana/k6:latest \
  run \
  --summary-export="/results/k6-summary.json" \
  /scripts/benchmark.js | tee "${results_dir}/k6-summary.txt"

echo "saved k6 results to $results_dir"
