# 타입 레퍼런스

## 목적

이 문서는 현재 프로젝트의 주요 타입이 어떤 책임을 갖는지 빠르게 확인하기 위한 참고 문서다.

특히 아래 상황에서 읽는 것을 의도한다.

- 처음 코드를 읽을 때
- 비슷해 보이는 타입의 차이를 구분해야 할 때
- desired state, runtime state, API view model의 경계를 확인할 때
- 새 타입을 어느 패키지에 둬야 할지 판단할 때

구현 세부사항 전체를 나열하기보다, 각 타입이 왜 존재하고 어떤 경계를 지키는지 설명하는 데 집중한다.

## 타입 분류 기준

현재 프로젝트의 타입은 크게 다섯 종류로 나뉜다.

### 1. 프로세스 부트 설정 타입

서버 프로세스를 어떻게 띄울지를 결정하는 타입이다.

주요 패키지:

- `internal/boot`

예:

- proxy listen address
- dashboard listen address
- Raft data dir

### 2. desired state 스키마 타입

Raft desired state에 저장되는 목표 설정을 표현하는 타입이다.

주요 패키지:

- `internal/spec`
- `internal/state`

예:

- namespace config
- route definition
- upstream pool definition
- cluster VIP policy
- cluster Raft timing policy

### 3. runtime 타입

실제 요청 처리와 현재 활성 상태 보관을 위해 사용하는 타입이다.

주요 패키지:

- `internal/config`
- `internal/route`
- `internal/upstream`
- `internal/runtime`

예:

- compiled route
- upstream registry
- runtime snapshot
- active VIP config

### 4. API view model 타입

내부 타입을 외부 JSON 응답으로 변환하기 위한 타입이다.

주요 패키지:

- `internal/dashboard`

예:

- `RuntimeView`
- `StatusView`
- `ClusterView`
- `RouteView`

### 5. 실행/wiring 타입

서버 실행, Raft lifecycle, VIP controller, proxy handler처럼 동작을 소유하는 타입이다.

주요 패키지:

- `internal/app`
- `internal/raft`
- `internal/vip`
- `internal/proxy`
- `internal/admin`

## `internal/boot`

### `AppConfig`

역할:

- process-local bootstrap 설정을 표현한다.

대표 필드:

- `ProxyListenAddr`
- `DashboardListenAddr`
- `RaftDataDir`

왜 필요한가:

- 서버 프로세스 실행 설정과 프록시 desired state의 lifecycle이 다르기 때문이다.
- 이 타입은 “서버를 어떻게 띄울지”를 표현하고, route/upstream/VIP policy 같은 cluster-wide 정책은 담지 않는다.

구분 포인트:

- `AppConfig`는 프록시 설정 전체가 아니다.
- Raft node identity와 timing은 `internal/config`의 공유 실행 설정 DTO로 분리한다.
- VIP address/GARP 정책은 `internal/state.ClusterVIPConfig`에 둔다.

## `internal/spec`

이 패키지 타입들은 namespace 단위 proxy desired config 스키마를 표현한다.

### `Config`

역할:

- namespace 하나의 route/upstream desired config를 표현한다.

대표 필드:

- `Name`
- `Routes`
- `UpstreamPools`

구분 포인트:

- runtime route table이 아니다.
- Raft desired state의 namespace 값으로 저장되는 원본 구조다.

### `LoadedConfig`

역할:

- Raft desired state에서 projection된 proxy config와 namespace metadata를 함께 보관한다.

대표 필드:

- `Source`
- `Path`
- `Config`

왜 필요한가:

- runtime projection 시 namespace source를 유지해야 한다.
- route/upstream 전역 ID를 만들 때 `<namespace>:<id>` 형태의 source가 필요하다.

### `RouteConfig`

역할:

- namespace desired config 안의 route 정의 하나를 표현한다.

대표 필드:

- `ID`
- `Enabled`
- `Match`
- `Algorithm`
- `UpstreamPool`

구분 포인트:

- 런타임 route인 `route.Route`와 다르다.
- `RouteConfig`는 저장/API 계약이고, `route.Route`는 실행 포맷이다.

### `RouteAlgorithm`

역할:

- route가 upstream target을 어떤 방식으로 고를지 표현하는 문자열 enum이다.

현재 값:

- `round_robin`
- `sticky_cookie`
- `5_tuple_hash`
- `least_connection`

구분 포인트:

- 비어 있으면 기본값은 `round_robin`으로 해석된다.
- 실제 요청 처리 시 `internal/proxy`가 이 값을 보고 `upstream.Pool`의 target selection 기능을 호출한다.

### `RouteMatchConfig`, `PathMatchConfig`, `PathMatchType`

역할:

