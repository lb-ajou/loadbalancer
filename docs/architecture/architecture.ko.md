# 아키텍처 상세 설명

## 목적

이 문서는 현재 프로젝트의 구조를 “추상적인 레이어 설명”이 아니라 “실제 입력이 어떤 과정을 거쳐 어떤 런타임 결과로 바뀌는지” 기준으로 설명하기 위한 문서다.

새 라우팅 알고리즘을 추가할 때 필요한 공통 설계/구현/검증 절차는 `docs/architecture/routing-algorithm-playbook.ko.md`를 먼저 참고한다.

특히 아래 질문에 답할 수 있도록 작성한다.

- `configs/app.json`과 Raft desired state는 각각 무엇을 의미하는가?
- 이 입력들은 어떤 패키지를 거쳐 어떤 Go 객체가 되는가?
- “현재 적용된 결과”란 정확히 무엇을 말하는가?
- 요청 하나가 들어오면 어떤 객체들이 어떤 순서로 관여하는가?
- 비슷한 이름의 타입들은 왜 따로 존재하는가?

---

## 가장 먼저 이해해야 할 핵심

이 프로젝트는 크게 아래 4단계를 거친다.

1. proxy desired state를 읽는다.
2. desired state를 런타임 구조로 변환한다.
3. 변환된 결과를 현재 활성 상태로 메모리에 보관한다.
4. 실제 요청은 그 활성 상태를 기준으로 처리한다.

즉 이 프로젝트의 핵심은 다음 문장으로 요약할 수 있다.

> proxy desired state를 직접 요청 처리에 쓰지 않고, desired state를 한 번 해석해서 런타임용 구조로 바꾼 뒤, 그 결과를 메모리에 보관하고, 요청은 항상 그 메모리 상태를 기준으로 처리한다.

이 문장에서 중요한 단어는 두 개다.

- desired state
- 활성 상태

둘은 같은 것이 아니다.

---

## “desired state”와 “활성 상태”의 차이

### desired state

desired state는 프록시가 갖춰야 할 목표 설정이다. 현재 런타임 source of truth는 Raft log/snapshot 하나다. `configs/app.json`은 노드 로컬 부팅 설정으로 남는다.

예:

- `configs/app.json`
- Raft log/snapshot 안의 namespace desired config

프록시 route/upstream JSON은 더 이상 앱 부팅 입력이 아니다. 정상 운영 중 dashboard/admin API 쓰기의 source of truth는 Raft 상태다.

### 활성 상태

활성 상태는 desired state를 읽고 검증하고 정렬하고 전역화한 뒤, 실제 요청 처리에 바로 사용할 수 있게 만든 메모리 구조다.

현재 활성 상태에는 아래가 들어간다.

- 어떤 listen 주소로 서버가 떠 있는지
- 어떤 namespace desired config들이 적용되었는지
- 어떤 route들이 어떤 우선순위로 정렬되었는지
- 어떤 upstream pool들이 어떤 전역 이름으로 관리되는지
- 각 upstream target이 현재 healthy한지 unhealthy한지

즉 “현재 적용된 결과”란 정확히 말하면 다음을 뜻한다.

> desired state를 런타임용 route table, upstream registry, health state를 포함한 실행 가능한 상태로 변환한 결과

이게 바로 `runtime.Snapshot`의 의미다.

---

## 전체 구조를 한 장으로 보면

```text
configs/app.json
    ↓
internal/boot.AppConfig
    ↓
internal/app
    ↓
Raft log/snapshot DesiredState
    ↓
internal/state.ProjectSnapshot()
    ↓
internal/route.BuildTable()
    ↓
[]route.Route

internal/upstream.BuildRegistry()
    ↓
*upstream.Registry
    ↓
runtime.NewSnapshot()
    ↓
runtime.Snapshot
    ↓
runtime.State
    ↓
proxy.Handler / dashboard.Handler
```

