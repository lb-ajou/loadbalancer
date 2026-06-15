# L7 Load Balancer

Raft 기반 상태 저장소와 VIP failover를 포함한 L7 로드밸런서입니다.

이 프로젝트는 단일 프록시 프로세스를 넘어서, 여러 노드가 같은 프록시 설정을 공유하고 리더 장애 시 VIP를 이전해 서비스를 계속 제공하는 구조를 제공합니다. 프록시 route/upstream 설정은 파일이 아니라 Admin API와 Dashboard를 통해 저장되며, 저장된 desired state는 Raft log/snapshot을 통해 복제됩니다.

## 주요 기능

- HTTP 로드밸런싱
- Host/path 기반 route 매칭
- 클러스터 단일 프록시 설정 관리
- Upstream pool과 health check
- `round_robin`, `sticky_cookie`, `5_tuple_hash`, `least_connection` 라우팅 알고리즘
- Dashboard UI와 JSON API
- Raft 기반 설정 복제
- 3-node HA cluster bootstrap/join
- Leader 기반 VIP 소유권 이전
- Docker Compose 기반 로컬 검증 시나리오
- wrk, vegeta, k6 기반 benchmark 보조 스크립트

## 동작 방식

프로젝트의 핵심은 desired state와 runtime state를 분리하는 것입니다.

```text
configs/app.json
    -> internal/boot.AppConfig
    -> internal/app
    -> Raft log/snapshot DesiredState
    -> internal/state.ProjectSnapshot()
    -> internal/route.BuildTable()
    -> internal/upstream.BuildRegistry()
    -> runtime.Snapshot
    -> runtime.State
    -> proxy.Handler / dashboard.Handler
```

`configs/app.json`은 프로세스가 어떻게 뜰지를 정하는 노드 로컬 부트 설정입니다. 프록시 route/upstream 설정은 여기에 두지 않습니다.

프록시 설정은 Dashboard 또는 Admin API로 저장합니다. 저장 요청은 검증을 거친 뒤 Raft command로 합의되고, 각 노드는 적용된 desired state를 route table, upstream registry, health state가 포함된 runtime snapshot으로 변환합니다. 실제 요청 처리는 항상 현재 runtime snapshot을 기준으로 수행됩니다.

## 프로젝트 구조

```text
.
├── configs/                  # 프로세스 부트 설정
├── internal/
│   ├── admin/                # desired state 저장/삭제 서비스
│   ├── app/                  # 앱 wiring, 서버 lifecycle, Raft/VIP 연결
│   ├── boot/                 # app.json 로드, 기본값, 검증
│   ├── cli/                  # serve, cluster status/bootstrap/join CLI
│   ├── dashboard/            # Dashboard UI와 JSON API
│   ├── proxy/                # HTTP 요청 전달 handler
│   ├── raft/                 # Raft node, store, FSM, snapshot
│   ├── config/               # 계층 간 공유되는 실행 설정 DTO
│   ├── route/                # route compile, sort, match, resolve
│   ├── runtime/              # 적용된 runtime snapshot/state
│   ├── spec/                 # proxy desired config schema와 validation
│   ├── state/                # project desired state와 snapshot 변환
│   ├── upstream/             # upstream registry, health, balancer
│   └── vip/                  # VIP controller, ARP, netlink
├── composes/                 # 로컬 검증용 Docker Compose 시나리오
├── docs/                     # 아키텍처, API, 컨벤션 문서
├── scripts/                  # HA smoke test 스크립트
└── tools/                    # 라우팅 검증과 benchmark 보조 스크립트
```

더 자세한 구조 설명은 `docs/architecture/architecture.ko.md`와 `docs/conventions/directory-convention.ko.md`를 참고하세요.

## 요구 사항

- Go `1.26.3`
- Docker, Docker Compose
- Linux 환경 또는 Linux 컨테이너 환경

VIP 기능은 Linux netlink와 ARP를 사용합니다. macOS/Windows에서 일반 프록시와 Dashboard는 실행할 수 있지만, 실제 VIP 제어는 Linux 환경에서 검증하는 것을 권장합니다.

## 실행

기본 설정 파일은 `configs/app.json`입니다.

```json
{
  "proxyListenAddr": ":8080",
  "dashboardListenAddr": ":9090",
  "raftDataDir": "configs/data"
}
```

로컬 실행:

```bash
go run . serve configs/app.json
```

또는 기본 경로를 사용할 수 있습니다.

```bash
go run .
```

기본 포트:

- Proxy: `http://localhost:8080`
- Dashboard: `http://localhost:9090`

## 빌드

로컬 바이너리:

```bash
go build -o loadbalancer ./main.go
./loadbalancer serve configs/app.json
```

Docker 이미지:

```bash
docker build -t loadbalancer .
docker run --rm \
  -p 8080:8080 \
  -p 9090:9090 \
  -v "$PWD/configs:/configs:ro" \
  loadbalancer serve /configs/app.json
```

## 테스트

전체 Go 테스트:

```bash
go test ./...
```

