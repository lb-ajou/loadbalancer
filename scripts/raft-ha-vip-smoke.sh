#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="composes/raft-ha-vip/compose.yaml"
PROJECT_NAME="${RAFT_HA_VIP_PROJECT_NAME:-reverseproxy-raft-ha-vip-$$}"
OUT_DIR="composes/raft-ha-vip/.out"
VIP_ADDR="172.30.10.100"
VIP_CIDR="172.30.10.100/24"
VIP_URL="http://172.30.10.100:8080/api/info"
VIP_HOST="raft.localtest.me"

dashboard_url() {
  case "$1" in
    node-1) printf "http://localhost:19090" ;;
    node-2) printf "http://localhost:19091" ;;
    node-3) printf "http://localhost:19092" ;;
    *) printf "unknown node: %s\n" "$1" >&2; return 1 ;;
  esac
}

service_for_node() {
  case "$1" in
    node-1) printf "proxy-1" ;;
    node-2) printf "proxy-2" ;;
    node-3) printf "proxy-3" ;;
    *) printf "unknown node: %s\n" "$1" >&2; return 1 ;;
  esac
}

log() {
  printf "\n[raft-ha-vip-smoke] %s\n" "$*"
}

fail() {
  printf "\n[raft-ha-vip-smoke] FAIL: %s\n" "$*" >&2
  exit 1
}

compose() {
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM

  if [ "${KEEP_RAFT_HA_VIP_SMOKE:-0}" = "1" ]; then
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
  lifecycle_cli bootstrap \
    --dashboard "$(dashboard_url node-1)" \
    --node-id "node-1" \
    --raft-bind "0.0.0.0:7001" \
    --raft-advertise "proxy-1:7001" \
    --vip-interface "eth0" \
    --vip-address "$VIP_CIDR" \
    --garp-count 3 \
    --garp-interval "100ms" \
    --acquire-delay "300ms" \
    --release-on-shutdown ||
    fail "bootstrap node-1 failed"
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
    --vip-interface "eth0" \
    --peer "http://proxy-1:9090" \
    --peer "http://proxy-2:9090" \
    --peer "http://proxy-3:9090" ||
    fail "join ${node} failed"
}

require_status_vip_owned() {
  local node="$1"
  local body
  body="$(curl -fsS "$(dashboard_url "$node")/api/status")" || fail "${node} status request failed"
  if ! printf "%s" "$body" | jq -e '.vip.enabled == true and .vip.owned == true' >/dev/null; then
    printf "%s\n" "$body" >&2
    fail "${node} status did not report vip ownership"
  fi
}

require_status_vip_not_owned() {
  local node="$1"
  local body
  body="$(curl -fsS "$(dashboard_url "$node")/api/status")" || fail "${node} status request failed"
  if ! printf "%s" "$body" | jq -e '.vip.enabled == true and .vip.owned == false' >/dev/null; then
    printf "%s\n" "$body" >&2
    fail "${node} status reported unexpected vip ownership"
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

try_create_failover_config() {
  local node="$1"

  put_config_with_route_pool "$node" "$(failover_route_json)" "pool-failover" "$(failover_pool_json)"
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

node_has_vip() {
  local node="$1"
  local service
  service="$(service_for_node "$node")"

  compose exec -T "$service" sh -c \
    'ip -4 addr show dev "$1" | grep -Fq "$2"' \
    sh eth0 "$VIP_CIDR" >/dev/null 2>&1
}

require_node_has_vip() {
  local node="$1"

  if ! node_has_vip "$node"; then
    fail "${node} does not own VIP ${VIP_CIDR}"
  fi
  log "${node} owns VIP ${VIP_CIDR}"
}

wait_node_has_vip() {
  local node="$1"
  local attempts="${2:-60}"

  for _ in $(seq 1 "$attempts"); do
    if node_has_vip "$node"; then
      log "${node} owns VIP ${VIP_CIDR}"
      return 0
    fi
    sleep 1
  done

  require_node_has_vip "$node"
}

require_node_lacks_vip() {
  local node="$1"

  if node_has_vip "$node"; then
    fail "${node} unexpectedly owns VIP ${VIP_CIDR}"
  fi
  log "${node} does not own VIP ${VIP_CIDR}"
}

try_vip_route() {
  local body
  body="$(compose exec -T observer wget -qO- \
    --header "Host: ${VIP_HOST}" \
    "$VIP_URL" 2>/dev/null)" || return 1

  printf "%s" "$body" | jq -e '.server | test("^backend-[abc]$")' >/dev/null
}

wait_vip_route() {
  local attempts="${1:-60}"

  for _ in $(seq 1 "$attempts"); do
    if try_vip_route; then
      return 0
    fi
    sleep 1
  done

  fail "VIP route did not serve ${VIP_HOST} at ${VIP_URL}"
}

survivor_other_than() {
  case "$1" in
    node-2) printf "node-3" ;;
    node-3) printf "node-2" ;;
    *) fail "unexpected failover leader: $1" ;;
  esac
}

