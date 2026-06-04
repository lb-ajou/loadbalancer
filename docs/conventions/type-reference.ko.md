# 타입 레퍼런스

## 목적

이 문서는 현재 프로젝트에 정의된 주요 타입들의 역할을 빠르게 파악하기 위한 참고 문서다.

특히 아래 상황에서 읽는 것을 의도한다.

- 처음 코드를 읽을 때
- 비슷해 보이는 타입들의 차이를 구분해야 할 때
- desired config 스키마 타입과 런타임 타입의 경계를 확인할 때
- 이후 기능을 추가하면서 어느 패키지에 타입을 둬야 할지 판단할 때

이 문서는 구현 세부사항 전체를 나열하기보다, 각 타입이 왜 존재하는지와 어떤 책임을 가지는지를 설명하는 데 집중한다.

---

## 타입 분류 기준

현재 프로젝트의 타입들은 크게 세 종류로 나뉜다.

### 1. 앱 부트 설정 타입

서버 프로세스를 어떻게 띄울지를 결정하는 타입이다.

예:

- listen 주소
- proxy 설정 디렉토리 경로

주로 `internal/boot`에 있다.

### 2. 설정 desired config 스키마 타입

Raft desired state에 저장되는 route/upstream 설정 구조를 표현하는 타입이다.

예:

- route 정의
- upstream pool 정의
- health check 설정

주로 `internal/spec`에 있다.

### 3. 런타임 타입

실제 요청 처리, 라우팅, 업스트림 선택, 현재 활성 상태 보관을 위해 사용하는 타입이다.

예:

- 전역 route table의 route
- 전역 upstream registry의 pool
- 현재 활성 snapshot

주로 `internal/route`, `internal/upstream`, `internal/runtime`에 있다.

---

## `internal/boot`

### `AppConfig`

역할:

- 앱 레벨 bootstrap 설정을 표현한다.

왜 필요한가:

- reverse proxy desired state와 서버 부트 설정은 lifecycle이 다르기 때문이다.
- 이 타입은 “서버를 어떻게 띄울지”를 표현하고, route/upstream 같은 프록시 정책은 담지 않는다.

대표 필드:

- `ProxyListenAddr`
- `DashboardListenAddr`
- `RaftNodeID`
- `RaftJoinAddr`

사용 위치:

- `main.go`
- `internal/app`
- `internal/runtime.Snapshot`

구분 포인트:

- `AppConfig`는 프록시 설정 전체가 아니다.
- 앱 실행을 위한 루트 설정이다.

---

## `internal/spec`

이 패키지 타입들은 모두 **proxy desired config 스키마**를 표현한다.

### `Config`

역할:

- namespace 하나의 route/upstream desired config를 표현한다.

대표 필드:

- `Name`
- `Routes`
- `UpstreamPools`

구분 포인트:

- 이 타입은 런타임 route table이 아니다.
- namespace 하나의 route/upstream desired config를 표현하는 원본 구조다.

### `LoadedConfig`

역할:

- Raft desired state에서 projection된 proxy config와 namespace metadata를 함께 보관한다.

대표 필드:

- `Source`
- `Path`
- `Config`

왜 필요한가:

- Raft namespace 기반 source를 유지해야 하기 때문이다.
- 이후 route/upstream를 runtime으로 컴파일할 때 `source:id` 전역 ID를 만들기 위해 사용한다.

구분 포인트:

- `Config`만 있으면 source namespace를 잃는다.
- `LoadedConfig`는 “namespace 하나를 projection한 결과”를 표현한다.

### `RouteConfig`

역할:

- namespace desired config 안의 route 정의 하나를 표현한다.

대표 필드:

- `ID`
- `Enabled`
- `Match`
- `Algorithm`
- `UpstreamPool`

왜 필요한가:

- desired config의 route 정의를 그대로 유지하기 위해서다.
- 검증과 decode는 이 타입 기준으로 수행된다.