즉 노드 로컬 부팅 입력은 파일이고, 프록시 desired state 입력은 Raft 상태다. 최종 실행 기준은 `runtime.State` 안의 `Snapshot`이다.

---

## 예시로 보는 전체 흐름

## 1. 앱 설정

`configs/app.json`

```json
{
  "proxyListenAddr": ":8080",
  "dashboardListenAddr": ":9090"
}
```

이 파일은 다음 질문에 답한다.

- 프록시 서버는 어느 포트에서 들을 것인가?
- 대시보드 서버는 어느 포트에서 들을 것인가?
- Raft node가 어떤 주소와 data dir로 뜰 것인가?

즉 “서버 프로세스를 어떻게 띄울 것인가”에 대한 설정이다.

---

## 2. 프록시 설정

Raft desired state의 `default` namespace

```json
{
  "name": "default",
  "routes": [
    {
      "id": "r-api",
      "enabled": true,
      "match": {
        "hosts": ["api.example.com"],
        "path": { "type": "prefix", "value": "/api/" }
      },
      "upstream_pool": "pool-api"
    }
  ],
  "upstream_pools": {
    "pool-api": {
      "upstreams": ["10.0.0.11:8080", "10.0.0.12:8080"],
      "health_check": {
        "path": "/health",
        "interval": "30s",
        "timeout": "3s",
        "expect_status": 200
      }
    }
  }
}
```

이 desired config는 다음 질문에 답한다.

- 어떤 요청이 이 namespace route에 매칭되는가?
- 매칭된 요청은 어떤 upstream pool로 가는가?
- 그 route는 어떤 알고리즘으로 upstream을 선택하는가?
- 그 pool 안의 backend target은 무엇인가?
- pool 단위 health check는 어떻게 수행하는가?

현재 선택 알고리즘 예:

- `round_robin`
- `sticky_cookie`
- `5_tuple_hash`
- `least_connection`

`sticky_cookie`는 첫 요청에서는 기존 round-robin 선택 결과를 사용하고, 이후에는 route 단위 cookie에 저장된 upstream target(`host:port`)을 우선 재사용한다. 해당 target이 unhealthy면 다른 healthy target으로 재선택하고 cookie를 갱신한다.

`5_tuple_hash`는 `protocol`, client address, client port, destination host, destination port를 조합한 키를 healthy target 집합에 stable hash로 매핑한다.

`least_connection`은 실제 backend TCP connection 수가 아니라 프록시가 target별로 추적하는 in-flight 요청 수를 기준으로 동작한다. 여기서 in-flight는 아직 `ReverseProxy.ServeHTTP`가 반환하지 않은 일반 HTTP 요청, 스트리밍 응답, websocket 업그레이드 연결을 모두 포함한다. healthy target 중 현재 in-flight 수가 가장 적은 target을 고르고, 동률이면 round-robin 순서로 결정한다.

즉 “프록시가 어떤 정책으로 요청을 보낼 것인가”에 대한 설정이다.

---

## 계층별 상세 설명

## 1. 입력 계층

입력 계층은 노드 로컬 부팅 설정과 proxy desired state를 Go 구조체로 읽어오는 계층이다.

여기에는 두 패키지가 있다.

- `internal/boot`
- `internal/spec`

### `internal/boot`

이 패키지는 `configs/app.json`과 직접 전달된 `boot.AppConfig`의 기본값 적용/검증을 담당한다.

입력:

- `configs/app.json` 파일

출력:

- `boot.AppConfig`

즉 이 패키지가 하는 일은:

> 앱 부트 설정을 읽거나 받아서, `boot.Normalize()`로 기본값이 적용된 `AppConfig` 객체로 만드는 것

예:

