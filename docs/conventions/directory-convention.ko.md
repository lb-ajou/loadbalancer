# Directory Convention

이 문서는 주요 디렉터리의 책임과 상태 흐름을 설명한다.

## 핵심 흐름

```text
Admin API
  -> internal/admin.Service
  -> internal/raft.Store or local state store
  -> state.DesiredState.ProxyConfig
  -> state.ProjectSnapshot()
  -> route.BuildTable(spec.Config)
  -> upstream.BuildRegistry(spec.Config)
  -> runtime.Snapshot
  -> proxy / dashboard read path
```

프록시 설정은 클러스터 전체에 적용되는 단일 `spec.Config`다. route와 upstream pool은 같은 config 안에서 ID로 직접 참조한다.

## 디렉터리 책임

### `internal/spec`

편집용 proxy desired config schema와 validation을 정의한다.

### `internal/state`

Raft desired state와 runtime projection을 담당한다.

- `DesiredState.ProxyConfig`를 검증한다.
- VIP/Raft timing cluster 정책을 local runtime 설정과 합성한다.
- 검증된 config를 route table과 upstream registry로 컴파일한다.

### `internal/route`

`spec.Config.Routes`를 runtime route table로 컴파일한다.

### `internal/upstream`

`spec.Config.UpstreamPools`를 runtime upstream registry로 컴파일한다.

### `internal/runtime`

현재 적용된 snapshot을 atomic하게 보관한다.

### `internal/proxy`

현재 runtime snapshot으로 HTTP request를 매칭하고 upstream으로 전달한다.

### `internal/admin`

Dashboard config API와 state store 사이의 service boundary다. public config 작업은 `GetConfig`, `ReplaceConfig` 두 가지다.

### `internal/raft`

HA mode의 cluster desired state 복제를 담당한다. 프록시 설정 변경은 `replace_config` command로 전체 config를 교체한다.

### `internal/dashboard`

Dashboard HTML과 JSON API를 제공한다.

- `GET /api/config`
- `PUT /api/config`
- `GET /api/status`
- `GET /api/runtime`
- cluster lifecycle/cluster status API

## ID 규칙

route ID와 upstream pool ID는 단일 config 안에서 유일해야 한다.

- runtime route ID는 `RouteConfig.ID`와 같다.
- runtime upstream pool ID는 `Config.UpstreamPools` map key와 같다.
- ID에 별도 scope prefix를 붙이지 않는다.

## 저장 규칙

설정 저장은 전체 config 교체 방식이다.

- route 생성/수정/삭제는 클라이언트가 config를 편집한 뒤 `PUT /api/config`로 저장한다.
- upstream pool 생성/수정/삭제도 같은 방식이다.
- 부분 CRUD command를 별도로 만들지 않는다.