- route match 조건의 원본 스키마를 표현한다.

대표 필드/값:

- `Hosts`
- `Path`
- `exact`
- `prefix`
- `regex`

구분 포인트:

- regex 컴파일과 segment prefix 판정은 `internal/route`의 runtime 타입이 담당한다.

### `UpstreamPool`

역할:

- desired config 안의 upstream pool 정의를 표현한다.

대표 필드:

- `Upstreams`
- `HealthCheck`

구분 포인트:

- `upstream.Pool`과 달리 health state, active connection count, round-robin cursor를 담지 않는다.

### `HealthCheckConfig`

역할:

- desired config에 적힌 health check 정책을 표현한다.

대표 필드:

- `Path`
- `Interval`
- `Timeout`
- `ExpectStatus`

구분 포인트:

- health check 결과를 담지 않는다.
- “어떻게 검사할지”만 표현한다.

### `Duration`

역할:

- `"30s"`, `"3s"` 같은 duration 문자열을 감싸는 타입이다.

왜 필요한가:

- JSON에서는 문자열 계약을 유지하면서 Go 코드에서는 `time.ParseDuration`으로 검증하기 위함이다.

### `ValidationError`, `ValidationErrors`

역할:

- desired config 검증 실패를 표현한다.
- 여러 검증 오류를 한 번에 반환한다.

## `internal/state`

이 패키지는 Raft에 합의되는 목표 상태와 runtime projection 경계를 표현한다.

### `DesiredState`

역할:

- Raft log/snapshot에 저장되는 전체 desired state를 표현한다.

대표 필드:

- `Namespaces`
- `VIP`
- `RaftTiming`
- `Version`
- `AppliedAt`

구분 포인트:

- runtime state가 아니다.
- `ProjectSnapshot()`을 거쳐 `runtime.Snapshot`으로 변환된다.

### `ClusterVIPConfig`

역할:

- cluster-wide VIP 정책을 표현한다.

대표 필드:

- `Address`
- `GARPCount`
- `GARPInterval`
- `AcquireDelay`
- `ReleaseOnShutdown`

구분 포인트:

- VIP network interface는 node-local 값이므로 이 타입에 두지 않는다.
- runtime 적용 값은 `config.VIPConfig`로 projection된다.

### `ClusterRaftTimingConfig`

역할:

- cluster-wide Raft timing 정책을 표현한다.

대표 필드:

- `HeartbeatTimeout`
- `ElectionTimeout`
- `LeaderLeaseTimeout`
- `CommitTimeout`

### `NamespaceSummary`, `NamespaceConfig`

역할:

- namespace API에서 쓰는 desired config 조회/요약 모델이다.

구분 포인트:

- `NamespaceConfig`는 편집용 desired config view다.
- runtime route/upstream 상태는 `dashboard.RuntimeView`로 조회한다.

### `StateError`

역할:

- state/admin/dashboard 경계에서 HTTP status, error code, leader address를 함께 전달한다.

대표 사용:

- not leader
- cluster not configured
- invalid namespace
- invalid VIP policy

## `internal/config`

여러 계층에서 공유하는 작은 실행 설정 DTO를 둔다. 파일 로딩, Raft 합의, runtime snapshot 자체를 담당하지 않는다.

### `RaftConfig`

역할:

- runtime snapshot에 넣을 Raft identity와 timing을 묶는다.

대표 필드:

- `RaftIdentity`
- `RaftTiming`

### `RaftIdentity`

역할:

- 현재 노드의 Raft identity를 표현한다.

대표 필드:

- `NodeID`
- `BindAddr`
- `AdvertiseAddr`

### `RaftTiming`

역할:

- 현재 노드가 사용하는 effective Raft timing을 표현한다.

대표 필드:

- `HeartbeatTimeout`
- `ElectionTimeout`
- `LeaderLeaseTimeout`
- `CommitTimeout`

### `VIPConfig`

역할:

- 현재 노드에 실제 적용할 VIP 값을 표현한다.

대표 필드:

- `Interface`
- `Address`
- `GARPCount`
- `GARPInterval`
- `AcquireDelay`
- `ReleaseOnShutdown`
- `BindAddr`
- `AdvertiseAddr`

구분 포인트:

- clean node에서는 기본 identity를 임의로 만들지 않는다.
- bootstrap/join 이후 node-local metadata로 저장/복원된다.

### `Timing`

역할:

- 현재 노드가 적용할 Raft timing 값을 표현한다.

대표 필드:

- `HeartbeatTimeout`
- `ElectionTimeout`
- `LeaderLeaseTimeout`
- `CommitTimeout`

## `internal/route`

이 패키지 타입들은 runtime 라우팅 정책을 표현한다.