- `configs/app.json`은 `internal/boot.fileConfig`가 정의한 listen 주소와 `raftDataDir` 같은 process-local 값만 담는다. 파일에 legacy `raftNodeId`, `raftBindAddr`, `raftAdvertiseAddr`, Raft timeout 키가 있어도 로더는 이를 Raft 입력으로 사용하지 않는다.
- `boot.AppConfig`는 process-local 정적 설정 모델이다. listen 주소와 `raftDataDir`만 담고, Raft identity/timing은 `internal/raftstate` 런타임 값으로 분리한다. Runtime read path는 `Snapshot.RaftIdentity`, `Snapshot.RaftTiming`을 사용한다. VIP 적용 값도 `AppConfig`가 아니라 별도 lifecycle local VIP 입력과 Raft desired state를 합성한 `Snapshot.VIP`에만 둔다.
- VIP cluster-wide 입력은 dashboard/CLI bootstrap request DTO가 받고, `state.NormalizeClusterVIP()`로 기본 GARP/acquire 정책을 채운 뒤 `state.ValidateClusterVIP()`으로 검증해 Raft desired state의 `state.ClusterVIPConfig`로 저장한다. `internal/vip/runtime`는 runtime에 적용할 합성 VIP 값을 표현하며, `boot` 패키지는 VIP DTO나 VIP policy 기본값을 갖지 않는다.
- Raft node identity는 bootstrap/join 입력으로 받고, 성공 후 Raft data dir의 node-local metadata에 저장한다.
- Raft timing은 bootstrap 입력의 `raft_timing`으로 받고, `state.ValidateClusterRaftTiming()`으로 검증한 뒤 Raft desired state에 cluster-wide 정책으로 저장한다.
- Join node는 Raft node를 시작하기 전에 peer 후보의 `GET /api/cluster`에서 cluster-wide Raft timing을 조회해 local start config에 반영한다.
- `boot.Normalize()`는 clean node에 Raft node identity 기본값을 채우지 않는다. `raftDataDir` 같은 process-local 기본값만 채운다.
- bootstrap/join lifecycle 입력에서 `raft_bind_addr`가 없으면 `raft_advertise_addr`의 port를 사용해 `0.0.0.0:<port>`로 기본값을 채운다.
- 기존 Raft state가 없으면 clean node는 cluster를 자동 생성하지 않고 unconfigured control-plane으로 시작한다. cluster 생성은 `POST /api/cluster/bootstrap`, 가입은 `POST /api/node/join-cluster`가 담당한다.
- 웹/CLI는 `GET /api/node/cluster-status`로 현재 노드가 bootstrap/join 가능한지 확인한다.
- `reverseproxy cluster status|bootstrap|join`은 같은 dashboard lifecycle API를 호출하는 운영용 CLI다. 서버 실행은 기존 `reverseproxy [configPath]`와 새 `reverseproxy serve [configPath]`를 모두 지원한다.
- `/cluster-lifecycle`은 같은 lifecycle API를 호출하는 웹 운영 화면이다. 기존 route/upstream SPA와 분리된 진입점이며, 파일 편집 없이 현재 노드를 bootstrap/join한다.
- bootstrap/join으로 받은 Raft node identity와 bind/advertise address는 Raft data dir의 node-local metadata로 저장한다. 기존 Raft state가 있는 재시작 경로에서는 이 metadata에서 Raft identity를 복원한다. HA compose app config는 identity fields 없이 lifecycle CLI 입력으로 cluster를 만든다.
- app config 파일은 join peer 목록이나 VIP address를 받지 않는다. join peer는 join API 요청의 `peers`, VIP address는 bootstrap API 요청의 `vip.address`로 전달한다.
- `configStore` mode selector와 `raftBootstrap` role flag는 제거됐고, `Normalize()`는 process-local 기본값과 Raft data dir 기본값만 적용한다.
- VIP는 cluster-wide address/GARP 정책과 node-local interface로 분리한다. address/GARP 정책은 Raft desired state로 이동하고, interface는 bootstrap/join 시 각 노드가 제공하는 local 값으로 유지한다. `vip.enabled` 입력은 제거됐으며 address 존재 여부가 활성 의미를 갖는다.

### `internal/spec`

