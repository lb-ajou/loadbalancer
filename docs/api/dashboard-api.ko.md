# Dashboard API

`internal/dashboard`와 `internal/admin` 구현 기준으로, 대시보드가 사용하는 namespace 기반 설정 API를 정리한 문서다.

## 개요

- 대시보드는 SPA HTML 한 장을 `go:embed`로 포함해 서빙한다.
- `/api/...` 요청은 JSON API로 처리한다.
- `/api/`가 아닌 `GET`/`HEAD` 요청은 모두 같은 `index.html`을 반환한다.
- 설정 편집은 `internal/dashboard/config_api.go`가 받고, desired state 저장과 런타임 반영은 `internal/admin/service.go`가 내부 `stateStore` 포트를 통해 처리한다.
- 런타임 조회는 `internal/dashboard/runtime_api.go`가 담당한다.
- 내부 runtime 모델의 Raft identity 표현은 API 계약과 분리되어 있으며, public JSON field 이름과 error shape를 바꾸지 않는다.
- 내부 runtime snapshot의 VIP 표현은 API 계약과 분리되어 있으며, `/api/status`의 `vip` 응답 shape를 바꾸지 않는다.

관련 구현:

- `internal/dashboard/handler.go`
- `internal/dashboard/config_api.go`
- `internal/dashboard/runtime_api.go`
- `internal/dashboard/view.go`
- `internal/admin/service.go`

## Namespace 모델

namespace는 proxy desired config의 논리 단위다.

- namespace는 Raft log/snapshot의 `DesiredState.Namespaces` key와 1:1로 대응한다.
- `path` 필드는 호환성을 위해 남아 있지만 로컬 파일 경로가 아니다. 현재 응답은 `raft://namespaces/default`처럼 Raft desired state namespace를 가리키는 metadata를 사용한다.

즉 namespace는 UI상의 임의 그룹이 아니라, 실제 편집 대상 proxy desired config다.

규칙:

- route와 upstream pool은 namespace 내부 리소스다.
- route의 `upstream_pool`은 같은 namespace 안의 pool ID를 참조해야 한다.
- 저장은 해당 namespace desired config에만 반영된다.
- 저장은 Raft command로 합의되고 FSM apply/restore callback이 runtime snapshot을 다시 만든다.

namespace 이름은 `^[A-Za-z0-9._-]+$`만 허용한다.

## 기본 동작

### `default` namespace

`GET /api/namespaces`는 `default` namespace desired config가 없어도 항상 `default`를 목록에 넣는다.

이 경우 응답 항목은 다음처럼 내려간다.

- `namespace: "default"`
- `exists: false`

### 존재하지 않는 namespace 조회

`GET /api/namespaces/{namespace}/config`는 namespace desired config가 없어도 `404`를 주지 않는다. 대신 빈 설정 뷰를 반환한다.

예:

```json
{
  "namespace": "default",
  "exists": false,
  "routes": [],
  "upstream_pools": {},
  "applied_at": "2026-04-13T00:00:00Z"
}
```

여기서 `applied_at`은 파일 시간이 아니라 현재 런타임 스냅샷의 적용 시각이다.

### 존재하지 않는 namespace에 대한 첫 저장

`PUT /api/namespaces/{namespace}/config`는 namespace desired config가 없어도 동작할 수 있다. 서비스 계층이 빈 설정에서 시작해 namespace desired config 전체를 새로 저장한다.

### 명시적 namespace 생성

`POST /api/namespaces`는 빈 namespace desired config를 미리 만들고 싶을 때 쓰는 API다. 필수는 아니지만, 사이드바의 "새 namespace 만들기" 같은 UI에는 직접 연결하기 좋다.

## Config API

모든 편집 API는 namespace 기준으로 동작한다.

## 최종 API Set

남는 API:

- `GET /api/status`
- `GET /api/runtime`
- `GET /api/cluster`
- `POST /api/cluster/join`
- `GET /api/node/cluster-status`
- `POST /api/cluster/bootstrap`
- `POST /api/node/join-cluster`
- `GET /api/namespaces`
- `POST /api/namespaces`
- `DELETE /api/namespaces/{namespace}`
- `GET /api/namespaces/{namespace}/config`
- `PUT /api/namespaces/{namespace}/config`

제거된 API는 호환 alias 없이 `404 Not Found`를 반환한다.

