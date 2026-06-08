# 디렉토리 컨벤션

## 목적

이 문서는 현재 저장소의 디렉토리 구조, 패키지 책임, 의존성 경계를 설명한다.

대상 독자는 다음과 같다.

- 코드를 읽고 유지보수하는 개발자
- 운영과 검증을 위해 실행 흐름을 파악해야 하는 사용자
- 이후 구조 변경 시 책임 경계를 확인해야 하는 기여자

디렉토리 책임, 공개 API, 타입 의미가 바뀌면 이 문서도 함께 갱신한다.

## 프로젝트 목표

이 저장소는 Raft 기반 상태 복제와 VIP failover를 포함한 L7 reverse proxy POC다.

현재 구현 방향은 아래와 같다.

- `configs/app.json`에서 프로세스 부트 설정을 로드한다.
- reverse proxy desired state는 Raft log/snapshot과 Admin API로 관리한다.
- desired state write 시 스키마와 참조 관계를 검증한다.
- 모든 namespace의 route를 하나의 전역 route table로 투영한다.
- 모든 namespace의 upstream pool을 하나의 전역 upstream registry로 투영한다.
- 활성 상태는 `runtime.Snapshot`으로 메모리에 보관한다.
- 요청은 현재 runtime snapshot을 기준으로 처리한다.
- Raft leader 상태를 기준으로 VIP acquire/release를 수행한다.

현재 범위 밖인 것:

- 파일 watch
- 프록시 route/upstream 정적 JSON 파일 로드
- L4 로드밸런싱

## 최상위 구조

### `README.md`

프로젝트를 처음 보는 사용자를 위한 진입 문서다.

역할:

- 프로젝트 목표와 기능 요약
- 실행, 빌드, 테스트 방법 안내
- 주요 문서 링크 제공
- 성능과 failover 측정 결과 요약

### `go.mod`, `go.sum`

단일 Go 모듈 정의와 의존성 잠금 파일이다.

규칙:

- 구조를 분리해야 할 명확한 이유가 없다면 하나의 모듈만 유지한다.
- 실제 구현은 `internal/` 아래에 둔다.

### `main.go`

실행 진입점이다.

역할:

- OS signal context 생성
- CLI 실행
- 에러 출력과 종료 코드 처리

규칙:

- `main.go`는 얇게 유지한다.
- 라우팅 정책, runtime 조립, 서버 wiring은 `internal/app` 또는 하위 패키지에 둔다.

### `Dockerfile`

컨테이너 이미지 빌드 정의다.

역할:

- Go builder stage에서 정적 바이너리 생성
- Alpine runtime image 구성
- 기본 설정 파일 복사
- `8080`, `9090` 포트 노출

### `configs/`

애플리케이션이 읽는 프로세스 부트 설정 디렉토리다.

현재 구조:

- `configs/app.json`

의도:

- proxy listen address, dashboard listen address, Raft data dir 같은 process-local 설정을 둔다.
- route/upstream desired state는 정적 파일이 아니라 Raft log/snapshot에 저장한다.

### `docs/`

아키텍처, API, 컨벤션, 운영 검증 문서의 공식 루트다.

현재 구조:

- `docs/api/`
- `docs/architecture/`
- `docs/conventions/`

규칙:

- 코드 구조와 책임 경계는 `docs/conventions/`에 둔다.
- 런타임 흐름과 설계 배경은 `docs/architecture/`에 둔다.
- HTTP API 계약은 `docs/api/`에 둔다.
- 날짜가 박힌 일회성 실험 결과나 작업 과정 문서는 기본적으로 커밋하지 않는다.

### `scripts/`

프로젝트 검증에 필요한 자동 실행 스크립트를 둔다.

현재 예:

- `scripts/raft-ha-cluster-smoke.sh`
- `scripts/raft-ha-vip-smoke.sh`

규칙:

- 특정 compose 시나리오와 강하게 묶인 smoke test는 `scripts/`에 둔다.
- 일반 수동 검증과 benchmark 보조 명령은 `tools/`에 둔다.

### `tools/`

운영, 실험, 수동 검증, benchmark용 보조 스크립트를 둔다.

현재 예:

- `tools/round-robin-check.sh`
- `tools/sticky-cookie-check.sh`
- `tools/5-tuple-hash-check.sh`
- `tools/least-connection-check.sh`
- `tools/benchmark-*.sh`

규칙:

- 핵심 앱 실행에 필수인 코드는 넣지 않는다.
- 시나리오 실행과 측정 보조에 집중한다.

### `composes/`

Docker Compose 기반 로컬 검증 환경을 둔다.

현재 예:

- `composes/route-basic/`
- `composes/lb-multi-upstream/`
- `composes/failure-healthcheck/`
- `composes/round-robin-check/`
- `composes/sticky-cookie-check/`
- `composes/5-tuple-hash-check/`
- `composes/least-connection-check/`
- `composes/raft-ha-cluster/`
- `composes/raft-ha-vip/`
- `composes/benchmark-check/`
- `composes/test-server/`

규칙:

