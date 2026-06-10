# Raft HA 클러스터 테스트 가이드

이 문서는 Docker Compose 기반 `raft-ha-cluster` 시나리오의 테스트 환경 정보와 검증 방법을 정리한다. 대상은 3개 로드밸런서 노드가 HashiCorp Raft로 설정 상태를 공유하고, 3개 backend server로 요청을 분산 처리하는 HA 검증 환경이다.

## 테스트 목적

- bootstrap node가 빈 desired state로 시작한 뒤 Admin API 쓰기를 Raft FSM 상태로 정상 기록하는지 확인한다.
- join node가 leader의 `/api/cluster/join` endpoint를 통해 cluster에 합류하고, Raft log를 통해 동일한 설정 상태를 따라오는지 확인한다.
- route, upstream pool 같은 desired proxy config의 쓰기가 leader에서만 허용되고 모든 node에 복제되는지 확인한다.
- follower에 대한 설정 쓰기 요청이 `409 not_raft_leader`로 거절되는지 확인한다.
- leader 장애 후 남은 node에서 새 leader가 선출되고 설정 쓰기가 가능한지 확인한다.
- 중단됐던 node가 재기동 후 최신 Raft 상태로 catch-up 하는지 확인한다.
- Raft data volume을 유지한 stop/start 이후 설정 상태가 유지되는지 확인한다.

## 파일 구성

| 경로 | 용도 |
| --- | --- |
| `composes/raft-ha-cluster/compose.yaml` | 3 proxy, 3 backend Docker Compose 시나리오 |
| `composes/raft-ha-cluster/Dockerfile.proxy` | local build된 로드밸런서 binary를 포함하는 테스트 이미지 |
| `composes/raft-ha-cluster/Dockerfile.test-server` | local build된 backend test server binary를 포함하는 테스트 이미지 |
| `composes/raft-ha-cluster/configs/node-1/app.json` | bootstrap proxy node 설정 |
| `composes/raft-ha-cluster/configs/node-2/app.json` | join proxy node 설정 |
| `composes/raft-ha-cluster/configs/node-3/app.json` | join proxy node 설정 |
| `scripts/raft-ha-cluster-smoke.sh` | 클러스터 기동, 복제, failover, persistence를 자동 검증하는 smoke script |

## 사전 조건

로컬 머신에 다음 command가 필요하다.

```bash
docker
go
curl
jq
```

Docker daemon이 실행 중이어야 하며, 다음 host port가 비어 있어야 한다.

| 용도 | Host port |
| --- | --- |
| proxy HTTP | `18080`, `18081`, `18082` |
| dashboard/admin HTTP | `19090`, `19091`, `19092` |
| Raft transport 확인용 host binding | `17001`, `17002`, `17003` |

Compose build는 `busybox:1.31.1` 이미지를 사용한다. 이미지가 로컬에 없으면 Docker가 pull할 수 있어야 한다.

## 클러스터 구성

### Backend server

| Service | Container port | 역할 |
| --- | --- | --- |
| `backend-a` | `8080` | `/health`, `/api/info` 제공 |
| `backend-b` | `8080` | `/health`, `/api/info` 제공 |
| `backend-c` | `8080` | `/health`, `/api/info` 제공 |

각 backend는 `SERVER_NAME`을 `backend-a`, `backend-b`, `backend-c`로 노출한다. proxy routing 검증은 `/api/info` 응답의 `server` 값이 `backend-a`, `backend-b`, `backend-c` 중 하나인지 확인한다.

### Proxy node

| Node | Service | Proxy URL | Dashboard URL | Raft advertise address | Raft data volume |
| --- | --- | --- | --- | --- | --- |
| `node-1` | `proxy-1` | `http://localhost:18080` | `http://localhost:19090` | `proxy-1:7001` | `proxy-1-raft` |
| `node-2` | `proxy-2` | `http://localhost:18081` | `http://localhost:19091` | `proxy-2:7002` | `proxy-2-raft` |
| `node-3` | `proxy-3` | `http://localhost:18082` | `http://localhost:19092` | `proxy-3:7003` | `proxy-3-raft` |

`proxy-1`은 clean node로 시작한 뒤 smoke script가 `loadbalancer cluster bootstrap` CLI를 호출해 single-node Raft cluster를 만든다. 초기 route/upstream은 dashboard/Admin API 쓰기로 생성한다.

