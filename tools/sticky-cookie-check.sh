#!/usr/bin/env bash

set -euo pipefail

endpoint="${1:-http://localhost:8080/api/info}"
host_header="${2:-sticky.localtest.me}"

jar_a="$(mktemp /tmp/sticky-a.XXXX.cookie)"
jar_b="$(mktemp /tmp/sticky-b.XXXX.cookie)"
cleanup() {
  rm -f "$jar_a" "$jar_b"
}
trap cleanup EXIT

extract_server() {
  perl -ne 'print "$1\n" if /"server"\s*:\s*"([^"]+)"/'
}

request_with_jar() {
  local jar="$1"
  curl -fsS -c "$jar" -b "$jar" -H "Host: ${host_header}" "$endpoint"
}

first_a="$(request_with_jar "$jar_a")"
second_a="$(request_with_jar "$jar_a")"
first_b="$(request_with_jar "$jar_b")"

server_first_a="$(printf '%s\n' "$first_a" | extract_server)"
server_second_a="$(printf '%s\n' "$second_a" | extract_server)"
server_first_b="$(printf '%s\n' "$first_b" | extract_server)"

if [[ -z "$server_first_a" || -z "$server_second_a" || -z "$server_first_b" ]]; then
  echo "FAIL: could not parse server field from response" >&2
  exit 1
fi

echo "sticky-cookie summary"
echo "- first request (jar A):  ${server_first_a}"
echo "- second request (jar A): ${server_second_a}"
echo "- first request (jar B):  ${server_first_b}"

if [[ "$server_first_a" != "$server_second_a" ]]; then
  echo "FAIL: same cookie jar did not stay on the same backend" >&2
  exit 1
fi

if [[ "$server_first_a" == "sticky-c" || "$server_second_a" == "sticky-c" || "$server_first_b" == "sticky-c" ]]; then
  echo "FAIL: unhealthy backend sticky-c should not be selected" >&2
  exit 1
fi

echo "PASS: sticky cookie behavior looks healthy"