이 패키지는 JSON 형태의 proxy config 구조와 validation을 담당한다. 정상 런타임 source of truth는 Raft 상태이며 앱 부팅은 프록시 JSON 파일을 읽지 않는다.

입력:

- Raft desired state의 namespace별 proxy config

출력:

- `[]spec.LoadedConfig`

즉 이 패키지가 하는 일은:

> Raft desired state 안의 namespace별 proxy config를 검증 가능한 desired state 객체 묶음으로 다루는 것

예를 들어 Raft desired state의 `default` namespace는 projection 단계에서 `LoadedConfig{Source: "default", Config: ...}` 형태로 런타임 컴파일 계층에 전달된다.

이 단계에서는 아직 route table이나 upstream registry를 만들지 않는다.
그냥 “신뢰 가능한 desired state 묶음”만 만든다.

---

## 2. 컴파일 계층

컴파일 계층은 desired state를 실제 요청 처리용 구조로 변환하는 계층이다.

여기에는 두 패키지가 있다.

- `internal/route`
- `internal/upstream`

이 계층이 중요한 이유는, Raft에 저장된 desired config 원본을 그대로 요청 처리에 쓰면 안 되기 때문이다.

예를 들어 desired config에 적힌 route는:

- 로컬 ID만 가지고 있고
- upstream pool 참조도 로컬 이름이고
- regex도 문자열 형태이며
- 정렬도 안 되어 있다

즉 요청 처리에 바로 쓰기에는 정보가 부족하다.

### `internal/route`

입력:

- `[]spec.LoadedConfig`

출력:

- `[]route.Route`

즉 이 패키지가 하는 일은:

> namespace별 desired config의 route 정의를 하나의 전역 route table로 합치고, 우선순위가 반영된 런타임 route 목록으로 바꾸는 것

구체적으로는 아래 변환을 한다.

#### 1. 로컬 route id를 전역 route id로 변환

예:

- `default.json`의 `r-api`
  -> `default:r-api`
- `admin.json`의 `r-api`
  -> `admin:r-api`

#### 2. route가 참조하는 upstream pool 이름도 전역 이름으로 변환

예:

- `pool-api`
  -> `default:pool-api`

#### 3. regex는 문자열에서 실제 `regexp.Regexp`로 컴파일

즉 런타임에서 매 요청마다 regex를 다시 컴파일하지 않는다.

#### 4. route 우선순위 정렬

현재 규칙:

1. exact
2. prefix
3. regex
4. any

prefix끼리는:

- segment depth가 큰 것 우선
- 같으면 `GlobalID` 순

즉 `internal/route`의 출력은 “정렬된 전역 route table”이다.

### `internal/upstream`

입력:

- `[]spec.LoadedConfig`

출력:

- `*upstream.Registry`

즉 이 패키지가 하는 일은:

> namespace별 desired config의 upstream pool 정의를 하나의 전역 upstream registry로 합치고, health state와 target 선택이 가능한 런타임 pool 구조로 바꾸는 것

구체적으로는 아래 변환을 한다.

#### 1. 로컬 pool id를 전역 pool id로 변환

예:

- `default.json`의 `pool-api`
  -> `default:pool-api`

#### 2. target 문자열 목록을 runtime target 목록으로 변환

예:

- `["10.0.0.11:8080", "10.0.0.12:8080"]`
  -> `[]upstream.Target`

#### 3. health check 설정을 runtime pool에 복사

#### 4. target별 health state 초기화

초기 정책은 현재 healthy다.

즉 `internal/upstream`의 출력은 “전역 pool 조회와 healthy target 선택이 가능한 registry”다.

---

## 3. 활성 상태 계층

활성 상태 계층은 “컴파일 결과를 현재 서버가 실제로 사용 중인 형태로 보관하는 계층”이다.

여기에는 `internal/runtime`이 있다.

### 정확히 무엇에 대한 결과인가?