`proxy-2`, `proxy-3`도 clean node로 시작한다. smoke script가 각 노드에 `loadbalancer cluster join` CLI로 peer dashboard/admin endpoint 후보를 전달하면, 노드는 후보를 순서대로 시도해 cluster에 join한 뒤 leader가 가진 Raft 상태를 따라온다.

compose proxy healthcheck는 bootstrap 전 clean node도 ready로 볼 수 있도록 `/api/node/cluster-status`를 사용한다. `/api/config`는 Raft store가 열린 뒤 설정 복제 검증 단계에서 조회한다.

각 proxy의 app config는 Raft node ID, bind address, advertise address를 포함하지 않는다. 이 값들은 smoke script의 lifecycle CLI 입력으로 전달되고, 성공 후 `/app/data/raft` 안의 node-local metadata로 저장된다.

각 proxy는 `/app/data/raft`를 별도 named volume에 저장한다. 따라서 container를 stop/start 해도 volume이 유지되면 Raft log/snapshot과 node-local metadata 기반으로 설정 상태와 Raft identity를 복원한다. clean bootstrap을 다시 수행하려면 `docker compose down -v`로 volume까지 삭제해야 한다.

## 초기 설정 생성

smoke script는 `proxy-1` dashboard/Admin API에 다음 route와 upstream pool을 저장해 초기 Raft desired state를 만든다.

| 항목 | 값 |
| --- | --- |
| Route ID | `r-raft` |
| Host match | `raft.localtest.me` |
| Upstream pool | `pool-raft` |
| Upstreams | `backend-a:8080`, `backend-b:8080`, `backend-c:8080` |
| Health check | `GET /health`, expected status `200` |

기본 routing 확인은 다음 형태로 수행한다.

```bash
curl -fsS -H 'Host: raft.localtest.me' http://localhost:18080/api/info | jq
```

정상이라면 `.server` 값이 `backend-a`, `backend-b`, `backend-c` 중 하나로 응답한다.

## 자동 Smoke Test

가장 권장하는 검증 방식은 smoke script 실행이다.

```bash
scripts/raft-ha-cluster-smoke.sh
```

script는 실행마다 고유 compose project name을 사용한다. 기본값은 `loadbalancer-raft-ha-<pid>` 형태다. 전체 흐름은 다음과 같다.

1. 기존 동일 project compose 환경을 `down -v --remove-orphans`로 정리한다.
2. Linux용 `loadbalancer`, `test-server` binary를 `composes/raft-ha-cluster/.out`에 빌드한다.
3. `backend-a`, `backend-b`, `backend-c`, `proxy-1`을 시작한다.
4. `proxy-1` dashboard가 응답할 때까지 대기한다.
5. dashboard/Admin API로 route `r-raft`와 pool `pool-raft`를 `proxy-1`에 생성한다.
6. `proxy-1`을 통해 `raft.localtest.me` routing이 가능한지 확인한다.
7. `proxy-2`, `proxy-3`을 시작하고 join 완료를 기다린다.
8. join node가 Raft 상태를 catch-up 했는지 확인한다.
9. leader인 `proxy-1`에 `pool-added`, `r-added`를 생성한다.
10. 모든 node에 `r-added`, `pool-added`가 복제되고 routing이 가능한지 확인한다.
11. follower node에 쓰기 요청을 보내 `409 not_raft_leader` 응답을 확인한다.
12. 잘못된 join 요청이 `400 invalid_node_id`, `400 invalid_raft_address`로 거절되는지 확인한다.
13. `proxy-1`을 중단하고 `proxy-2`, `proxy-3` 중 새 leader를 찾는다.
14. 새 leader에 `pool-failover`, `r-failover`를 생성한다.
15. 살아 있는 node에서 failover 후 설정 복제와 routing을 확인한다.
16. `proxy-1`을 재기동하고 failover 중 생성된 설정을 catch-up 하는지 확인한다.
17. 모든 proxy node를 stop/start 한 뒤 Raft volume 기반 persistence를 확인한다.
18. 재시작 후 leader를 다시 찾고 `pool-persistence`, `r-persistence` 쓰기와 복제를 확인한다.
19. 기본 모드에서는 종료 시 compose container와 volume을 정리한다.

성공 시 주요 log는 다음과 같은 형태로 출력된다.

```text
[raft-ha-smoke] bootstrap checks passed
[raft-ha-smoke] join and replication checks passed
[raft-ha-smoke] negative checks passed
[raft-ha-smoke] failover, rejoin, and persistence checks passed
```