기본 compose 시나리오:

```bash
docker compose -f composes/route-basic/compose.yaml up -d
go run . serve configs/app.json
```

백엔드 직접 확인:

```bash
curl http://localhost:18081/api/info
curl http://localhost:18082/api/info
```

프록시 확인:

```bash
curl -H 'Host: alpha.localtest.me' http://localhost:8080/api/info
curl -H 'Host: beta.localtest.me' http://localhost:8080/api/info
```

라우팅 알고리즘별 검증 스크립트:

```bash
tools/round-robin-check.sh
tools/sticky-cookie-check.sh
tools/5-tuple-hash-check.sh
tools/least-connection-check.sh
```

각 compose 시나리오의 자세한 설정과 기대 결과는 `composes/` 아래 README를 참고하세요.

## 설정 API

Dashboard API는 클러스터 전체에 적용되는 단일 proxy desired state를 관리합니다.

주요 endpoint:

- `GET /api/status`
- `GET /api/runtime`
- `GET /api/cluster`
- `GET /api/config`
- `PUT /api/config`
- `POST /api/cluster/bootstrap`
- `POST /api/node/join-cluster`
- `GET /api/node/cluster-status`

예시 route/upstream 설정:

```json
{
  "routes": [
    {
      "id": "r-api",
      "enabled": true,
      "algorithm": "round_robin",
      "match": {
        "hosts": ["api.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-api"
    }
  ],
  "upstream_pools": {
    "pool-api": {
      "upstreams": ["127.0.0.1:18081", "127.0.0.1:18082"],
      "health_check": {
        "path": "/health",
        "interval": "10s",
        "timeout": "3s",
        "expect_status": 200
      }
    }
  }
}
```

저장:

```bash
curl -X PUT http://localhost:9090/api/config \
  -H 'Content-Type: application/json' \
  --data @config.json
```

API 계약의 자세한 내용은 `docs/api/dashboard-api.ko.md`를 참고하세요.

## 클러스터와 VIP

클러스터는 실행 중인 노드에 lifecycle API 또는 CLI로 bootstrap/join합니다. clean node는 자동으로 cluster를 만들지 않고, 명시적인 bootstrap 또는 join 요청을 기다립니다.

상태 확인:

```bash
./loadbalancer cluster status --dashboard http://localhost:9090
```

첫 노드 bootstrap:

```bash
./loadbalancer cluster bootstrap \
  --dashboard http://node1:9090 \
  --node-id node-1 \
  --raft-advertise node1:7000 \
  --vip-interface eth0 \
  --vip-address 10.0.0.100/24
```

다른 노드 join:

```bash
./loadbalancer cluster join \
  --dashboard http://node2:9090 \
  --node-id node-2 \
  --raft-advertise node2:7000 \
  --vip-interface eth0 \
  --peer http://node1:9090
```

로컬 HA smoke test:

```bash
scripts/raft-ha-cluster-smoke.sh
scripts/raft-ha-vip-smoke.sh
```

Raft HA와 VIP 검증 절차는 `docs/architecture/raft-ha-test-guide.ko.md`와 `docs/architecture/raft-ha-vip-test-guide.ko.md`를 참고하세요.

## 성능 및 장애 전환 측정

이 저장소에는 리버스 프록시 처리량을 측정하기 위한 wrk, vegeta, k6 보조 스크립트가 포함되어 있습니다.

```bash
tools/benchmark-wrk.sh
tools/benchmark-vegeta.sh
tools/benchmark-k6.sh
tools/benchmark-matrix.sh
```

벤치마크 구성은 `composes/benchmark-check/`와 `docs/architecture/benchmark-playbook.ko.md`를 참고하세요.

HA 관련 측정 결과는 OpenStack 3노드 환경 기준입니다.

| 항목 | 비교 대상 | 측정 결과 |
| --- | --- | --- |
| VIP failover | keepalived 평균 `4.21s` | Raft 구조 평균 `3.78s`, 최소 `3.77s`, 최대 `4.91s` |
| 상태 전파 시간 | Syncthing 평균 `9.51s`, CephFS 평균 `0.40s` | Raft 구조 평균 `1.76s` |

Failover 측정은 leader 장애 이후 새로운 leader가 VIP를 획득하고 서비스가 회복되는 시간을 기준으로 봅니다. 상태 전파 시간은 설정 변경이 다른 노드의 runtime state에 반영되는 데 걸리는 시간을 기준으로 측정했습니다.

## 추가 문서

- `docs/architecture/architecture.ko.md`: 전체 아키텍처와 runtime state 흐름
- `docs/api/dashboard-api.ko.md`: Dashboard/Admin API 계약
- `docs/conventions/type-reference.ko.md`: 주요 타입 의미
- `docs/architecture/routing-algorithm-playbook.ko.md`: 라우팅 알고리즘 추가 절차
- `docs/architecture/raft-config-state.ko.md`: Raft 기반 설정 상태 모델
- `docs/architecture/benchmark-playbook.ko.md`: 벤치마크 실행 기준