“현재 적용된 결과”는 아래 3개의 결과를 뜻한다.

1. 앱 부트 설정 파일을 읽은 결과
   - `boot.AppConfig`
2. namespace desired config들을 검증한 결과
   - `[]spec.LoadedConfig`
3. 그 desired config들을 런타임용으로 컴파일한 결과
   - `[]route.Route`
   - `*upstream.Registry`

즉 활성 상태 계층은 다음의 결과를 보관한다.

> 현재 desired state를 해석한 결과, 서버가 실제로 요청 처리에 사용하고 있는 route table / upstream registry / health 상태 / 앱 설정

### `runtime.Snapshot`

이 타입은 “현재 적용된 결과” 전체를 하나로 묶는다.

현재 담는 것:

- `AppConfig`
- `RaftIdentity`
- `RaftTiming`
- `VIP`
- `ProxyConfigs`
- `RouteTable`
- `Upstreams`
- `AppliedAt`

즉 이 타입 하나만 보면:

- 어떤 app 설정으로 서버가 떠 있는지
- 어떤 namespace desired config들이 활성화되었는지
- 어떤 route들이 어떤 순서로 평가되는지
- 어떤 upstream pool들이 존재하는지
- 각 pool 안의 health 상태가 어떤지

를 추적할 수 있다.

### 왜 snapshot이 필요한가?

예를 들어 Raft log에 새 desired state가 apply된다고 하자.

이때:

- route table은 새 버전
- upstream registry는 옛 버전

인 중간 상태가 생기면 안 된다.

그래서 route와 upstream을 같은 단위로 묶어서 한 번에 교체할 단위가 필요하다.
그게 snapshot이다.

### `runtime.State`

`State`는 현재 snapshot을 thread-safe하게 들고 있는 컨테이너다.

입력:

- `runtime.Snapshot`

출력:

- 현재 활성 snapshot을 읽거나
- 새 snapshot으로 교체하는 기능

즉 `State`는 “활성 상태 저장소”라고 보면 된다.

---

## 4. 실행 계층

실행 계층은 위에서 만든 활성 상태를 실제로 사용해서 서버를 띄우고 요청을 처리하는 계층이다.

여기에는 아래가 있다.

- `internal/app`
- `internal/proxy`
- `internal/dashboard`
- `main.go`

### `internal/app`

이 패키지는 “조립 계층”이다.

입력:

- `boot.AppConfig`

출력:

- `*app.App`

즉 이 패키지가 하는 일은:

> app 설정과 proxy 설정을 읽고, route table과 upstream registry를 만들고, 그것을 runtime snapshot으로 묶고, 그 snapshot을 사용하는 서버와 handler를 만드는 것

#### Raft projection callback

앱 런타임 snapshot은 Raft FSM apply/restore callback에서 만든다.

입력:

- `boot.AppConfig`
- Raft FSM의 `state.DesiredState`

출력:

- `runtime.Snapshot`

즉 이 경로는:

> Raft desired state를 `state.ProjectSnapshot()`으로 투영해 전역 route table과 upstream registry를 만들고, `runtime.State`에 교체 적용하는 경로

#### `App`

`App`은 실제 서버를 돌리기 위한 조립 결과물이다.

보관하는 것:

- logger
- config path
- runtime state
- health checker lifecycle 관련 정보
- proxy handler
- dashboard handler
- proxy server
- dashboard server

즉 “실행 가능한 애플리케이션 인스턴스”라고 보면 된다.

### `internal/proxy`

이 패키지는 실제 프록시 요청을 처리한다.

입력:

- `runtime.State`
- HTTP 요청

출력:

- backend로 프록시된 응답
또는
- 404 / 502 같은 에러 응답

즉 이 패키지가 하는 일은:

> 현재 활성 snapshot을 읽고, route를 찾고, upstream target을 골라서 요청을 backend로 전달하는 것

이 패키지는 route 정책을 정의하지 않는다.
그 정책은 `internal/route`에서 이미 끝난 상태여야 한다.