- `GET /api/runtime/config`
- `GET /api/app-config`
- `GET /api/proxy-configs`
- `GET /api/runtime/routes`
- `GET /api/upstreams`
- `POST /api/raft/join`
- `/api/namespaces/{namespace}/routes*`
- `/api/namespaces/{namespace}/upstream-pools*`

### Namespace 목록/생성/삭제

- `GET /api/namespaces`
- `POST /api/namespaces`
- `DELETE /api/namespaces/{namespace}`

`GET /api/namespaces` 응답:

```json
{
  "items": [
    {
      "namespace": "default",
      "path": "raft://namespaces/default",
      "exists": true,
      "route_count": 1,
      "upstream_pool_count": 1
    }
  ],
  "default_namespace": "default"
}
```

`POST /api/namespaces` 요청:

```json
{
  "namespace": "admin"
}
```

동작:

- 빈 namespace desired config를 저장한다.
- namespace 생성 command가 Raft log에 합의된 뒤 FSM apply로 런타임에 반영된다.

`DELETE /api/namespaces/{namespace}` 동작:

- 대상 namespace desired config를 삭제한다.
- namespace 삭제 command가 Raft log에 합의된 뒤 FSM apply로 런타임에 반영된다.

### Namespace 설정 조회/전체 저장

- `GET /api/namespaces/{namespace}/config`
- `PUT /api/namespaces/{namespace}/config`

이 응답은 편집용 원본 config 뷰다. `PUT`은 같은 구조의 `routes`, `upstream_pools`를 받아 namespace desired config 전체를 저장한다. HA mode에서는 이 전체 저장이 하나의 Raft command로 합의된다.

```json
{
  "namespace": "default",
  "exists": true,
  "routes": [],
  "upstream_pools": {},
  "applied_at": "2026-04-13T00:00:00Z"
}
```

`PUT` 요청 body:

```json
{
  "routes": [],
  "upstream_pools": {}
}
```

route/upstream-pool 개별 CRUD API는 제거됐다. route와 pool 생성, 수정, 삭제는 `GET /api/namespaces/{namespace}/config`로 현재 config를 읽고, 클라이언트 상태에서 변경한 뒤, `PUT /api/namespaces/{namespace}/config`로 namespace desired config 전체를 저장한다.

### Route Config

`routes[]` 항목은 `spec.RouteConfig` 형식이다.

```json
{
  "id": "r-api",
  "enabled": true,
  "algorithm": "sticky_cookie",
  "match": {
    "hosts": ["api.example.com"],
    "path": {
      "type": "prefix",
      "value": "/api/"
    }
  },
  "upstream_pool": "pool-api"
}
```

주의:

- route ID 중복은 config validation 오류다.
- `algorithm`은 `round_robin`, `sticky_cookie`, `5_tuple_hash`, `least_connection` 중 하나다.
- 생략하면 기본값은 `round_robin`이다.

검증 규칙 중 프론트에서 바로 알아야 할 부분:

- `match.hosts`는 최소 1개 이상이어야 한다.
- host는 빈 문자열일 수 없다.
- wildcard host는 현재 스키마에서 지원하지 않는다.
- `path.type`은 `exact`, `prefix`, `regex`만 허용한다.
- `exact`와 `prefix`는 `/`로 시작해야 한다.
- `prefix`는 `/` 또는 `/.../` 형태여야 한다.
- `algorithm`은 `round_robin`, `sticky_cookie`, `5_tuple_hash`, `least_connection`만 허용한다.
- `upstream_pool`은 같은 namespace 안에 이미 정의된 pool이어야 한다.

### Upstream Pool Config

`upstream_pools`는 pool ID를 key로 하는 object다.

```json
{
  "id": "pool-api",
  "upstreams": ["10.0.0.11:8080"],
  "health_check": {
    "path": "/health",
    "interval": "30s",
    "timeout": "3s",
    "expect_status": 200
  }
}
```

각 pool 값은 `spec.UpstreamPool` 형식이다.

```json
{
  "upstreams": ["10.0.0.11:8080"],
  "health_check": {
    "path": "/health",
    "interval": "30s",
    "timeout": "3s",
    "expect_status": 200
  }
}
```

검증 규칙:

- `upstreams`는 최소 1개 이상이어야 한다.
- 각 upstream은 `host:port` 또는 `[ipv6]:port` 형식이어야 한다.
- `health_check.path`는 `/`로 시작해야 한다.
- `health_check.interval`, `health_check.timeout`은 Go duration 문자열이어야 한다. 예: `30s`, `1m`
- `health_check.expect_status`는 `100..599` 범위여야 한다.

삭제 제약:

- 해당 pool을 참조하는 route가 남아 있으면 config validation 오류다. pool 삭제와 참조 route 변경/삭제를 같은 `PUT /api/namespaces/{namespace}/config` 요청에 함께 담는다.

## Runtime API

runtime API는 편집용 API가 아니라, 현재 프로세스에 적용된 활성 스냅샷과 로컬 관측값 조회용 API다.

- `GET /api/status`
- `GET /api/runtime`
- `GET /api/cluster`

제거된 legacy 조회 API는 호환 alias 없이 `404 Not Found`를 반환한다.

- `GET /api/runtime/config`
- `GET /api/runtime/routes`
- `GET /api/app-config`
- `GET /api/proxy-configs`
- `GET /api/upstreams`

`GET /api/status`는 현재 노드의 짧은 운영 요약이다.

```json
{
  "node": {
    "id": "node-1",
    "config_store": "raft",
    "proxy_listen_addr": ":8080",
    "dashboard_listen_addr": ":9090",
    "applied_at": "2026-05-24T12:00:00Z",
    "projection": {
      "status": "ok"
    }
  },
  "raft": {
    "enabled": true,
    "state": "leader",
    "is_leader": true,
    "leader_id": "node-1",
    "leader_address": "127.0.0.1:7001",
    "has_leader": true,
    "quorum_status": "available"
  },
  "vip": {
    "enabled": true,
    "interface": "eth0",
    "address": "172.30.10.100/24",
    "owned": true
  },
  "runtime": {
    "route_count": 1,
    "upstream_pool_count": 1,
    "target_count": 3,
    "healthy_target_count": 3,
    "unhealthy_target_count": 0
  }
}
```

`vip.enabled`는 응답 호환 필드다. 설정 입력에는 `enabled` 스위치가 없으며, cluster-wide VIP address가 있으면 VIP가 활성인 것으로 본다. `vip.interface`는 응답한 노드의 local interface다.

`GET /api/runtime`은 현재 노드에 적용된 route table, upstream pool, target별 로컬 health 상태를 함께 반환한다.

```json
{
  "applied_at": "2026-05-24T12:00:00Z",
  "node": {
    "id": "node-1",
    "config_store": "raft"
  },
  "config_sources": [
    {
      "source": "default",
      "path": "raft://namespaces/default",
      "name": "default",
      "route_count": 1,
      "upstream_pool_count": 1
    }
  ],
  "routes": [
    {
      "global_id": "default:r-api",
      "local_id": "r-api",
      "source": "default",
      "enabled": true,
      "hosts": ["api.example.com"],
      "path": {
        "kind": "prefix",
        "value": "/"
      },
      "algorithm": "sticky_cookie",
      "upstream_pool": "default:pool-api"
    }
  ],
  "upstreams": [
    {
      "global_id": "default:pool-api",
      "local_id": "pool-api",
      "source": "default",
      "targets": [
        {
          "address": "10.0.0.11:8080",
          "healthy": true,
          "active_connections": 0
        }
      ],
      "health_check": {
        "path": "/health",
        "interval": "30s",
        "timeout": "3s",
        "expect_status": 200
      }
    }
  ]
}
```

`GET /api/cluster`는 Raft cluster 상태와 membership을 반환한다. `leader.address`는 dashboard/admin HTTP URL이 아니라 Raft advertise address다.

```json
{
  "enabled": true,
  "quorum_status": "available",
  "leader": {
    "id": "node-1",
    "address": "127.0.0.1:7001"
  },
  "local": {
    "id": "node-1",
    "address": "127.0.0.1:7001",
    "state": "leader",
    "last_log_index": "12",
    "commit_index": "12",
    "applied_index": "12",
    "term": "2"
  },
  "members": [
    {
      "id": "node-1",
      "address": "127.0.0.1:7001",
      "role": "voter",
      "is_leader": true
    }
  ],
  "raft_timing": {
    "heartbeat_timeout": "3s",
    "election_timeout": "5s",
    "leader_lease_timeout": "2s",
    "commit_timeout": "250ms"
  }
}
```

