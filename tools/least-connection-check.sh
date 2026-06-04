#!/usr/bin/env bash

set -euo pipefail

endpoint="${1:-http://localhost:8080/api/info}"
host_header="${2:-least.localtest.me}"
settle_seconds="${3:-6}"

extract_server() {
  perl -ne 'print "$1\n" if /"server"\s*:\s*"([^"]+)"/'
}

request() {
  curl -fsS -H "Host: ${host_header}" "${endpoint}"
}

if ! [[ "$settle_seconds" =~ ^[0-9]+$ ]]; then
  echo "FAIL: settle seconds must be a non-negative integer" >&2
  exit 1
fi

slow_file="$(mktemp /tmp/least-connection-slow.XXXX.json)"
trap 'rm -f "$slow_file"' EXIT

sleep "$settle_seconds"

request >"$slow_file" &
slow_pid=$!
sleep 0.3

fast_response="$(request)"
wait "$slow_pid"
slow_response="$(cat "$slow_file")"

slow_server="$(printf '%s\n' "$slow_response" | extract_server)"
fast_server="$(printf '%s\n' "$fast_response" | extract_server)"

if [[ -z "$slow_server" || -z "$fast_server" ]]; then
  echo "FAIL: could not parse server field from response" >&2
  exit 1
fi

echo "least-connection summary"
echo "- occupied request server: ${slow_server}"
echo "- concurrent request server: ${fast_server}"

if [[ "$slow_server" != "lc-slow" ]]; then
  echo "FAIL: first occupied request should land on lc-slow" >&2
  exit 1
fi

if [[ "$fast_server" != "lc-fast" ]]; then
  echo "FAIL: concurrent request should avoid busy lc-slow and land on lc-fast" >&2
  exit 1
fi

if [[ "$slow_server" == "lc-unhealthy" || "$fast_server" == "lc-unhealthy" ]]; then
  echo "FAIL: unhealthy backend lc-unhealthy should not be selected" >&2
  exit 1
fi

echo "PASS: least_connection behavior looks healthy"
