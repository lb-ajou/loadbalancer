#!/usr/bin/env bash

set -euo pipefail

endpoint="${1:-http://localhost:8080/api/info}"
host_header="${2:-rr.localtest.me}"
request_count="${3:-9}"

if ! [[ "$request_count" =~ ^[0-9]+$ ]] || [[ "$request_count" -le 0 ]]; then
  echo "FAIL: request count must be a positive integer" >&2
  exit 1
fi

declare -A counts
total=0

for ((i = 1; i <= request_count; i++)); do
  response="$(curl -fsS -H "Host: ${host_header}" "${endpoint}")"
  server="$(
    printf '%s\n' "$response" |
      perl -ne 'print "$1\n" if /"server"\s*:\s*"([^"]+)"/'
  )"

  if [[ -z "$server" ]]; then
    echo "FAIL: response does not contain a server field" >&2
    printf '%s\n' "$response" >&2
    exit 1
  fi

  counts["$server"]=$(( ${counts["$server"]:-0} + 1 ))
  total=$((total + 1))
done

echo "round-robin summary (${total} requests)"
for server in $(printf '%s\n' "${!counts[@]}" | sort); do
  echo "- ${server}: ${counts[$server]}"
done

if ((${#counts[@]} < 3)); then
  echo "FAIL: expected at least 3 distinct backends, got ${#counts[@]}" >&2
  exit 1
fi

expected=$((request_count / ${#counts[@]}))
remainder=$((request_count % ${#counts[@]}))

for server in "${!counts[@]}"; do
  count="${counts[$server]}"
  if ((count < expected)); then
    echo "FAIL: backend ${server} appeared ${count} times, expected at least ${expected}" >&2
    exit 1
  fi
  if ((count > expected + remainder)); then
    echo "FAIL: backend ${server} appeared ${count} times, expected at most $((expected + remainder))" >&2
    exit 1
  fi
done

echo "PASS: round-robin distribution looks healthy"
