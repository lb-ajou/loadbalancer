# Dashboard API

`internal/dashboard`와 `internal/admin` 구현 기준으로, 대시보드가 사용하는 JSON API 계약을 정리한다.

## 개요

- 대시보드는 `go:embed`로 포함된 SPA HTML을 서빙한다.
- `/api/...` 요청은 JSON API로 처리한다.
- `/api/`가 아닌 `GET`/`HEAD` 요청은 같은 `index.html`을 반환한다.
- 프록시 설정은 클러스터 전체에 적용되는 단일 `spec.Config`로 관리한다.
- 설정 저장은 전체 config 교체 방식이며, HA mode에서는 하나의 Raft `replace_config` command로 합의된다.
- route와 upstream pool 개별 CRUD API는 없다. 클라이언트가 config를 편집한 뒤 `PUT /api/config`로 전체 저장한다.

관련 구현:

- `internal/dashboard/handler.go`
- `internal/dashboard/config_api.go`
- `internal/dashboard/runtime_api.go`
- `internal/dashboard/view.go`
- `internal/admin/service.go`

## API Set

남는 API:

- `GET /api/status`
- `GET /api/runtime`
- `GET /api/cluster`
- `POST /api/cluster/join`
- `GET /api/node/cluster-status`
- `POST /api/cluster/bootstrap`
- `POST /api/node/join-cluster`
- `GET /api/config`
- `PUT /api/config`

제거된 과거 설정 API는 호환 alias 없이 `404 Not Found`를 반환한다.

## Config API

### `GET /api/config`

현재 적용된 편집용 proxy config view를 반환한다.

```json
{
  "routes": [],
  "upstream_pools": {},
  "applied_at": "2026-06-09T00:00:00Z"
}
```

### `PUT /api/config`

클러스터 proxy config 전체를 교체한다.

요청 body:

```json
{
  "routes": [
    {
      "id": "r-api",
      "enabled": true,
      "algorithm": "round_robin",
      "match": {
        "hosts": ["api.example.com"],
        "path": { "type": "prefix", "value": "/api/" }
      },
      "upstream_pool": "pool-api"
    }
  ],
  "upstream_pools": {
    "pool-api": {
      "upstreams": ["10.0.0.11:8080"]
    }
  }
}
```

성공 응답은 `GET /api/config`와 같은 `ConfigView`다.

## Config Schema

`routes[]` 항목은 `spec.RouteConfig` 형식이다.

- `id`: route ID
- `enabled`: route 활성 여부
- `algorithm`: `round_robin`, `sticky_cookie`, `5_tuple_hash`, `least_connection` 중 하나. 생략하면 `round_robin`
- `match.hosts`: 최소 1개 이상의 exact host
- `match.path.type`: `exact`, `prefix`, `regex`
- `match.path.value`: `exact`/`prefix`는 `/`로 시작해야 하며, `prefix`는 `/` 또는 `/.../` 형태여야 한다.
- `upstream_pool`: `upstream_pools` object에 존재하는 pool ID

`upstream_pools`는 pool ID를 key로 하는 object다. pool 값 내부에는 `id` 필드가 없다.

```json
{
  "pool-api": {
    "upstreams": ["10.0.0.11:8080"],
    "health_check": {
      "path": "/health",
      "interval": "10s",
      "timeout": "3s",
      "expect_status": 200
    }
  }
}
```

## Runtime API

`GET /api/runtime`은 현재 runtime snapshot을 반환한다. 설정 관련 요약은 단일 config 기준이다.

```json
{
  "applied_at": "2026-06-09T00:00:00Z",
  "config": {
    "route_count": 1,
    "upstream_pool_count": 1
  },
  "routes": [
    {
      "id": "r-api",
      "enabled": true,
      "hosts": ["api.example.com"],
      "path": { "kind": "prefix", "value": "/api/" },
      "algorithm": "round_robin",
      "upstream_pool": "pool-api"
    }
  ],
  "upstreams": [
    {
      "id": "pool-api",
      "targets": [
        {
          "address": "10.0.0.11:8080",
          "healthy": true,
          "active_connections": 0
        }
      ]
    }
  ]
}
```

## Error Shape

API 오류는 다음 shape를 사용한다.

```json
{
  "message": "validation failed",
  "code": "invalid_config",
  "leader_address": "http://leader:9090",
  "errors": [
    {
      "field": "routes[0].upstream_pool",
      "message": "unknown upstream pool"
    }
  ]
}
```

`leader_address`와 `errors`는 상황에 따라 생략된다.
