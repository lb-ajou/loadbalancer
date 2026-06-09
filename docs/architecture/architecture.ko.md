# Architecture

이 프로젝트는 L7 reverse proxy, Dashboard/Admin API, Raft 기반 cluster desired state 복제를 하나의 프로세스 안에 둔다.

## 핵심 원칙

- 프로세스 기동 설정과 프록시 desired config를 분리한다.
- 프록시 desired config는 클러스터 전체에 적용되는 단일 `spec.Config`다.
- Raft는 proxy config, cluster VIP policy, cluster Raft timing policy만 복제한다.
- health 상태, active connection, reverse proxy cache는 각 노드의 local runtime state다.
- 요청 처리는 항상 `runtime.Snapshot`을 기준으로 수행한다.

## 상태 흐름

```text
configs/app.json
  -> boot.AppConfig
  -> app.App

Dashboard/Admin API
  -> admin.Service
  -> raft.Store
  -> raft.FSM
  -> state.DesiredState
  -> state.ProjectSnapshot()
  -> runtime.Snapshot
  -> proxy.Handler
```

`configs/app.json`은 listen address, dashboard address, Raft data dir 같은 node-local process config만 담는다. route/upstream 설정은 파일 입력이 아니라 Admin API로 저장한다.

## Desired State

`state.DesiredState`는 Raft log/snapshot으로 복제되는 cluster state다.

- `ProxyConfig spec.Config`
- `VIP *state.ClusterVIPConfig`
- `RaftTiming *state.ClusterRaftTimingConfig`
- `Version`
- `AppliedAt`

프록시 설정 변경은 `replace_config` command 하나로 전체 `ProxyConfig`를 교체한다. route/upstream pool 개별 command는 없다.

## Runtime Projection

`state.ProjectSnapshot()`은 desired state와 node-local config를 합성해 runtime snapshot을 만든다.

1. `DesiredState.ProxyConfig`를 정규화한다.
2. `spec.Config.Validate()`로 config 전체를 검증한다.
3. `route.BuildTable(spec.Config)`로 runtime route table을 만든다.
4. `upstream.BuildRegistry(spec.Config)`로 runtime upstream registry를 만든다.
5. VIP/Raft timing cluster policy를 node-local runtime 입력과 합성한다.
6. `runtime.Snapshot`을 atomic하게 교체한다.

## ID Model

route와 upstream pool은 단일 config 안의 ID를 그대로 runtime ID로 사용한다.

- route runtime ID = `RouteConfig.ID`
- upstream pool runtime ID = `Config.UpstreamPools` map key
- 별도 scope prefix나 source metadata를 붙이지 않는다.

이 구조에서는 ID 충돌 범위가 단일 config 하나뿐이므로, route/upstream lookup이 직접적이고 Dashboard runtime view도 같은 ID를 표시한다.

## Admin API

설정 API는 전체 config 조회/저장만 제공한다.

- `GET /api/config`
- `PUT /api/config`

클라이언트는 route/pool 생성, 수정, 삭제를 로컬 편집 상태에서 수행하고, 저장 시 전체 config를 보낸다.

## Raft

Raft command set:

- `replace_config`
- `set_cluster_vip`
- `clear_cluster_vip`
- `set_cluster_raft_timing`

Leader가 command를 commit하면 각 노드 FSM이 같은 순서로 `DesiredState`를 갱신하고 runtime snapshot을 다시 project한다.

Follower에 write 요청이 들어오면 leader forwarding 없이 `409 Conflict`와 `not_raft_leader` error code를 반환한다.

## Dashboard Runtime View

`GET /api/runtime`은 현재 노드의 runtime snapshot을 보여준다.

- applied time
- route count / upstream pool count
- runtime route list
- upstream pool target health
- active connection count

Health와 active connection은 Raft로 복제되지 않으므로 응답한 노드의 local 관측값이다.

## VIP

VIP address와 GARP/acquire/release 정책은 cluster-wide desired state다. Linux interface 이름은 node-local 환경에 종속되므로 bootstrap/join 입력으로 각 노드가 제공한다.

Leader transition을 감지한 노드는 local interface에 VIP를 추가하거나 제거한다.