`raft_timing`은 cluster bootstrap에서 설정된 경우에만 포함된다. Join node는 `/api/node/join-cluster` 처리 중 peer 후보의 `GET /api/cluster`를 먼저 조회해 이 값을 local Raft start config에 반영한다.

route 조회 응답에는 `algorithm` 필드가 포함된다.

예:

```json
[
  {
    "global_id": "default:r-api",
    "local_id": "r-api",
    "source": "default",
    "enabled": true,
    "hosts": ["api.example.com"],
    "path": {
      "kind": "prefix",
      "value": "/"
    },
    "algorithm": "sticky_cookie",
    "upstream_pool": "default:pool-api"
  }
]
```

`sticky_cookie` 동작:

- 첫 요청은 pool의 현재 round-robin 선택 결과를 사용한다.
- 응답 시 route 단위 cookie를 내려준다.
- 이후 같은 cookie를 가진 요청은 같은 upstream target을 재사용한다.
- cookie가 가리키는 target이 unhealthy면 다른 healthy target을 선택하고 cookie를 갱신한다.

`5_tuple_hash` 동작:

- client 식별은 `Forwarded`의 `for=` 값 또는 `X-Forwarded-For` 첫 번째 값을 우선 사용한다.
- 신뢰 헤더가 없으면 `RemoteAddr`를 사용한다.
- `protocol`, client address, client port, destination host, destination port를 조합해 해시하고 healthy target 중 하나를 고른다.

`least_connection` 동작:

- 실제 backend TCP connection 수가 아니라 target별 in-flight 프록시 요청 수를 기준으로 한다.
- in-flight는 아직 종료되지 않은 일반 HTTP 요청, 스트리밍 응답, websocket 업그레이드 연결을 모두 포함한다.
- healthy target 중 현재 in-flight 수가 가장 적은 target을 고른다.
- 동률이면 round-robin 순서로 결정한다.

차이:

- `GET /api/namespaces/{namespace}/config`
  현재 namespace desired config의 편집용 원본 구조를 반환한다.
- `GET /api/runtime`
  현재 노드 메모리에 적용된 전체 route table을 반환한다.

즉 config API는 "desired config 편집", runtime API는 "현재 적용 상태 확인" 용도다.

주요 응답 형태:

- `/api/status`: `node`, `raft`, `vip`, `runtime`
- `/api/runtime`: `applied_at`, `node`, `config_sources`, `routes`, `upstreams`
- `/api/cluster`: `enabled`, `quorum_status`, `leader`, `local`, `members`

`GET /api/runtime` 응답의 `upstreams[].targets[]` health 상태와 active connection 수는 Raft 복제 상태가 아니라 응답한 노드의 로컬 관측값이다.

## 저장과 적용

설정 변경 흐름은 `internal/admin/service.go`에 모여 있다. Dashboard handler는 HTTP 입출력만 담당하고, Raft-backed source of truth와 런타임 반영은 서비스 계층과 `stateStore` 구현이 담당한다.

흐름:

1. namespace, route, upstream pool 요청을 `stateStore` operation으로 변환한다.
2. Raft-backed store가 leader 여부를 확인한다.
3. write 요청을 Raft command로 encode하고 `raft.Apply`를 호출한다.
4. FSM apply 또는 restore callback이 `DesiredState`를 runtime snapshot으로 projection하고 `runtime.State`를 교체한다.
5. namespace 이름, 리소스 충돌, 전체 config validation 오류는 기존 API error shape로 반환한다.
6. 성공하면 같은 API response shape로 namespace/config view를 반환한다.

`configStore`는 더 이상 로컬 설정 입력이 아니다. API 응답의 `config_store` 필드는 호환성을 위해 남아 있으며 값은 `"raft"`다. 프록시 route/upstream JSON은 앱 부팅 입력이 아니며, 설정 생성과 변경은 Admin API 쓰기를 통해 Raft log에 기록된다.

## 요청/에러 규칙

JSON body는 다음 규칙으로 파싱한다.

- unknown field를 허용하지 않는다.
- body는 JSON 객체 하나만 포함해야 한다.

에러 응답은 `admin.APIError` 형식이다.

