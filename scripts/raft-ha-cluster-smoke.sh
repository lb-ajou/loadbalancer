#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="composes/raft-ha-cluster/compose.yaml"
PROJECT_NAME="${RAFT_HA_PROJECT_NAME:-reverseproxy-raft-ha-$$}"
OUT_DIR="composes/raft-ha-cluster/.out"

dashboard_url() {
  case "$1" in
    node-1) printf "http://localhost:19090" ;;
    node-2) printf "http://localhost:19091" ;;
    node-3) printf "http://localhost:19092" ;;
    *) printf "unknown node: %s\n" "$1" >&2; return 1 ;;
  esac
}

proxy_url() {
  case "$1" in
    node-1) printf "http://localhost:18080" ;;
    node-2) printf "http://localhost:18081" ;;
    node-3) printf "http://localhost:18082" ;;
    *) printf "unknown node: %s\n" "$1" >&2; return 1 ;;
  esac
}

log() {
  printf "\n[raft-ha-smoke] %s\n" "$*"
}

fail() {
  printf "\n[raft-ha-smoke] FAIL: %s\n" "$*" >&2
  exit 1
}

compose() {
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM

  if [ "${KEEP_RAFT_HA_SMOKE:-0}" = "1" ]; then
    log "leaving compose environment running for inspection: project=${PROJECT_NAME}"
  else
    compose down -v --remove-orphans >/dev/null 2>&1 || true
  fi

  exit "$status"
}

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    fail "required command not found: ${command_name}"
  fi
}

check_dependencies() {
  require_command curl
  require_command docker
  require_command go
  require_command jq
}

build_binaries() {
  mkdir -p "$OUT_DIR"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "${OUT_DIR}/reverseproxy" ./main.go
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "${OUT_DIR}/test-server" ./composes/test-server
}

wait_http() {
  local url="$1"
  local name="$2"
  local attempts="${3:-60}"
  local delay="${4:-1}"

  for _ in $(seq 1 "$attempts"); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done

  fail "timed out waiting for ${name} at ${url}"
}

config_url() {
  local node="$1"
  printf "%s/api/config" "$(dashboard_url "$node")"
}

lifecycle_cli() {
  "${OUT_DIR}/reverseproxy" cluster "$@"
}

bootstrap_cluster() {
  local node="$1"
  local node_id="$2"
  local bind_addr="$3"
  local advertise_addr="$4"

  lifecycle_cli bootstrap \
    --dashboard "$(dashboard_url "$node")" \
    --node-id "$node_id" \
    --raft-bind "$bind_addr" \
    --raft-advertise "$advertise_addr" ||
    fail "bootstrap ${node} failed"
}

join_cluster() {
  local node="$1"
  local node_id="$2"
  local bind_addr="$3"
  local advertise_addr="$4"

  lifecycle_cli join \
    --dashboard "$(dashboard_url "$node")" \
    --node-id "$node_id" \
    --raft-bind "$bind_addr" \
    --raft-advertise "$advertise_addr" \
    --peer "http://proxy-1:9090" \
    --peer "http://proxy-2:9090" \
    --peer "http://proxy-3:9090" ||
    fail "join ${node} failed"
}

require_status_has_raft() {
  local node="$1"
  local body
  body="$(curl -fsS "$(dashboard_url "$node")/api/status")" || fail "${node} status request failed"
  if ! printf "%s" "$body" | jq -e '.raft.enabled == true and (.raft.state | type == "string")' >/dev/null; then
    printf "%s\n" "$body" >&2
    fail "${node} status did not include raft state"
  fi
}

require_cluster_has_member() {
  local node="$1"
  local member="$2"
  local body
  body="$(curl -fsS "$(dashboard_url "$node")/api/cluster")" || fail "${node} cluster request failed"
  if ! printf "%s" "$body" | jq -e --arg member "$member" '.enabled == true and (.members[]? | select(.id == $member))' >/dev/null; then
    printf "%s\n" "$body" >&2
    fail "${node} cluster did not include member ${member}"
  fi
}

config_has_route() {
  local body="$1"
  local route_id="$2"
  printf "%s" "$body" | jq -e --arg route_id "$route_id" '.routes[]? | select(.id == $route_id)' >/dev/null
}

config_has_pool() {
  local body="$1"
  local pool_id="$2"
  printf "%s" "$body" | jq -e --arg pool_id "$pool_id" '.upstream_pools[$pool_id] != null' >/dev/null
}