### 검사 후 환경 보존

실패 원인 분석이나 수동 확인을 위해 container와 volume을 남기려면 다음처럼 실행한다.

```bash
KEEP_RAFT_HA_SMOKE=1 scripts/raft-ha-cluster-smoke.sh
```

고정 project name을 사용하면 이후 Docker Compose command로 같은 환경을 다루기 쉽다.

```bash
RAFT_HA_PROJECT_NAME=loadbalancer-raft-ha-debug KEEP_RAFT_HA_SMOKE=1 scripts/raft-ha-cluster-smoke.sh
```

보존한 환경을 정리하려면 같은 project name으로 `down -v`를 실행한다.

```bash
docker compose -p loadbalancer-raft-ha-debug -f composes/raft-ha-cluster/compose.yaml down -v --remove-orphans
```

## 수동 테스트 절차

자동 smoke script가 전체 회귀 검증용이라면, 수동 절차는 특정 단계의 상태를 직접 관찰하기 위한 용도다.

### 1. 테스트 binary 빌드

Compose image는 repository root의 Go source를 직접 빌드하지 않고, 미리 준비된 Linux binary를 복사한다. 수동 compose 실행 전 다음을 실행한다.

```bash
mkdir -p composes/raft-ha-cluster/.out
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o composes/raft-ha-cluster/.out/loadbalancer ./main.go
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o composes/raft-ha-cluster/.out/test-server ./composes/test-server
```

### 2. Clean bootstrap으로 시작

기존 Raft volume이 남아 있으면 기존 Raft 상태가 복원된다. 빈 desired state bootstrap부터 반복하려면 volume까지 삭제한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml down -v --remove-orphans
docker compose -f composes/raft-ha-cluster/compose.yaml up -d --build
```

기동 상태를 확인한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml ps
```

### 3. Lifecycle status 확인

각 node의 lifecycle status API가 응답하는지 확인한다.

```bash
curl -fsS http://localhost:19090/api/node/cluster-status | jq
curl -fsS http://localhost:19091/api/node/cluster-status | jq
curl -fsS http://localhost:19092/api/node/cluster-status | jq
```

### 4. Dashboard config 확인

각 node의 dashboard config API가 같은 route와 pool을 반환하는지 확인한다.

```bash
curl -fsS http://localhost:19090/api/config | jq
curl -fsS http://localhost:19091/api/config | jq
curl -fsS http://localhost:19092/api/config | jq
```

최소 확인 기준은 다음과 같다.

```bash
curl -fsS http://localhost:19090/api/config | jq -e '.routes[] | select(.id == "r-raft")'
curl -fsS http://localhost:19091/api/config | jq -e '.routes[] | select(.id == "r-raft")'
curl -fsS http://localhost:19092/api/config | jq -e '.routes[] | select(.id == "r-raft")'
```

### 4. Proxy routing 확인

모든 proxy node에서 초기 route가 backend로 전달되는지 확인한다.

```bash
curl -fsS -H 'Host: raft.localtest.me' http://localhost:18080/api/info | jq
curl -fsS -H 'Host: raft.localtest.me' http://localhost:18081/api/info | jq
curl -fsS -H 'Host: raft.localtest.me' http://localhost:18082/api/info | jq
```

응답의 `.server` 값이 `backend-a`, `backend-b`, `backend-c` 중 하나면 routing은 정상이다.

### 5. Leader write와 복제 확인

초기 상태에서는 `proxy-1`이 bootstrap leader다. dashboard API로 proxy config 전체를 저장해 새 upstream pool과 route를 추가한다.

```bash
curl -fsS http://localhost:19090/api/config |
  jq '{
    routes: (((.routes // []) | map(select(.id != "r-manual"))) + [{
      "id": "r-manual",
      "enabled": true,
      "match": {
        "hosts": ["raft-manual.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-manual"
    }]),
    upstream_pools: ((.upstream_pools // {}) + {
      "pool-manual": {
        "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
        "health_check": {
          "path": "/health",
          "interval": "5s",
          "timeout": "2s",
          "expect_status": 200
        }
      }
    })
  }' >/tmp/raft-default-config.json

curl -fsS -X PUT http://localhost:19090/api/config \
  -H 'Content-Type: application/json' \
  -d @/tmp/raft-default-config.json
```

모든 node에 route가 복제됐는지 확인한다.

