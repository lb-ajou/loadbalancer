#!/usr/bin/env bash

set -euo pipefail

endpoint="${1:-http://localhost:8080/api/info}"
host_header="${2:-tuple.localtest.me}"
header_a="${3:-203.0.113.10}"
header_b="${4:-198.51.100.20}"
distribution_clients=(
  "${header_a}"
  "${header_b}"
  "198.51.100.21"
  "198.51.100.22"
  "198.51.100.23"
  "198.51.100.24"
)

extract_server() {
  perl -ne 'print "$1\n" if /"server"\s*:\s*"([^"]+)"/'
}

request_with_forwarded_for() {
  local client_ip="$1"
  curl -fsS \
    -H "Host: ${host_header}" \
    -H "X-Forwarded-For: ${client_ip}" \
    "${endpoint}"
}

request_with_forwarded() {
  local client_ip="$1"
  curl -fsS \
    -H "Host: ${host_header}" \
    -H "Forwarded: for=${client_ip}" \
    "${endpoint}"
}

first_a="$(request_with_forwarded_for "$header_a")"
second_a="$(request_with_forwarded_for "$header_a")"
first_b="$(request_with_forwarded_for "$header_b")"
second_b="$(request_with_forwarded_for "$header_b")"
forwarded_first="$(request_with_forwarded "$header_a")"
forwarded_second="$(request_with_forwarded "$header_a")"

server_first_a="$(printf '%s\n' "$first_a" | extract_server)"
server_second_a="$(printf '%s\n' "$second_a" | extract_server)"
server_first_b="$(printf '%s\n' "$first_b" | extract_server)"
server_second_b="$(printf '%s\n' "$second_b" | extract_server)"
server_forwarded_first="$(printf '%s\n' "$forwarded_first" | extract_server)"
server_forwarded_second="$(printf '%s\n' "$forwarded_second" | extract_server)"

if [[ -z "$server_first_a" || -z "$server_second_a" || -z "$server_first_b" || -z "$server_second_b" || -z "$server_forwarded_first" || -z "$server_forwarded_second" ]]; then
  echo "FAIL: could not parse server field from response" >&2
  exit 1
fi

echo "5-tuple-hash summary"
echo "- first request (${header_a}):  ${server_first_a}"
echo "- second request (${header_a}): ${server_second_a}"
echo "- first request (${header_b}):  ${server_first_b}"
echo "- second request (${header_b}): ${server_second_b}"
echo "- first Forwarded (${header_a}):  ${server_forwarded_first}"
echo "- second Forwarded (${header_a}): ${server_forwarded_second}"

if [[ "$server_first_a" != "$server_second_a" ]]; then
  echo "FAIL: same forwarded client did not stay on the same backend" >&2
  exit 1
fi

if [[ "$server_first_b" != "$server_second_b" ]]; then
  echo "FAIL: second forwarded client did not stay on the same backend" >&2
  exit 1
fi

if [[ "$server_forwarded_first" != "$server_forwarded_second" ]]; then
  echo "FAIL: Forwarded header path did not stay on the same backend" >&2
  exit 1
fi

if [[ "$server_first_a" == "tuple-c" || "$server_second_a" == "tuple-c" || "$server_first_b" == "tuple-c" || "$server_second_b" == "tuple-c" || "$server_forwarded_first" == "tuple-c" || "$server_forwarded_second" == "tuple-c" ]]; then
  echo "FAIL: unhealthy backend tuple-c should not be selected" >&2
  exit 1
fi

declare -A distribution

for client_ip in "${distribution_clients[@]}"; do
  response="$(request_with_forwarded_for "$client_ip")"
  server="$(printf '%s\n' "$response" | extract_server)"
  if [[ -z "$server" ]]; then
    echo "FAIL: distribution sample for ${client_ip} has no server field" >&2
    exit 1
  fi
  if [[ "$server" == "tuple-c" ]]; then
    echo "FAIL: unhealthy backend tuple-c appeared in distribution sample" >&2
    exit 1
  fi
  distribution["$server"]=$(( ${distribution["$server"]:-0} + 1 ))
done

echo "distribution sample (${#distribution_clients[@]} clients)"
for server in $(printf '%s\n' "${!distribution[@]}" | sort); do
  echo "- ${server}: ${distribution[$server]}"
done

echo "PASS: 5_tuple_hash behavior looks healthy"