### `internal/dashboard`

이 패키지는 현재 활성 상태를 사람이 읽기 쉬운 JSON으로 노출한다.

입력:

- `runtime.State`

출력:

- app config
- proxy configs
- route table
- upstream 목록

즉 이 패키지가 하는 일은:

> 내부 snapshot 구조를 운영자가 읽기 쉬운 응답 형태로 변환해서 보여주는 것

중요한 점은 dashboard가 runtime 내부 타입을 그대로 노출하지 않는다는 것이다.
예를 들어 `Registry` 내부 map을 그대로 보여주기보다 `UpstreamPoolView` 형태로 변환한다.

### `internal/admin`

이 패키지는 dashboard config API가 요청한 desired config 변경을 내부 `stateStore` operation으로 실행한다.

- `internal/app`은 Raft-backed store를 admin service에 넘긴다.
- admin service 자체는 file/runtime 타입을 알지 않고, store error를 API error shape로 보존하는 역할에 집중한다.

---

## 실제 요청 하나가 처리되는 과정을 아주 구체적으로 보면

다음과 같은 요청이 들어온다고 하자.

- Host: `api.example.com`
- Path: `/api/admin/users`

그리고 설정 파일은 아래 두 개가 있다고 하자.

### `default.json`

```json
{
  "routes": [
    {
      "id": "r-api",
      "enabled": true,
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

### `admin.json`

```json
{
  "routes": [
    {
      "id": "r-api-admin",
      "enabled": true,
      "match": {
        "hosts": ["api.example.com"],
        "path": { "type": "prefix", "value": "/api/admin/" }
      },
      "upstream_pool": "pool-admin"
    }
  ],
  "upstream_pools": {
    "pool-admin": {
      "upstreams": ["10.0.1.10:9000"]
    }
  }
}
```

### startup 시 변환 결과

#### route table

- `admin:r-api-admin`
- `default:r-api`

왜 이 순서인가?

- `/api/admin/`가 `/api/`보다 더 깊은 prefix이기 때문이다.

#### upstream registry

- `admin:pool-admin`
- `default:pool-api`

### 요청 처리 시 실제 순서

1. `proxy.Handler`가 `runtime.State`에서 현재 snapshot을 읽는다.
2. `route.Resolve()`가 route table을 앞에서부터 검사한다.
3. `/api/admin/users`는 `admin:r-api-admin`에 먼저 매치된다.
4. 그 route가 가리키는 `admin:pool-admin`을 upstream registry에서 찾는다.
5. `pool.NextTarget()`이 healthy target 하나를 고른다.
6. `ReverseProxy`가 `10.0.1.10:9000`으로 요청을 보낸다.

즉 실제 요청 처리 시점에는 Raft log를 직접 읽지 않는다.
이미 만들어진 활성 상태 snapshot만 읽는다.

---

## health check는 구조 안에서 어디에 위치하는가

health check는 설정 파일이 아니라 runtime 상태의 일부다.

정확히 말하면:

- `spec.HealthCheckConfig`
  - 검사 정책
  - desired config 원본
- `upstream.HealthCheck`
  - 런타임 pool이 가진 검사 정책
- `upstream.TargetState`
  - 실제 검사 결과

즉 health check는 두 층으로 나뉜다.

1. 어떻게 검사할지
2. 지금 검사 결과가 어떤지

현재 결과는 `upstream.Pool` 안의 `targetState`에 들어 있고, background checker가 주기적으로 갱신한다.

즉 health check 결과도 “현재 적용된 결과”의 일부다.
정확히는 snapshot 안의 `Upstreams`가 들고 있는 runtime 상태의 일부다.

---

## 왜 파일 스키마 타입과 런타임 타입이 따로 있는가

이건 이 아키텍처에서 가장 중요한 구분 중 하나다.

예를 들어 `spec.RouteConfig`와 `route.Route`는 이름이 비슷하지만 역할이 완전히 다르다.

### `spec.RouteConfig`

이건 desired config 원본이다.

의미:

- 사람이 JSON에 적은 값
- 아직 global ID가 없음
- upstream pool 참조도 로컬 이름
- regex도 문자열

### `route.Route`

이건 런타임 결과다.

의미:

- global route id를 가짐
- source 이름을 가짐
- upstream pool 참조도 전역 이름
- regex는 이미 컴파일됨
- 정렬 대상

즉 desired config 원본과 런타임 결과를 분리하지 않으면:

- JSON 스키마와 실행 로직이 섞이고
- 전역 ID와 컴파일된 regex 같은 런타임 필드가 원본 타입에 침투하고
- 구조가 빠르게 복잡해진다

그래서 현재 프로젝트는 이 둘을 의도적으로 분리한다.

같은 논리가 `spec.UpstreamPool` vs `upstream.Pool`에도 적용된다.

---

## 의존성 방향을 구체적으로 표현하면

현재 의도한 방향은 아래와 같다.

```text
main
  ↓