### `Route`

역할:

- 전역 route table에 올라간 runtime route 하나를 표현한다.

대표 필드:

- `GlobalID`
- `LocalID`
- `Source`
- `Enabled`
- `Hosts`
- `Path`
- `Algorithm`
- `UpstreamPool`

왜 필요한가:

- `spec.RouteConfig`는 desired config 원본이고, `Route`는 실행에 필요한 컴파일 결과다.
- regex 사전 컴파일, 전역 ID, 전역 upstream pool 참조 같은 runtime 정보가 필요하다.

### `PathMatcher`

역할:

- runtime path match 로직에 필요한 정보를 담는다.

대표 필드:

- `Kind`
- `Value`
- `Regex`

구분 포인트:

- regex는 route compile 시점에 미리 컴파일한다.
- prefix는 segment 기반 matching을 수행한다.

### `PathKind`

역할:

- runtime path match 종류를 나타내는 enum이다.

현재 값:

- `PathKindAny`
- `PathKindExact`
- `PathKindPrefix`
- `PathKindRegex`

## `internal/upstream`

이 패키지 타입들은 runtime upstream 선택 상태를 표현한다.

### `Pool`

역할:

- 전역 upstream pool 하나의 runtime 표현이다.

대표 필드:

- `GlobalID`
- `LocalID`
- `Source`
- `Targets`
- `HealthCheck`
- `targetState`
- `active`
- `healthy`
- `next`

왜 필요한가:

- desired config 원본 pool 정의만으로는 target URL, health state, in-flight count, round-robin cursor를 보관할 수 없기 때문이다.

대표 기능:

- `NextTarget()`
- `HashTarget(key)`
- `LeastConnectionTarget()`
- `SetTargetHealthy()`
- `SetTargetUnhealthy()`
- `SnapshotStates()`

### `Target`

역할:

- pool 안의 target 하나를 표현한다.

현재 필드:

- `Raw`
- `URL`

구분 포인트:

- `Raw`는 설정/API에서 온 원본 주소다.
- `URL`은 reverse proxy가 바로 사용할 수 있도록 사전 파싱한 값이다.

### `TargetState`

역할:

- target별 health 상태를 보관한다.

대표 필드:

- `Healthy`
- `LastCheckedAt`
- `LastError`

### `HealthCheck`

역할:

- runtime pool에 복사된 health check 정책이다.

구분 포인트:

- health check 결과가 아니라 검사 설정이다.

### `Registry`

역할:

- 전역 upstream pool 저장소다.

대표 기능:

- `Get(globalID)`
- `All()`

- `AcquireDelay`
- `ReleaseOnShutdown`

구분 포인트:

- `Interface`는 node-local lifecycle 입력이다.
- `Address`, GARP 정책, acquire 정책은 Raft desired state의 cluster-wide VIP 정책에서 온다.
- `Active()`는 address 존재 여부로 VIP 활성 상태를 판단한다.

## `internal/runtime`

이 패키지 타입들은 현재 활성 메모리 상태를 표현한다.

### `Snapshot`

역할:

- 현재 서버가 사용 중인 활성 상태 전체를 묶는다.

대표 필드:

- `AppConfig`
- `RaftIdentity`
- `RaftTiming`
- `VIP`
- `ProxyConfigs`
- `RouteTable`
- `Upstreams`
- `AppliedAt`

왜 필요한가:

- route table, upstream registry, Raft/VIP 상태를 같은 버전으로 읽어야 하기 때문이다.
- Raft apply/restore로 새 desired state가 들어오면 전체 상태를 한 번에 교체해야 하기 때문이다.

구분 포인트:

- `Snapshot`은 desired state 자체가 아니다.
- desired state를 컴파일한 활성 상태 뷰다.

### `State`

역할:

- 현재 활성 snapshot을 thread-safe하게 보관하고 교체한다.

대표 기능:

- `Snapshot()`
- `Swap()`

## `internal/proxy`

### `Handler`

역할:

- 현재 runtime snapshot을 읽고 실제 backend로 요청을 전달하는 HTTP handler다.

대표 책임:

- route resolve
- algorithm별 upstream target 선택
- reverse proxy 요청 전달
- upstream transport pool 재사용

구분 포인트:

- `Handler`는 desired config를 수정하지 않는다.
- `transport`는 reverse proxy 전용 connection pool 정책의 소유자다.

### `transportConfig`

역할:

- reverse proxy가 사용할 upstream `http.Transport` 기본값 묶음을 표현한다.

대표 필드:

- `maxIdleConns`
- `maxIdleConnsPerHost`
- `maxConnsPerHost`
- `idleConnTimeout`
- `responseHeaderWait`

## `internal/dashboard`