try_config_has_route() {
  local node="$1"
  local route_id="$2"

  local body
  body="$(curl -fsS "$(config_url "$node")" 2>/dev/null)" || return 1
  config_has_route "$body" "$route_id"
}

try_config_has_pool() {
  local node="$1"
  local pool_id="$2"

  local body
  body="$(curl -fsS "$(config_url "$node")" 2>/dev/null)" || return 1
  config_has_pool "$body" "$pool_id"
}

require_config_has_route() {
  local node="$1"
  local route_id="$2"

  local body
  if ! body="$(curl -fsS "$(config_url "$node")")"; then
    fail "${node} config request failed"
  fi
  if ! config_has_route "$body" "$route_id"; then
    printf "%s\n" "$body" >&2
    fail "${node} config does not contain route ${route_id}"
  fi
}

require_config_has_pool() {
  local node="$1"
  local pool_id="$2"

  local body
  if ! body="$(curl -fsS "$(config_url "$node")")"; then
    fail "${node} config request failed"
  fi
  if ! config_has_pool "$body" "$pool_id"; then
    printf "%s\n" "$body" >&2
    fail "${node} config does not contain pool ${pool_id}"
  fi
}

try_proxy_route() {
  local node="$1"
  local host="$2"
  local url
  url="$(proxy_url "$node")/api/info"

  local body
  body="$(curl -fsS -H "Host: ${host}" "$url" 2>/dev/null)" || return 1
  printf "%s" "$body" | jq -e '.server | test("^backend-[abc]$")' >/dev/null
}

require_proxy_route() {
  local node="$1"
  local host="$2"
  local url
  url="$(proxy_url "$node")/api/info"

  local body
  if ! body="$(curl -fsS -H "Host: ${host}" "$url")"; then
    fail "${node} request for ${host} failed"
  fi
  if ! printf "%s" "$body" | jq -e '.server | test("^backend-[abc]$")' >/dev/null; then
    printf "%s\n" "$body" >&2
    fail "${node} did not route ${host} to a backend"
  fi
}

request_json_capture() {
  local method="$1"
  local url="$2"
  local request_body="$3"
  local response_file status

  response_file="$(mktemp)"
  status="$(curl -sS -o "$response_file" -w "%{http_code}" \
    -X "$method" "$url" \
    -H "Content-Type: application/json" \
    -d "$request_body" || true)"
  CAPTURED_RESPONSE="$(cat "$response_file")"
  rm -f "$response_file"

  if [ -z "$status" ]; then
    fail "${method} ${url} failed before an HTTP response was received"
  fi

  CAPTURED_STATUS="$status"
}

post_json_capture() {
  request_json_capture POST "$1" "$2"
}

put_json_capture() {
  request_json_capture PUT "$1" "$2"
}