구분 포인트:

- 런타임 route인 `route.Route`와 다르다.
- `RouteConfig`는 desired config 포맷이고, `route.Route`는 실행 포맷이다.

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
- 이 타입은 설정 파일 계약이며, 실제 요청 처리 시에는 `route.Route.Algorithm` 문자열로 내려간다.

### `RouteMatchConfig`

역할:

- route 안의 match 조건을 표현한다.

대표 필드:

- `Hosts`
- `Path`

### `PathMatchConfig`

역할:

- path 매칭 규칙의 원본 정의를 표현한다.

대표 필드:

- `Type`
- `Value`

### `PathMatchType`

역할:

- path match 종류를 표현하는 문자열 enum이다.

현재 값:

- `exact`
- `prefix`
- `regex`

### `UpstreamPool`

역할:

- desired config 안의 upstream pool 정의를 표현한다.

대표 필드:

- `Upstreams`
- `HealthCheck`

구분 포인트:

- 런타임 pool인 `upstream.Pool`과 다르다.

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

- JSON에서는 문자열로 유지하면서, Go 코드에서는 `time.ParseDuration`으로 파싱하기 쉽게 하기 위해서다.

### `ValidationError`

역할:

- desired config 검증 실패 하나를 표현한다.

대표 필드:

- `Field`
- `Message`

### `ValidationErrors`

역할:

- 여러 개의 validation error를 묶어서 `error`처럼 다루기 위한 타입이다.

왜 필요한가:

- 검증 실패를 한 건만 반환하지 않고 여러 건을 함께 보여주기 위해서다.

---

## `internal/route`

이 패키지 타입들은 **런타임 라우팅 정책**을 표현한다.

### `Route`

역할:

- 전역 route table에 올라간 런타임 route 하나를 표현한다.

대표 필드:

- `GlobalID`
- `LocalID`
- `Source`
- `Hosts`
- `Path`
- `Algorithm`
- `UpstreamPool`

구분 포인트:

- `Algorithm`은 컴파일된 런타임 문자열 값이다.
- 현재 `round_robin`, `sticky_cookie`, `5_tuple_hash`, `least_connection`을 사용하며, 프록시 핸들러가 이 값을 보고 upstream 선택 방식을 결정한다.

### `Route`

역할:

- 실제 요청 처리에 사용하는 런타임 route 하나를 표현한다.

대표 필드:

- `GlobalID`
- `LocalID`
- `Source`
- `Enabled`
- `Hosts`
- `Path`
- `UpstreamPool`

왜 필요한가:

- `RouteConfig`는 desired config 원본이고, `Route`는 런타임 컴파일 결과이기 때문이다.
- regex 사전 컴파일, 전역 ID, 전역 upstream pool 참조 같은 런타임 정보가 필요하다.

구분 포인트:

- `spec.RouteConfig`와 이름은 비슷하지만 역할이 다르다.

### `PathMatcher`

역할:

- 런타임 path match 로직에 필요한 정보를 담는다.

대표 필드:

- `Kind`
- `Value`
- `Regex`

왜 필요한가:

- regex는 요청마다 컴파일하면 비효율적이므로, route compile 시점에 미리 컴파일해서 들고 있어야 한다.

### `PathKind`

역할:

- 런타임 path match 종류를 나타내는 enum이다.

현재 값:

- `PathKindAny`
- `PathKindExact`
- `PathKindPrefix`
- `PathKindRegex`

구분 포인트:

- `spec.PathMatchType`는 파일 스키마용 문자열 enum이다.
- `route.PathKind`는 런타임 로직용 enum이다.

---

## `internal/upstream`

이 패키지 타입들은 **런타임 업스트림 선택 상태**를 표현한다.

### `Pool`

역할:

- 전역 upstream pool 하나의 런타임 표현이다.

대표 필드:

- `GlobalID`
- `LocalID`
- `Source`
- `Targets`
- `HealthCheck`
- `targetState`
- `next`