이 패키지 타입들은 내부 runtime/state 타입을 외부 응답용 JSON으로 변환한 view model이다.

### `RuntimeView`

역할:

- `/api/runtime` 응답 구조다.

대표 필드:

- `AppliedAt`
- `Node`
- `ConfigSources`
- `Routes`
- `Upstreams`

### `StatusView`

역할:

- `/api/status` 응답 구조다.

대표 필드:

- `Node`
- `Raft`
- `VIP`
- `Runtime`

### `ClusterView`

역할:

- `/api/cluster` 응답 구조다.

대표 필드:

- `Enabled`
- `QuorumStatus`
- `Leader`
- `Local`
- `Members`
- `RaftTiming`

### `RouteView`, `RuntimeUpstreamView`, `RuntimeTargetView`

역할:

- runtime route/upstream/target 상태를 관리 API 응답용으로 표현한다.

구분 포인트:

- runtime 내부 타입을 그대로 JSON으로 노출하지 않는다.
- target health와 active connection count처럼 운영자가 읽어야 하는 값을 정리해 반환한다.

## 자주 헷갈리는 타입 비교

### `boot.AppConfig` vs `spec.Config`

- `boot.AppConfig`
  - process-local bootstrap 설정
  - listen 주소, Raft data dir 등
- `spec.Config`
  - namespace 하나의 proxy desired config
  - routes, upstream pools 등

### `state.DesiredState` vs `runtime.Snapshot`

- `state.DesiredState`
  - Raft log/snapshot에 저장되는 source of truth
  - namespace config, VIP policy, Raft timing policy 포함
- `runtime.Snapshot`
  - 현재 프로세스가 요청 처리에 사용하는 활성 상태
  - route table, upstream registry, Raft identity/timing, VIP runtime config 포함

### `state.ClusterVIPConfig` vs `config.VIPConfig`

- `ClusterVIPConfig`
  - Raft desired state에 저장되는 cluster-wide VIP 정책
- `config.VIPConfig`
  - 현재 노드에 적용할 VIP 값
  - cluster-wide 정책과 node-local interface를 합성한 결과

### `spec.RouteConfig` vs `route.Route`

- `RouteConfig`
  - JSON/API/Raft desired state의 원본 route 정의
- `Route`
  - runtime route table의 route
  - global ID, compiled regex, 전역 upstream pool 참조 포함

### `spec.UpstreamPool` vs `upstream.Pool`

- `spec.UpstreamPool`
  - desired config 원본 upstream pool 정의
- `upstream.Pool`
  - runtime pool
  - parsed target URL, target health, active count, round-robin cursor 포함

### `spec.HealthCheckConfig` vs `upstream.HealthCheck`

- `HealthCheckConfig`
  - desired config 원본 health check 설정
- `HealthCheck`
  - runtime pool에 복사된 health check 설정

### `dashboard.*View` vs runtime/internal type

- `dashboard.*View`
  - 외부 JSON 응답 계약
- runtime/internal type
  - 내부 동작과 상태 보관용 타입

## 타입 추가 시 판단 기준

새 타입을 추가할 때는 먼저 아래를 판단한다.

### 1. 이 타입은 process-local bootstrap 설정인가?

그렇다면 보통 `internal/boot`에 둔다.

### 2. 이 타입은 desired state 포맷인가?

그렇다면 보통 `internal/spec` 또는 `internal/state`에 둔다.

### 3. 이 타입은 여러 계층에서 공유하는 작은 실행 설정 DTO인가?

그렇다면 보통 `internal/config`에 둔다.

### 4. 이 타입은 runtime 계산 결과인가?

그렇다면 보통 `internal/route`, `internal/upstream`, `internal/runtime`에 둔다.

### 5. 이 타입은 외부 API 응답용인가?

그렇다면 보통 `internal/dashboard`에 view model로 둔다.

### 6. 이 타입은 실제 실행 handler/controller 상태인가?

그렇다면 `internal/proxy`, `internal/app`, `internal/raft`, `internal/vip`에 둔다.

## 요약

현재 프로젝트의 타입들은 다음처럼 이해하면 된다.

- `boot`
  - process-local bootstrap 설정
- `spec`, `state`
  - Raft desired state와 검증
- `config`, `route`, `upstream`, `runtime`
  - 실행을 위한 runtime 도메인 타입
- `dashboard`
  - 외부 API 응답 view model
- `app`, `raft`, `vip`, `proxy`, `admin`
  - 실행 흐름과 controller/handler/service 타입

이 구분이 무너지면 설정 스키마, 합의 상태, runtime 상태, API 응답 구조가 서로 섞인다.

특별한 이유가 없다면 이 경계는 유지한다.