```bash
curl -fsS http://localhost:19090/api/config | jq -e '.routes[] | select(.id == "r-manual")'
curl -fsS http://localhost:19091/api/config | jq -e '.routes[] | select(.id == "r-manual")'
curl -fsS http://localhost:19092/api/config | jq -e '.routes[] | select(.id == "r-manual")'
```

새 route로 routing이 되는지 확인한다.

```bash
curl -fsS -H 'Host: raft-manual.localtest.me' http://localhost:18080/api/info | jq
curl -fsS -H 'Host: raft-manual.localtest.me' http://localhost:18081/api/info | jq
curl -fsS -H 'Host: raft-manual.localtest.me' http://localhost:18082/api/info | jq
```

### 6. Follower write 거절 확인

`proxy-2` 또는 `proxy-3`이 follower라면 설정 쓰기 요청은 `409`로 거절된다. 실제 leader 선출 상태에 따라 둘 중 하나가 leader일 수 있으므로, 수동 테스트에서는 두 node 중 하나가 `409 not_raft_leader`를 반환하는지 확인한다.

```bash
curl -sS -o /tmp/raft-follower-write.json -w '%{http_code}\n' \
  -X PUT http://localhost:19091/api/config \
  -H 'Content-Type: application/json' \
  -d '{
    "routes": [{
      "id": "r-follower-rejected",
      "enabled": true,
      "match": {
        "hosts": ["raft-follower-rejected.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-raft"
    }],
    "upstream_pools": {
      "pool-raft": { "upstreams": ["backend-a:8080"] }
    }
  }'
```

```bash
jq . /tmp/raft-follower-write.json
```

status가 `2xx`라면 `proxy-2`가 현재 leader일 수 있다. 같은 요청의 URL만 `http://localhost:19092`로 바꿔 `proxy-3`에서도 확인한다.

기대 응답은 다음 조건을 만족해야 한다.

- HTTP status가 `409`다.
- JSON body의 `code`가 `not_raft_leader`다.
- 가능한 경우 `leader_address`가 포함된다.

`leader_address`는 Raft advertise address다. dashboard/admin HTTP URL이 아니므로 client가 그대로 retry URL로 사용하면 안 된다.

### 7. Join validation 확인

join API가 잘못된 `node_id`, `raft_address`를 거절하는지 확인한다.

```bash
curl -sS -o /tmp/raft-invalid-node-id.json -w '%{http_code}\n' \
  -X POST http://localhost:19090/api/cluster/join \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"bad:node","raft_address":"proxy-bad:7009"}'
```

```bash
jq . /tmp/raft-invalid-node-id.json
```

```bash
curl -sS -o /tmp/raft-invalid-address.json -w '%{http_code}\n' \
  -X POST http://localhost:19090/api/cluster/join \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"node-bad","raft_address":"not-a-host-port"}'
```

```bash
jq . /tmp/raft-invalid-address.json
```

기대 status는 `400`이고, code는 각각 `invalid_node_id`, `invalid_raft_address`다.

### 8. Leader 장애와 failover 확인