app
  ↓
boot / spec / state / raft / route / upstream / runtime / proxy / dashboard
```

좀 더 구체적으로 쓰면:

- `main`
  - `app`만 알아야 한다
- `app`
  - 각 패키지를 조립한다
- `proxy`
  - `runtime`, `route`, `upstream` 결과를 소비한다
- `dashboard`
  - `runtime`을 읽어서 응답을 만든다
- `route`
  - `spec` 원본을 읽어 런타임 route로 바꾼다
- `upstream`
  - `spec` 원본을 읽어 런타임 pool/registry로 바꾼다

여기서 중요한 금지 사항은:

- `route`가 `dashboard`를 알면 안 된다
- `upstream`이 `dashboard`를 알면 안 된다
- `boot`가 HTTP handler를 알면 안 된다

즉 “설정”, “정책”, “실행”을 섞지 않는 것이 핵심이다.

---

## 활성 상태 계층을 더 직접적으로 다시 설명하면

“활성 상태 계층은 현재 적용된 결과를 메모리에 보관하는 계층”이라는 말을 더 구체적으로 풀면 아래와 같다.

### 현재 적용된 결과란 무엇인가?

다음 변환의 결과다.

#### 입력

- `configs/app.json`
- Raft log/snapshot의 namespace desired config

#### 변환

- Raft desired state projection
- validation
- route 전역 ID 부여
- upstream 전역 ID 부여
- regex compile
- route 우선순위 정렬
- upstream health state 초기화

#### 결과

- `boot.AppConfig`
- `[]spec.LoadedConfig`
- `[]route.Route`
- `*upstream.Registry`

이 4개 묶음이 현재 적용된 결과다.

즉 활성 상태 계층은:

> desired state를 실제 요청 처리에 사용 가능한 형태로 바꾼 결과를 메모리에 보관하는 계층

이라고 이해하면 된다.

---

## 결론

현재 프로젝트의 구조를 가장 정확하게 한 문장으로 말하면 아래와 같다.

> 앱 설정 파일과 Raft desired state를 입력으로 받아, 이를 전역 route table과 upstream registry를 가진 runtime snapshot으로 컴파일하고, 실제 요청은 항상 그 활성 snapshot을 기준으로 route resolve -> upstream 선택 -> reverse proxy 전달을 수행하는 구조

이 문장을 기준으로 보면 각 패키지의 역할도 자연스럽게 나뉜다.

- `boot`, `spec`
  - 입력 해석
- `route`, `upstream`
  - 런타임 구조 생성
- `runtime`
  - 현재 활성 상태 보관
- `app`
  - 전체 조립
- `proxy`
  - 실제 요청 처리
- `dashboard`
  - 현재 상태 조회

즉 이 프로젝트는 “설정을 읽는 코드”와 “실행하는 코드” 사이에 “해석된 활성 상태”를 명확히 두는 구조라고 보면 된다.