왜 필요한가:

- desired config 원본 pool 정의만으로는 round-robin 포인터나 target별 health 상태를 보관할 수 없기 때문이다.

구분 포인트:

- `spec.UpstreamPool`은 desired config 원본
- `upstream.Pool`은 실행 중 상태를 포함한 런타임 객체

### `Target`

역할:

- pool 안의 target 하나를 표현한다.

현재 필드:

- `Raw`

현재 의미:

- 아직 단순 `host:port` 문자열 래퍼다.
- 이후 필요하면 parsed URL, weight, 메타데이터를 확장할 수 있다.

### `TargetState`

역할:

- target별 health 상태를 보관한다.

대표 필드:

- `Healthy`
- `LastCheckedAt`
- `LastError`

왜 필요한가:

- health check 결과는 설정 파일이 아니라 runtime 메모리에만 유지해야 하기 때문이다.

### `HealthCheck`

역할:

- 런타임 pool에 복사된 health check 정책이다.

대표 필드:

- `Path`
- `Interval`
- `Timeout`
- `ExpectStatus`

구분 포인트:

- health check 결과가 아니라 검사 설정이다.

### `Registry`

역할:

- 전역 upstream pool 저장소다.

대표 기능:

- `Get(globalID)`
- `All()`

왜 필요한가:

- route resolve 결과가 가리키는 전역 pool ID로 빠르게 pool을 찾기 위해서다.

구분 포인트:

- `Registry`는 여러 개의 `Pool`을 보관하는 컨테이너다.

---

## `internal/runtime`

이 패키지 타입들은 **현재 활성 메모리 상태**를 표현한다.

### `Snapshot`

역할:

- 현재 서버가 사용 중인 활성 상태 전체를 묶는다.

대표 필드:

- `AppConfig`
- `ProxyConfigs`
- `RouteTable`
- `Upstreams`
- `AppliedAt`

왜 필요한가:

- route table과 upstream registry를 항상 같은 버전으로 읽어야 하기 때문이다.
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

왜 필요한가:

- 여러 요청 goroutine이 동시에 읽고, 이후 Raft apply/restore가 들어오면 안전하게 교체할 수 있어야 하기 때문이다.

---

## `internal/proxy`

이 패키지는 별도 도메인 타입보다는 handler 중심이지만, 하나 중요한 타입이 있다.

### `Handler`

역할:

- 현재 runtime snapshot을 읽고 실제 backend로 요청을 전달하는 HTTP handler다.

대표 필드:

- `state`
- `transport`

왜 필요한가:

- 요청마다 현재 활성 route table과 upstream registry를 읽어야 하기 때문이다.
- upstream 연결 재사용 정책을 handler 단위로 소유해야 하기 때문이다.

구분 포인트:

- `Handler`는 라우팅 정책을 정의하지 않는다.
- route/upstream이 계산한 결과를 사용해 실제 프록시를 수행한다.
- `transport`는 단순 의존성 주입 필드가 아니라 reverse proxy 전용 connection pool 정책의 소유자다.

### `transportConfig`

역할:

- reverse proxy가 사용할 upstream `http.Transport` 기본값 묶음을 표현한다.

대표 필드:

- `maxIdleConns`
- `maxIdleConnsPerHost`
- `maxConnsPerHost`
- `idleConnTimeout`
- `responseHeaderWait`

왜 필요한가:

- transport 튜닝 상수를 한 곳에 모아 재측정과 조정을 쉽게 하기 위해서다.

구분 포인트:

- 설정 파일에서 읽는 타입이 아니다.
- runtime transport 생성 전용의 내부 정책 타입이다.

---

## `internal/dashboard`

이 패키지에는 조회용 API 응답 타입들이 있다.

이 타입들은 **runtime 내부 타입을 외부 응답용으로 변환한 view model**이다.

### `SnapshotView`

역할:

- dashboard의 `/api/config` 응답 전체 구조다.