config_with_route_pool() {
  local current_config="$1"
  local route_json="$2"
  local pool_id="$3"
  local pool_json="$4"

  printf "%s" "$current_config" | jq \
    --argjson route "$route_json" \
    --arg pool_id "$pool_id" \
    --argjson pool "$pool_json" \
    '{
      routes: (((.routes // []) | map(select(.id != $route.id))) + [$route]),
      upstream_pools: ((.upstream_pools // {}) + {($pool_id): $pool})
    }'
}

put_config_with_route_pool() {
  local node="$1"
  local route_json="$2"
  local pool_id="$3"
  local pool_json="$4"
  local current_config updated_config

  current_config="$(curl -fsS "$(config_url "$node")")" || return 1
  updated_config="$(config_with_route_pool "$current_config" "$route_json" "$pool_id" "$pool_json")" || return 1

  curl -fsS -X PUT "$(config_url "$node")" \
    -H "Content-Type: application/json" \
    -d "$updated_config" >/dev/null
}

capture_put_config_with_route_pool() {
  local node="$1"
  local route_json="$2"
  local pool_id="$3"
  local pool_json="$4"
  local current_config updated_config

  if ! current_config="$(curl -fsS "$(config_url "$node")")"; then
    fail "${node} config request failed before write capture"
  fi
  updated_config="$(config_with_route_pool "$current_config" "$route_json" "$pool_id" "$pool_json")" ||
    fail "${node} config transform failed before write capture"

  put_json_capture "$(config_url "$node")" "$updated_config"
}

initial_pool_json() {
  cat <<'JSON'
{
  "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
  "health_check": {
    "path": "/health",
    "interval": "5s",
    "timeout": "2s",
    "expect_status": 200
  }
}
JSON
}

initial_route_json() {
  cat <<'JSON'
{
  "id": "r-raft",
  "enabled": true,
  "match": {
    "hosts": ["raft.localtest.me"],
    "path": { "type": "prefix", "value": "/" }
  },
  "upstream_pool": "pool-raft"
}
JSON
}

create_initial_config() {
  put_config_with_route_pool "$1" "$(initial_route_json)" "pool-raft" "$(initial_pool_json)"
}

added_pool_json() {
  cat <<'JSON'
{
  "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
  "health_check": {
    "path": "/health",
    "interval": "5s",
    "timeout": "2s",
    "expect_status": 200
  }
}
JSON
}

added_route_json() {
  cat <<'JSON'
{
  "id": "r-added",
  "enabled": true,
  "match": {
    "hosts": ["raft-added.localtest.me"],
    "path": { "type": "prefix", "value": "/" }
  },
  "upstream_pool": "pool-added"
}
JSON
}

create_added_config() {
  put_config_with_route_pool "$1" "$(added_route_json)" "pool-added" "$(added_pool_json)"
}

capture_follower_write() {
  local follower="$1"

  capture_put_config_with_route_pool "$follower" '{
    "id": "r-follower-rejected",
    "enabled": true,
    "match": {
      "hosts": ["rejected.localtest.me"],
      "path": { "type": "prefix", "value": "/" }
    },
    "upstream_pool": "pool-raft"
  }' "pool-raft" '{
    "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
    "health_check": {
      "path": "/health",
      "interval": "5s",
      "timeout": "2s",
      "expect_status": 200
    }
  }'
}

try_follower_write_rejected() {
  local follower="$1"

  capture_follower_write "$follower"

  [ "$CAPTURED_STATUS" = "409" ] &&
    printf "%s" "$CAPTURED_RESPONSE" | jq -e '.code == "not_raft_leader"' >/dev/null &&
    printf "%s" "$CAPTURED_RESPONSE" | jq -e '.leader_address | type == "string" and length > 0' >/dev/null
}

require_follower_write_rejected() {
  local follower="$1"

  capture_follower_write "$follower"

  if [ "$CAPTURED_STATUS" != "409" ]; then
    printf "%s\n" "$CAPTURED_RESPONSE" >&2
    fail "expected follower write on ${follower} to return 409, got ${CAPTURED_STATUS}"
  fi
  if ! printf "%s" "$CAPTURED_RESPONSE" | jq -e '.code == "not_raft_leader"' >/dev/null; then
    printf "%s\n" "$CAPTURED_RESPONSE" >&2
    fail "expected follower write on ${follower} to return not_raft_leader"
  fi
  if ! printf "%s" "$CAPTURED_RESPONSE" | jq -e '.leader_address | type == "string" and length > 0' >/dev/null; then
    printf "%s\n" "$CAPTURED_RESPONSE" >&2
    fail "expected follower write on ${follower} to include leader_address"
  fi
}

find_follower() {
  for node in node-2 node-3; do
    if try_follower_write_rejected "$node" >/dev/null 2>&1; then
      printf "%s" "$node"
      return 0
    fi
  done

  fail "could not find a follower among node-2 and node-3"
}

require_join_validation_rejected() {
  local request_body="$1"
  local expected_code="$2"

  post_json_capture "http://localhost:19090/api/cluster/join" "$request_body"

  if [ "$CAPTURED_STATUS" != "400" ]; then
    printf "%s\n" "$CAPTURED_RESPONSE" >&2
    fail "expected join validation to return 400, got ${CAPTURED_STATUS}"
  fi
  if ! printf "%s" "$CAPTURED_RESPONSE" | jq -e --arg code "$expected_code" '.code == $code' >/dev/null; then
    printf "%s\n" "$CAPTURED_RESPONSE" >&2
    fail "expected join validation code ${expected_code}"
  fi
}

try_create_failover_config() {
  local node="$1"

  put_config_with_route_pool "$node" "$(failover_route_json)" "pool-failover" "$(failover_pool_json)"
}

try_create_persistence_config() {
  local node="$1"

  put_config_with_route_pool "$node" "$(persistence_route_json)" "pool-persistence" "$(persistence_pool_json)"
}

failover_pool_json() {
  cat <<'JSON'
{
  "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
  "health_check": {
    "path": "/health",
    "interval": "5s",
    "timeout": "2s",
    "expect_status": 200
  }
}
JSON
}

failover_route_json() {
  cat <<'JSON'
{
  "id": "r-failover",
  "enabled": true,
  "match": {
    "hosts": ["raft-failover.localtest.me"],
    "path": { "type": "prefix", "value": "/" }
  },
  "upstream_pool": "pool-failover"
}
JSON
}

create_failover_config() {
  put_config_with_route_pool "$1" "$(failover_route_json)" "pool-failover" "$(failover_pool_json)"
}

persistence_pool_json() {
  cat <<'JSON'
{
  "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
  "health_check": {
    "path": "/health",
    "interval": "5s",
    "timeout": "2s",
    "expect_status": 200
  }
}
JSON
}

persistence_route_json() {
  cat <<'JSON'
{
  "id": "r-persistence",
  "enabled": true,
  "match": {
    "hosts": ["raft-persistence.localtest.me"],
    "path": { "type": "prefix", "value": "/" }
  },
  "upstream_pool": "pool-persistence"
}
JSON
}

create_persistence_config() {
  put_config_with_route_pool "$1" "$(persistence_route_json)" "pool-persistence" "$(persistence_pool_json)"
}

find_leader_after_failover() {
  local attempts="${1:-60}"

  for _ in $(seq 1 "$attempts"); do
    for node in node-2 node-3; do
      if try_create_failover_config "$node" >/dev/null 2>&1; then
        printf "%s" "$node"
        return 0
      fi
    done
    sleep 1
  done

  fail "could not find leader after stopping proxy-1"
}

find_leader_after_restart() {
  local attempts="${1:-60}"

  for _ in $(seq 1 "$attempts"); do
    for node in node-1 node-2 node-3; do
      if try_create_persistence_config "$node" >/dev/null 2>&1; then
        printf "%s" "$node"
        return 0
      fi
    done
    sleep 1
  done

  fail "could not find leader after persistence restart"
}

wait_config_has_route() {
  local node="$1"
  local route_id="$2"
  local attempts="${3:-60}"

  for _ in $(seq 1 "$attempts"); do
    if try_config_has_route "$node" "$route_id"; then
      return 0
    fi
    sleep 1
  done

  fail "${node} config did not converge on route ${route_id}"
}

wait_config_has_pool() {
  local node="$1"
  local pool_id="$2"
  local attempts="${3:-60}"

  for _ in $(seq 1 "$attempts"); do
    if try_config_has_pool "$node" "$pool_id"; then
      return 0
    fi
    sleep 1
  done

  fail "${node} config did not converge on pool ${pool_id}"
}

wait_proxy_route() {
  local node="$1"
  local host="$2"
  local attempts="${3:-60}"

  for _ in $(seq 1 "$attempts"); do
    if try_proxy_route "$node" "$host"; then
      return 0
    fi
    sleep 1
  done

  require_proxy_route "$node" "$host"
}

main() {
  trap cleanup EXIT INT TERM

  check_dependencies

  log "reset compose environment"
  compose down -v --remove-orphans

  log "build linux binaries"
  build_binaries

  log "start backends and bootstrap node"
  compose up -d --build backend-a backend-b backend-c proxy-1

  wait_http "http://localhost:19090/api/status" "proxy-1 dashboard"
  bootstrap_cluster node-1 node-1 "0.0.0.0:7001" "proxy-1:7001"
  wait_http "http://localhost:19090/api/config" "proxy-1 config API"
  require_status_has_raft node-1

  log "create initial route through proxy-1 leader"
  create_initial_config node-1
  require_config_has_route node-1 "r-raft"
  require_config_has_pool node-1 "pool-raft"
  wait_proxy_route node-1 "raft.localtest.me"

  log "bootstrap checks passed"

  log "start joining nodes"
  compose up -d --build proxy-2 proxy-3

  wait_http "http://localhost:19091/api/status" "proxy-2 dashboard"
  wait_http "http://localhost:19092/api/status" "proxy-3 dashboard"
  join_cluster node-2 node-2 "0.0.0.0:7002" "proxy-2:7002"
  join_cluster node-3 node-3 "0.0.0.0:7003" "proxy-3:7003"
  wait_http "http://localhost:19091/api/config" "proxy-2 config API"
  wait_http "http://localhost:19092/api/config" "proxy-3 config API"
  require_cluster_has_member node-1 "node-1"
  require_cluster_has_member node-1 "node-2"
  require_cluster_has_member node-1 "node-3"

  log "verify joined nodes caught up with raft state"
  wait_config_has_route node-2 "r-raft"
  wait_config_has_route node-3 "r-raft"
  wait_proxy_route node-2 "raft.localtest.me"
  wait_proxy_route node-3 "raft.localtest.me"

  log "write new route through proxy-1 leader"
  create_added_config node-1

  log "verify replication to all nodes"
  wait_config_has_route node-1 "r-added"
  wait_config_has_route node-2 "r-added"
  wait_config_has_route node-3 "r-added"
  wait_config_has_pool node-1 "pool-added"
  wait_config_has_pool node-2 "pool-added"
  wait_config_has_pool node-3 "pool-added"
  wait_proxy_route node-1 "raft-added.localtest.me"
  wait_proxy_route node-2 "raft-added.localtest.me"
  wait_proxy_route node-3 "raft-added.localtest.me"

  log "join and replication checks passed"

  log "verify follower write rejection"
  local follower
  follower="$(find_follower)"
  log "verified follower write rejection on ${follower}"

  log "verify raft join validation"
  require_join_validation_rejected '{"node_id":"bad:node","raft_address":"proxy-bad:7009"}' "invalid_node_id"
  require_join_validation_rejected '{"node_id":"node-bad","raft_address":"not-a-host-port"}' "invalid_raft_address"

  log "negative checks passed"

  log "stop proxy-1 and wait for failover"
  compose stop proxy-1
  sleep 3

  local new_leader
  new_leader="$(find_leader_after_failover)"
  log "new leader after failover: ${new_leader}"
  create_failover_config "$new_leader"

  log "verify failover write on surviving nodes"
  wait_config_has_route node-2 "r-failover"
  wait_config_has_route node-3 "r-failover"
  wait_config_has_pool node-2 "pool-failover"
  wait_config_has_pool node-3 "pool-failover"
  wait_proxy_route node-2 "raft-failover.localtest.me"
  wait_proxy_route node-3 "raft-failover.localtest.me"

  log "restart old leader and verify catch-up"
  compose up -d proxy-1
  wait_http "http://localhost:19090/api/config" "proxy-1 dashboard after rejoin"
  wait_config_has_route node-1 "r-failover"
  wait_config_has_pool node-1 "pool-failover"
  wait_proxy_route node-1 "raft-failover.localtest.me"

  log "verify persistence across stop/start"
  compose stop proxy-1 proxy-2 proxy-3
  compose up -d proxy-1 proxy-2 proxy-3
  wait_http "http://localhost:19090/api/config" "proxy-1 dashboard after persistence restart"
  wait_http "http://localhost:19091/api/config" "proxy-2 dashboard after persistence restart"
  wait_http "http://localhost:19092/api/config" "proxy-3 dashboard after persistence restart"
  wait_config_has_route node-1 "r-failover"
  wait_config_has_route node-2 "r-failover"
  wait_config_has_route node-3 "r-failover"
  wait_config_has_pool node-1 "pool-failover"
  wait_config_has_pool node-2 "pool-failover"
  wait_config_has_pool node-3 "pool-failover"
  wait_proxy_route node-1 "raft-failover.localtest.me"
  wait_proxy_route node-2 "raft-failover.localtest.me"
  wait_proxy_route node-3 "raft-failover.localtest.me"

  local restart_leader
  restart_leader="$(find_leader_after_restart)"
  log "leader after persistence restart: ${restart_leader}"
  create_persistence_config "$restart_leader"
  wait_config_has_route node-1 "r-persistence"
  wait_config_has_route node-2 "r-persistence"
  wait_config_has_route node-3 "r-persistence"
  wait_config_has_pool node-1 "pool-persistence"
  wait_config_has_pool node-2 "pool-persistence"
  wait_config_has_pool node-3 "pool-persistence"
  wait_proxy_route node-1 "raft-persistence.localtest.me"
  wait_proxy_route node-2 "raft-persistence.localtest.me"
  wait_proxy_route node-3 "raft-persistence.localtest.me"

  log "failover, rejoin, and persistence checks passed"
}

main "$@"