```json
{
  "message": "validation failed",
  "errors": [
    {
      "field": "routes[0].id",
      "message": "duplicate route id"
    }
  ]
}
```

주요 상태 코드:

- `400 Bad Request`
  잘못된 JSON, 잘못된 namespace 형식
- `404 Not Found`
  존재하지 않는 namespace 삭제, 존재하지 않는 API 경로, 제거된 legacy API 호출
- `405 Method Not Allowed`
  지원하지 않는 HTTP 메서드
- `409 Conflict`
  namespace 중복 생성, HA mode follower write
- `422 Unprocessable Entity`
  config validation 실패
- `500 Internal Server Error`
  내부 상태 투영 실패 또는 예상하지 못한 서버 오류

## HA 모드 오류

설정 쓰기 요청이 follower에 도착하면 첫 구현은 leader forward를 하지 않는다.
응답은 `409 Conflict`이며 body는 다음 형태다.

```json
{
  "message": "configuration writes must be sent to the raft leader",
  "code": "not_raft_leader",
  "leader_address": "127.0.0.1:9090"
}
```

`leader_address`는 HashiCorp Raft가 보고한 leader의 Raft advertise address다. dashboard/admin HTTP URL과 다를 수 있으므로, 별도 매핑이 없는 클라이언트나 운영자는 직접 재시도 URL이 아니라 leader 식별 힌트로만 사용한다.

## Raft Lifecycle API

clean node는 기존 Raft data dir이 없으면 cluster를 자동으로 만들지 않고 dashboard/control-plane만 띄운다. 이 상태에서 설정 쓰기 API는 `409 Conflict`, `code: "cluster_not_configured"`를 반환한다.

웹/CLI는 다음 API로 현재 노드가 bootstrap/join 가능한지 확인한다.

```http
GET /api/node/cluster-status
```

```json
{
  "state": "unconfigured",
  "raft_data_dir": "data/raft",
  "has_raft_state": false,
  "raft_running": false,
  "can_bootstrap": true,
  "can_join": true
}
```

`state`는 `unconfigured`, `clustered`, `existing_state` 중 하나다. clean node에서는 `node_id`와 `raft_advertise_addr`가 아직 없을 수 있다. `last_error`가 있으면 Raft data dir 검사 중 오류가 발생한 것이다.

리더로 시작할 노드는 다음 API로 cluster를 만든다. VIP를 사용하는 경우 bootstrap 요청에서 cluster-wide VIP address를 함께 제공한다.

```http
POST /api/cluster/bootstrap
Content-Type: application/json
```

```json
{
  "node_id": "node-1",
  "raft_bind_addr": "0.0.0.0:7001",
  "raft_advertise_addr": "10.0.0.11:7001",
  "raft_timing": {
    "heartbeat_timeout": "3s",
    "election_timeout": "5s",
    "leader_lease_timeout": "2s",
    "commit_timeout": "250ms"
  },
  "vip_interface": "eth0",
  "vip": {
    "address": "10.0.0.100/24",
    "garp_count": 3,
    "garp_interval": "100ms",
    "acquire_delay": "300ms",
    "release_on_shutdown": true
  }
}
```

`raft_timing`은 선택 필드다. 제공하면 leader node를 해당 timing으로 시작하고, 같은 값을 Raft desired state에 기록해 cluster-wide timing 정책으로 복제한다. duration 값은 Go duration 문자열이다.

`vip`은 선택 필드다. 제공하면 `address`는 필수이며 `garp_count`, `garp_interval`, `acquire_delay`, `release_on_shutdown`은 선택 필드다. 생략한 VIP policy 값은 bootstrap 처리 중 기본값으로 정규화되어 Raft desired state에 저장된다. 기본값은 `garp_count: 5`, `garp_interval: "200ms"`, `acquire_delay: "500ms"`이고 `release_on_shutdown`은 명시하지 않으면 `false`다.

join할 노드는 자기 local Raft address와 VIP interface, 기존 cluster admin URL 후보를 제공한다. join node는 Raft node를 시작하기 전에 peer 후보의 `GET /api/cluster`를 조회해 cluster-wide Raft timing을 가져온다.

```http
POST /api/node/join-cluster
Content-Type: application/json
```

```json
{
  "node_id": "node-2",
  "raft_bind_addr": "0.0.0.0:7002",
  "raft_advertise_addr": "10.0.0.12:7002",
  "vip_interface": "eth0",
  "peers": [
    "http://10.0.0.11:9090",
    "http://10.0.0.12:9090"
  ]
}
```