- 시나리오별 목적과 실행 방법은 하위 `README.md`에 둔다.
- 공용 테스트 서버 코드는 `composes/test-server/`에 둔다.
- 일반 backend 시나리오는 프록시 앱 자체를 띄우지 않고, HA 시나리오는 프록시 노드까지 함께 띄울 수 있다.

## `internal/` 패키지 의도

실제 구현은 모두 `internal/` 아래에 둔다.

공통 규칙:

- 핵심 로직은 `internal/` 아래에 둔다.
- 패키지 이름은 짧고 책임이 드러나야 한다.
- `utils`, `helpers`, `common` 같은 모호한 이름은 피한다.
- 외부 API 응답 타입은 내부 runtime 타입을 직접 노출하지 않고 view model로 분리한다.

## 패키지별 책임

### `internal/app`

애플리케이션 wiring과 lifecycle orchestration 계층이다.

역할:

- boot config와 runtime 구성 연결
- Raft store와 FSM apply/restore callback 연결
- runtime snapshot 생성과 교체
- proxy handler와 dashboard handler 생성
- HTTP 서버 생성과 종료
- cluster bootstrap/join 흐름 연결
- VIP controller wiring

여기에 두지 않을 것:

- 세부 route matching
- upstream target 선택 알고리즘
- 원본 desired config 스키마
- Linux netlink/raw ARP 세부 구현

### `internal/boot`

프로세스 부트 설정 전용 패키지다.

현재 역할:

- `AppConfig` 정의
- `configs/app.json` 로드
- 기본값 적용
- 앱 레벨 설정 검증

여기에 들어가는 예:

- proxy listen address
- dashboard listen address
- Raft data dir

여기에 들어가면 안 되는 예:

- route 정의
- upstream pool 정의
- Raft node identity
- cluster-wide Raft timing
- VIP address/GARP 정책

이유:

부트 설정은 노드 로컬 process 설정이고, 프록시 desired state와 cluster-wide 정책은 Raft 상태로 관리하기 때문이다.

### `internal/cli`

운영용 CLI 계층이다.

현재 역할:

- `serve` 명령으로 앱 실행
- `cluster status`로 노드 cluster lifecycle 상태 조회
- `cluster bootstrap`으로 첫 노드 bootstrap 요청
- `cluster join`으로 다른 노드 join 요청

규칙:

- CLI는 dashboard lifecycle API client 역할만 한다.
- cluster 상태 변경의 실제 검증과 적용은 `internal/app`, `internal/dashboard`, `internal/state`, `internal/raft` 경계를 통과해야 한다.

### `internal/spec`

reverse proxy desired state의 원본 스키마와 검증을 담당한다.

현재 역할:

- namespace 단위 route/upstream desired config 정의
- route match, algorithm, upstream pool, health check 스키마 정의
- desired config validation

중요한 구분:

- 이 패키지는 desired state 표현을 담당한다.
- runtime route table이나 upstream registry를 만들지는 않는다.

### `internal/state`

Raft에 합의된 desired state 모델과 runtime projection 경계를 담당한다.

현재 역할:

- `DesiredState` 정의
- namespace별 desired config 관리 모델 정의
- cluster-wide VIP 정책 정의와 검증
- cluster-wide Raft timing 정책 정의와 검증
- desired state를 `runtime.Snapshot`으로 projection
- Admin/API 오류 표현에 필요한 state error 제공

중요한 구분:

- `state`는 합의된 목표 상태와 projection 경계를 담당한다.
- Raft log 저장, transport, FSM 구현은 `internal/raft`가 담당한다.

### `internal/raft`

HashiCorp Raft 기반 저장소 구현을 담당한다.

현재 역할:

- Raft node 생성과 종료
- command encode/decode
- FSM apply/snapshot/restore
- leader-only write 검증과 Raft apply
- node-local Raft metadata 저장/복원

규칙:

- Raft 내부 구현 세부사항은 `internal/app`이나 API 계층으로 새지 않게 한다.
- desired state 의미와 검증은 `internal/state`에 둔다.

### `internal/config`

여러 계층에서 공유하는 작은 실행 설정 DTO를 표현한다.

현재 역할:

- node identity 표현
- bind/advertise address 표현
- cluster-wide Raft timing 표현
- 현재 노드에 적용할 effective VIP config 표현
- VIP 활성 여부 판단

### `internal/route`

runtime 라우팅 정책 계층이다.

현재 역할:

- `spec.RouteConfig`를 runtime `route.Route`로 컴파일
- 전역 route ID 부여
- 전역 upstream pool 참조 부여
- regex 사전 컴파일
- 전역 route table 생성
- route 우선순위 정렬
- 요청 host/path에 대한 route resolve

중요한 규칙:

- 모든 namespace의 route는 하나의 전역 route table로 투영한다.
- route 적용 순서는 JSON 배열 순서가 아니라 고정 우선순위 규칙을 따른다.

현재 우선순위:

1. exact
2. prefix
3. regex
4. any

prefix 의미:

- 단순 문자열 prefix가 아니라 segment 기반 prefix다.