bootstrap leader인 `proxy-1`을 중단한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml stop proxy-1
```

잠시 대기한 뒤 `proxy-2`, `proxy-3` 중 하나에 config 저장 요청을 보낸다. 한 node가 leader라면 성공하고, follower라면 `409 not_raft_leader`가 반환된다. 아래 예시는 먼저 `proxy-2`에 요청한다.

```bash
curl -fsS http://localhost:19091/api/config |
  jq '{
    routes: (((.routes // []) | map(select(.id != "r-manual-failover"))) + [{
      "id": "r-manual-failover",
      "enabled": true,
      "match": {
        "hosts": ["raft-manual-failover.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-manual-failover"
    }]),
    upstream_pools: ((.upstream_pools // {}) + {
      "pool-manual-failover": {
        "upstreams": ["backend-a:8080", "backend-b:8080", "backend-c:8080"],
        "health_check": {
          "path": "/health",
          "interval": "5s",
          "timeout": "2s",
          "expect_status": 200
        }
      }
    })
  }' >/tmp/raft-failover-config.json

curl -sS -o /tmp/raft-failover-write.json -w '%{http_code}\n' \
  -X PUT http://localhost:19091/api/config \
  -H 'Content-Type: application/json' \
  -d @/tmp/raft-failover-config.json
```

status가 `409`면 같은 config body를 `http://localhost:19092`에 다시 보낸다. config 저장이 성공한 dashboard URL을 변수로 지정한다.

```bash
LEADER_DASHBOARD=http://localhost:19091
```

`proxy-3`에서 pool 생성이 성공했다면 다음처럼 바꾼다.

```bash
LEADER_DASHBOARD=http://localhost:19092
```

`proxy-2`, `proxy-3`에서 failover route가 보이고 routing이 되는지 확인한다.

```bash
curl -fsS http://localhost:19091/api/config | jq -e '.routes[] | select(.id == "r-manual-failover")'
curl -fsS http://localhost:19092/api/config | jq -e '.routes[] | select(.id == "r-manual-failover")'
curl -fsS -H 'Host: raft-manual-failover.localtest.me' http://localhost:18081/api/info | jq
curl -fsS -H 'Host: raft-manual-failover.localtest.me' http://localhost:18082/api/info | jq
```

### 9. 중단 node 재합류 확인

`proxy-1`을 다시 시작한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml up -d proxy-1
```

`proxy-1`이 failover 중 생성된 route를 catch-up 했는지 확인한다.

```bash
curl -fsS http://localhost:19090/api/config | jq -e '.routes[] | select(.id == "r-manual-failover")'
curl -fsS -H 'Host: raft-manual-failover.localtest.me' http://localhost:18080/api/info | jq
```

### 10. Persistence 확인

Raft volume을 유지한 상태로 proxy container를 stop/start 한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml stop proxy-1 proxy-2 proxy-3
docker compose -f composes/raft-ha-cluster/compose.yaml up -d proxy-1 proxy-2 proxy-3
```

재시작 후 이전에 생성한 route가 유지되는지 확인한다.

```bash
curl -fsS http://localhost:19090/api/config | jq -e '.routes[] | select(.id == "r-manual")'
curl -fsS http://localhost:19091/api/config | jq -e '.routes[] | select(.id == "r-manual")'
curl -fsS http://localhost:19092/api/config | jq -e '.routes[] | select(.id == "r-manual")'
```

## 정리

테스트가 끝나면 container와 volume을 함께 삭제한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml down -v --remove-orphans
```

`KEEP_RAFT_HA_SMOKE=1` 또는 고정 project name으로 실행한 smoke 환경은 해당 project name을 명시해서 정리한다.

```bash
docker compose -p loadbalancer-raft-ha-debug -f composes/raft-ha-cluster/compose.yaml down -v --remove-orphans
```

## 문제 해결

### Port가 이미 사용 중인 경우

다른 compose project나 local process가 `18080`-`18082`, `19090`-`19092`, `17001`-`17003`을 사용 중인지 확인한다.

```bash
docker ps
```

이전 smoke 환경을 보존한 경우에는 해당 project name으로 `down -v --remove-orphans`를 실행한다.

### 초기 route가 예상과 다른 경우

기존 Raft data volume이 남아 있으면 Raft log/snapshot의 상태가 복원된다. 빈 상태에서 초기 route 생성부터 다시 확인하려면 반드시 `down -v`로 volume을 삭제한다.

### Join node가 시작 직후 종료되는 경우

join node는 `/api/node/join-cluster` 요청의 `peers` 후보 중 하나 이상의 dashboard가 reachable 해야 한다. 수동으로 일부 service만 띄울 때는 기존 cluster의 leader 또는 leader가 될 수 있는 node가 healthy 상태가 된 뒤 새 node에 join 요청을 보낸다.

### Follower write가 예상과 다르게 성공하는 경우

해당 node가 현재 leader일 수 있다. Raft leader는 장애, 재시작, timing에 따라 바뀔 수 있으므로 `proxy-2`, `proxy-3` 중 다른 node에도 같은 쓰기 요청을 보내 follower 거절을 확인한다.

### Failover route 생성 시 `409`가 반환되는 경우

요청을 보낸 node가 follower다. surviving node 중 다른 dashboard URL에 동일 요청을 보내 leader를 찾는다.

### Docker image build가 실패하는 경우

`composes/raft-ha-cluster/.out/loadbalancer`와 `composes/raft-ha-cluster/.out/test-server`가 존재하는지 확인한다. 없으면 수동 빌드 명령을 다시 실행하거나 smoke script를 사용한다.

### 보안 전제

`/api/cluster/join`은 admin/control-plane endpoint다. 이 POC에는 내장 인증이 없으므로 compose 시나리오는 로컬 검증용으로만 사용한다. 운영 환경에서는 admin network 분리, 인증 proxy, network policy 같은 보호 장치가 필요하다.