대표 필드:

- `AppConfig`
- `ProxyConfigs`
- `RouteTable`
- `Upstreams`
- `AppliedAt`

### `ProxyConfigView`

역할:

- namespace 단위 proxy config를 dashboard 응답용으로 표현한다.

### `ProxyRouteView`

역할:

- namespace desired config 안의 route 원본을 응답용으로 표현한다.

### `ProxyPoolView`

역할:

- namespace desired config 안의 upstream pool 원본을 응답용으로 표현한다.

### `RouteView`

역할:

- 전역 route table의 route를 응답용으로 표현한다.

### `PathMatcherView`

역할:

- runtime path matcher를 사람이 읽기 쉬운 문자열로 표현한다.

### `PathMatchView`

역할:

- 원본 path match 설정을 응답용으로 표현한다.

### `UpstreamPoolView`

역할:

- 전역 upstream registry의 pool을 응답용으로 표현한다.

왜 이런 view model이 필요한가:

- runtime 내부 타입은 JSON 응답으로 직접 노출하기에 적합하지 않다.
- 예를 들어 `upstream.Registry`는 내부 map 구조를 그대로 보여주지 않는다.
- dashboard는 내부 구조가 아니라 읽기 쉬운 관리용 응답을 줘야 한다.

---

## 자주 헷갈리는 타입 비교

### `boot.AppConfig` vs `spec.Config`

- `boot.AppConfig`
  - 앱 bootstrap 설정
  - listen 주소, Raft data dir 등 process-local 설정
- `spec.Config`
  - namespace 하나의 proxy desired config
  - routes, upstream pools 등

### `spec.RouteConfig` vs `route.Route`

- `RouteConfig`
  - JSON 원본 route 정의
- `Route`
  - 런타임 route table의 route
  - global ID, compiled regex, 전역 upstream pool 참조 포함

### `spec.UpstreamPool` vs `upstream.Pool`

- `spec.UpstreamPool`
  - JSON 원본 upstream pool 정의
- `upstream.Pool`
  - 런타임 pool
  - target state, round-robin 포인터 등 실행 상태 포함

### `spec.HealthCheckConfig` vs `upstream.HealthCheck`

- `HealthCheckConfig`
  - desired config 원본 health check 설정
- `HealthCheck`
  - 런타임 pool에 복사된 health check 설정

### `runtime.Snapshot` vs `upstream.Registry`

- `runtime.Snapshot`
  - 현재 활성 상태 전체
- `upstream.Registry`
  - 그 중 upstream 쪽만 모아둔 저장소

---

## 타입 추가 시 판단 기준

새 타입을 추가할 때는 먼저 아래를 판단한다.

### 1. 이 타입은 desired config 포맷인가?

그렇다면 보통 `internal/boot` 또는 `internal/spec`에 둔다.

### 2. 이 타입은 런타임 계산 결과인가?

그렇다면 보통 `internal/route`, `internal/upstream`, `internal/runtime`에 둔다.

### 3. 이 타입은 외부 API 응답용인가?

그렇다면 보통 `internal/dashboard`에 view model로 둔다.

### 4. 이 타입은 실제 요청 처리용 handler 상태인가?

그렇다면 `internal/proxy` 또는 `internal/app`에 둔다.

---

## 요약

현재 프로젝트의 타입들은 다음처럼 이해하면 된다.

- `boot`, `spec`
  - 부팅 설정과 desired config 스키마
- `route`, `upstream`
  - 실행을 위한 런타임 도메인 타입
- `runtime`
  - 현재 활성 상태를 묶는 타입
- `dashboard`
  - 외부 조회용 응답 타입
- `proxy`
  - 요청 처리용 handler 타입

이 구분이 무너지기 시작하면 설정 스키마, 런타임 상태, API 응답 구조가 서로 섞이게 된다.

특별한 이유가 없다면 이 경계는 유지하는 것이 원칙이다.