main() {
  trap cleanup EXIT INT TERM

  check_dependencies

  log "reset compose environment"
  compose down -v --remove-orphans

  log "build linux binaries"
  build_binaries

  log "start backends, observer, and bootstrap node"
  compose up -d --build backend-a backend-b backend-c observer proxy-1

  wait_http "http://localhost:19090/api/status" "proxy-1 dashboard"
  bootstrap_cluster
  wait_http "http://localhost:19090/api/config" "proxy-1 config API"

  log "create initial route through node-1 leader"
  create_initial_config node-1
  require_config_has_route node-1 "r-raft"
  require_config_has_pool node-1 "pool-raft"
  wait_node_has_vip node-1
  require_status_vip_owned node-1
  wait_vip_route

  log "start joining nodes"
  compose up -d --build proxy-2 proxy-3

  wait_http "http://localhost:19091/api/status" "proxy-2 dashboard"
  wait_http "http://localhost:19092/api/status" "proxy-3 dashboard"
  join_cluster node-2 node-2 "0.0.0.0:7002" "proxy-2:7002"
  join_cluster node-3 node-3 "0.0.0.0:7003" "proxy-3:7003"
  wait_http "http://localhost:19091/api/config" "proxy-2 config API"
  wait_http "http://localhost:19092/api/config" "proxy-3 config API"

  log "verify joined nodes caught up with raft state"
  wait_config_has_route node-2 "r-raft"
  wait_config_has_route node-3 "r-raft"
  wait_vip_route
  require_node_lacks_vip node-2
  require_node_lacks_vip node-3
  require_status_vip_not_owned node-2
  require_status_vip_not_owned node-3

  log "verify follower write rejection"
  local follower
  follower="$(find_follower)"
  require_follower_write_rejected "$follower"
  log "verified follower write rejection on ${follower}"

  log "stop proxy-1 and wait for VIP failover"
  compose stop proxy-1
  sleep 3

  local new_leader other_survivor
  new_leader="$(find_leader_after_failover)"
  other_survivor="$(survivor_other_than "$new_leader")"
  log "new leader after failover: ${new_leader}"
  create_failover_config "$new_leader"

  log "verify VIP moved to failover leader"
  wait_node_has_vip "$new_leader"
  require_node_lacks_vip "$other_survivor"
  require_status_vip_owned "$new_leader"
  require_status_vip_not_owned "$other_survivor"
  wait_vip_route

  log "restart old leader and verify catch-up"
  compose up -d proxy-1
  wait_http "http://localhost:19090/api/config" "proxy-1 dashboard after rejoin"
  wait_config_has_route node-1 "r-failover"
  wait_config_has_pool node-1 "pool-failover"

  if try_follower_write_rejected node-1 >/dev/null 2>&1; then
    log "node-1 rejoined as follower"
    require_node_lacks_vip node-1
  else
    log "node-1 rejoined as leader"
    wait_node_has_vip node-1
  fi

  log "success"
}

main "$@"