두 API 모두 성공 시 `204 No Content`를 반환한다. 이미 Raft node가 실행 중이거나 기존 Raft state가 있으면 `409 Conflict`, `code: "cluster_already_configured"`를 반환한다. `raft_bind_addr`를 생략하면 `raft_advertise_addr`의 port를 사용해 `0.0.0.0:<port>`로 bind한다. 성공한 bootstrap/join 요청의 `node_id`, `raft_bind_addr`, `raft_advertise_addr`는 Raft data dir의 node-local metadata에 저장되며, 기존 Raft state가 있는 재시작 경로에서 우선 사용된다.

같은 흐름은 CLI에서도 사용할 수 있다. CLI는 파일을 수정하지 않고 대상 노드의 dashboard API만 호출한다.

```bash
reverseproxy cluster status \
  --dashboard http://10.0.0.11:9090

reverseproxy cluster bootstrap \
  --dashboard http://10.0.0.11:9090 \
  --node-id node-1 \
  --raft-advertise 10.0.0.11:7001 \
  --raft-heartbeat-timeout 3s \
  --raft-election-timeout 5s \
  --vip-interface eth0 \
  --vip-address 10.0.0.100/24

reverseproxy cluster join \
  --dashboard http://10.0.0.12:9090 \
  --node-id node-2 \
  --raft-advertise 10.0.0.12:7002 \
  --vip-interface eth0 \
  --peer http://10.0.0.11:9090
```

웹 대시보드는 `/cluster-lifecycle`에서 같은 API를 호출하는 운영 화면을 제공한다. 이 화면도 local config 파일이나 프록시 설정 JSON을 수정하지 않고, 현재 노드의 lifecycle API에만 요청을 보낸다.

## Raft Membership Join API

`/api/cluster/join`은 leader가 받는 membership add API다. 사용자가 join node에 직접 호출하는 API는 `/api/node/join-cluster`다. join node는 peer 후보를 순회하면서 leader의 `/api/cluster/join`에 아래 요청을 보낸다.

```http
POST /api/cluster/join
Content-Type: application/json
```

```json
{
  "node_id": "node-2",
  "raft_address": "127.0.0.1:7002"
}
```

성공 시 `204 No Content`를 반환한다. 요청을 받은 노드가 leader가 아니면 설정 쓰기와 같은 `409 Conflict`, `code: "not_raft_leader"`, `leader_address` 응답을 반환한다. 시작 join 흐름은 해당 응답도 실패 후보로 기록하고 다음 join 주소를 계속 시도한다.

`/api/raft/join`은 제거됐으며 `404 Not Found`를 반환한다. Raft membership add 경로는 `/api/cluster/join`만 사용한다. 사용자가 새 노드를 클러스터에 가입시킬 때 호출하는 lifecycle 경로는 `/api/node/join-cluster`다.

`/api/cluster/join`은 admin/control-plane endpoint다. 이 POC에는 내장 인증이 없으므로 보호된 admin network에만 노출하거나 외부 인증, network policy 뒤에 둔다.

런타임 health 상태는 Raft 복제 상태가 아니라 응답한 노드의 로컬 관측값이다.

## 프론트엔드에서 바로 쓰는 흐름

대시보드 UI는 아래 순서로 붙이면 된다.

1. `GET /api/namespaces`로 목록을 가져온다.
2. 현재 선택 namespace를 전역 상태로 관리한다.
3. 편집 조회는 `GET /api/namespaces/{namespace}/config`를 사용한다.
4. route/pool 생성, 수정, 삭제는 클라이언트 상태에서 수행한 뒤 `PUT /api/namespaces/{namespace}/config`로 전체 namespace config를 저장한다.
5. 새 namespace를 미리 만들고 싶으면 `POST /api/namespaces`를 사용한다.
6. namespace 제거는 `DELETE /api/namespaces/{namespace}`를 사용한다.

## 검증 범위

현재 동작은 최소한 아래 테스트로 확인된다.

- `internal/dashboard/api_test.go`
  handler 레벨의 라우팅, 상태 코드, JSON 응답 형식
- `internal/admin/service_test.go`
  Raft-backed namespace write, rollback, namespace 기본 규칙
