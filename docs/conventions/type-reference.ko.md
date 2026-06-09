# Type Reference

현재 구현에서 자주 오가는 핵심 타입을 패키지 경계 기준으로 정리한다.

## `internal/spec`

### `Config`

클러스터 전체 proxy desired config다.

- `Routes`: `[]RouteConfig`
- `UpstreamPools`: `map[string]UpstreamPool`

`Config.Validate()`는 route ID 중복, route match, upstream pool 참조, upstream 주소, health check 설정을 검증한다.

### `RouteConfig`

편집용 route desired config다.

- `ID`
- `Enabled`
- `Match`
- `Algorithm`
- `UpstreamPool`

### `UpstreamPool`

편집용 upstream pool desired config다. pool ID는 `Config.UpstreamPools` map key이며 값 내부에 별도 ID 필드를 두지 않는다.

## `internal/state`

### `DesiredState`

Raft log/snapshot으로 복제되는 cluster desired state다.

- `ProxyConfig`: 단일 `spec.Config`
- `VIP`: 선택적 cluster VIP 정책
- `RaftTiming`: 선택적 cluster Raft timing 정책
- `Version`
- `AppliedAt`

### `AppliedProxyConfig`

Admin API가 반환하는 적용된 proxy config view다.

- `Routes`
- `UpstreamPools`
- `AppliedAt`

### `StateError`

상태 계층에서 HTTP API까지 전달되는 오류 metadata다.

- `StatusCode`
- `Code`
- `Message`
- `LeaderAddress`
- `Err`

## `internal/runtime`

### `Snapshot`

요청 처리와 dashboard read path가 참조하는 불변 runtime snapshot이다.

- `AppConfig`
- `RaftIdentity`
- `RaftTiming`
- `VIP`
- `ProxyConfig`
- `RouteTable`
- `Upstreams`
- `AppliedAt`

`runtime.State`는 현재 snapshot을 atomic하게 교체하고 조회한다.

## `internal/route`

### `Route`

컴파일된 runtime route다.

- `ID`
- `Enabled`
- `Hosts`
- `Path`
- `Algorithm`
- `UpstreamPool`

`BuildTable(spec.Config)`는 desired route를 정렬 가능한 runtime route table로 변환한다.

## `internal/upstream`

### `Pool`

컴파일된 runtime upstream pool이다.

- `ID`
- `Targets`
- `HealthCheck`

`BuildRegistry(spec.Config)`는 desired upstream pool map을 runtime registry로 변환한다.

## `internal/admin`

### `Service`

Dashboard config API가 사용하는 service port다.

- `GetConfig(ctx)`
- `ReplaceConfig(ctx, cfg)`

### `ConfigView`

`GET /api/config`와 `PUT /api/config` 성공 응답이다.

- `Routes`
- `UpstreamPools`
- `AppliedAt`

## `internal/raft`

### `Command`

Raft log에 기록되는 command다.

- `replace_config`
- `set_cluster_vip`
- `clear_cluster_vip`
- `set_cluster_raft_timing`

프록시 설정 변경은 route/pool 단위 command가 아니라 `replace_config` 하나로 전체 config를 교체한다.