### `internal/upstream`

runtime upstream registry와 target selection 상태를 담당한다.

현재 역할:

- 모든 namespace의 upstream pool을 runtime pool로 컴파일
- 전역 pool ID 부여
- target URL 사전 파싱
- health state 보관
- healthy target 목록 관리
- round-robin target 선택
- stable hash target 선택
- least-connection target 선택과 active request count 관리

중요한 구분:

- upstream pool 설정 스키마는 `internal/spec`에 둔다.
- runtime registry, target health, in-flight count, target selection은 `internal/upstream`에 둔다.
- route별 algorithm 해석과 reverse proxy 호출은 `internal/proxy`가 담당한다.

### `internal/vip`

Raft leader 기반 VIP failover를 담당한다.

현재 역할:

- Raft leadership transition을 VIP acquire/release 동작으로 변환
- Linux interface에 VIP address 추가/삭제
- VIP 획득 후 Gratuitous ARP 송신

중요한 경계:

- leader election과 quorum 판정은 HashiCorp Raft를 신뢰한다.
- VIP address/GARP 정책은 Raft desired state다.
- VIP interface는 bootstrap/join에서 받는 node-local lifecycle 입력이다.
- `internal/app`은 controller wiring만 담당하고 netlink/raw ARP 세부 구현을 알지 않는다.
- Linux 권한이 필요한 구현은 build tag로 분리해 일반 개발 환경의 단위 테스트를 막지 않는다.

### `internal/runtime`

활성 메모리 상태를 관리한다.

현재 역할:

- process-local app config 보관
- Raft identity/timing 보관
- 적용할 VIP runtime config 보관
- Raft desired state에서 projection된 namespace metadata 보관
- 전역 route table 보관
- 전역 upstream registry 보관
- snapshot 읽기 제공
- snapshot 원자적 교체 지원

중요한 의도:

- runtime은 source of truth 대체물이 아니다.
- runtime은 desired state를 컴파일한 활성 상태 뷰다.

### `internal/proxy`

실제 reverse proxy 요청 전달을 담당한다.

현재 역할:

- 현재 runtime snapshot 조회
- route table 기준 route resolve
- route algorithm에 따른 upstream target 선택
- 선택된 upstream으로 요청 전달
- upstream transport pool 생성과 재사용

중요한 경계:

- `internal/proxy`는 desired config를 직접 수정하지 않는다.
- `internal/proxy`는 route/upstream runtime 객체가 제공하는 결정을 사용해 전달을 수행한다.

### `internal/dashboard`

Dashboard UI와 JSON API를 담당한다.

현재 역할:

- embedded dashboard HTML 제공
- cluster lifecycle page 제공
- namespace config 조회/저장/삭제 API 제공
- runtime/status/cluster 조회 API 제공
- cluster bootstrap/join lifecycle API 제공
- 내부 runtime/state 타입을 외부 응답용 view model로 변환

규칙:

- 내부 타입을 JSON으로 직접 노출하지 않는다.
- API 계약은 `docs/api/dashboard-api.ko.md`와 동기화한다.

## 의존성 방향

의도한 의존성 방향:

- `main` -> `cli`
- `cli` -> `app`, `boot`
- `app` -> `boot`, `config`, `state`, `raft`, `runtime`, `proxy`, `dashboard`, `vip`
- `state` -> `config`, `spec`, `route`, `upstream`, `runtime`
- `proxy` -> `runtime`, `route`, `upstream`
- `route` -> `spec`
- `upstream` -> `spec`
- `dashboard` -> `admin`, `runtime`, `state`, `spec`, `route`, `upstream`
- `admin` -> `state`, `spec`

서로 결합되면 안 되는 것:

- `route`는 `dashboard`를 알면 안 된다.
- `upstream`은 `dashboard`를 알면 안 된다.
- `boot`는 HTTP/UI 패키지를 알면 안 된다.
- `spec`은 runtime 패키지를 알면 안 된다.

## 네임스페이스 규칙

각 namespace는 Raft desired state map의 키로 관리한다.

전역 ID는 이 namespace를 붙여서 만든다.

- route ID: `<namespace>:<route.id>`
- upstream pool ID: `<namespace>:<pool.id>`

이유:

- 서로 다른 namespace에서 같은 로컬 ID를 허용하기 위해서
- runtime에서는 전역 유일성을 보장하기 위해서

## 설계 의도 요약

이 코드베이스는 의도적으로 네 계층을 분리한다.

1. process bootstrap 계층
2. desired state schema 계층
3. runtime projection/policy 계층
4. application/API wiring 계층

대응 관계:

- process bootstrap -> `internal/boot`, `internal/cli`
- desired state schema -> `internal/spec`, `internal/state`
- shared execution config -> `internal/config`
- runtime projection/policy -> `internal/route`, `internal/upstream`, `internal/runtime`
- application/API wiring -> `internal/app`, `internal/proxy`, `internal/dashboard`, `internal/admin`, `internal/raft`, `internal/vip`

특별한 이유가 없다면 이 분리는 유지한다.
