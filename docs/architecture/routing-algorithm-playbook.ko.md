# 라우팅 알고리즘 추가 플레이북

## 목적

이 문서는 이 저장소에 새로운 라우팅 알고리즘을 추가할 때 반복해서 검토해야 하는 공통 절차를 정리한 플레이북이다.

특히 아래 질문에 바로 답할 수 있도록 작성한다.

- 새 알고리즘을 추가하려면 어디부터 봐야 하는가?
- 어떤 패키지와 파일을 보통 수정하게 되는가?
- 구현 전에 무엇을 먼저 결정해야 하는가?
- 단위 테스트와 compose 검증은 무엇을 확인해야 하는가?
- 문서와 하네스는 어디까지 함께 갱신해야 하는가?

이 문서는 `sticky_cookie`, `5_tuple_hash`, `least_connection` 추가 작업에서 반복된 패턴을 기준으로 정리한다.

---

## 먼저 결정할 것

새 알고리즘을 구현하기 전에 아래 항목을 먼저 고정한다.

### 1. 선택 기준

요청 하나가 들어왔을 때 target을 무엇으로 선택할지 먼저 정의한다.

예:

- round-robin index
- cookie 값
- 요청 메타데이터 해시
- target별 in-flight 요청 수

이 항목이 모호하면 이후 validation, 테스트, 문서가 모두 흔들린다.

### 2. 입력 데이터

알고리즘이 어떤 입력을 읽는지 고정한다.

예:

- route 설정값만 읽는가
- request header를 읽는가
- cookie를 읽는가
- runtime health 상태를 읽는가
- target별 runtime counter를 읽는가

가능하면 “어떤 순서로 어떤 값을 우선 사용하는지”까지 확정한다.

예:

- `Forwarded` 우선, 없으면 `X-Forwarded-For`, 없으면 `RemoteAddr`
- cookie가 유효하면 재사용, 아니면 fallback

### 3. healthy/unhealthy 처리

이 저장소의 모든 알고리즘은 healthy target 집합 위에서 동작해야 한다.

반드시 먼저 정할 것:

- unhealthy target을 완전히 후보에서 제외하는가
- 기존 선택값이 unhealthy가 되면 어떻게 fallback 하는가
- healthy target이 하나도 없으면 어떤 결과를 반환하는가

### 4. 동률 처리

선택 기준이 여러 target에 대해 같은 값을 만들 수 있으면 tie-break 규칙을 반드시 문서화한다.

예:

- round-robin으로 분산
- 현재 순서 유지
- lexical order 사용 금지

이 저장소에서는 편향을 줄이기 위해 tie-break에 round-robin을 재사용하는 방식이 보통 더 자연스럽다.

### 5. 상태 수명주기

runtime 상태를 추가로 추적하는 알고리즘이면 증가/감소 시점을 먼저 고정한다.

예:

- 요청 선택 직후 증가
- `ReverseProxy.ServeHTTP` 반환 직후 감소

이 항목을 먼저 정하지 않으면 경쟁 상태와 누수 버그가 쉽게 생긴다.

### 6. 공개 계약 변화

아래 중 어디까지 바뀌는지 먼저 판단한다.

- route `algorithm` 허용값만 추가되는가
- dashboard API 응답 설명이 바뀌는가
- runtime route 설명이 바뀌는가
- 타입 의미가 바뀌는가

---

## 공통 수정 지점

새 알고리즘 추가는 보통 아래 순서로 퍼진다.

### 1. `internal/spec`

역할:

- 설정 스키마의 허용값 정의
- validation 에러 메시지 갱신

보통 수정하는 파일:

- `internal/spec/config.go`
- `internal/spec/validate.go`
- `internal/spec/validate_test.go`

보통 추가하는 것:

- `RouteAlgorithm...` enum 상수
- `isValidRouteAlgorithm()` 허용값
- unknown algorithm 거부 테스트
- 새 알고리즘 허용 테스트

### 2. `internal/route`

역할:

- 원본 설정의 algorithm 값을 runtime route에 싣는다

현재 구조에서는 algorithm 문자열이 이미 route에 포함되므로, 새 알고리즘 추가 시 `internal/route` 변경이 항상 필요한 것은 아니다.

하지만 아래 경우에는 확인이 필요하다.

- route 기본값 해석이 달라지는 경우
- runtime route 필드가 새 의미를 가져야 하는 경우

### 3. `internal/proxy`

역할:

- 요청별 target 선택 분기
- request metadata 추출
- cookie/header 기반 fallback
- runtime release 수명주기 연결

보통 수정하는 파일:

- `internal/proxy/reverse_proxy.go`
- `internal/proxy/reverse_proxy_test.go`

보통 추가하는 것:

- `uses...()` 분기 함수
- 선택 helper
- request metadata 추출 helper
- release/noop release 연결
- 프록시 수명주기 테스트

### 4. `internal/upstream`

역할:

- healthy target 집합 계산
- 실제 balancing/hash/selection 구현
- target별 runtime 상태 추적

보통 수정하는 파일:

- `internal/upstream/balancer.go`
- `internal/upstream/balancer_test.go`

상태가 필요한 알고리즘이면 추가로 확인할 파일:

- `internal/upstream/upstream.go`
- `internal/upstream/compile.go`
- `internal/upstream/registry.go`

예:

- `least_connection`은 target별 active counter 저장소와 초기화가 필요했다

### 5. `internal/dashboard`

route algorithm이 설정 편집 API나 runtime view에 노출되는 구조를 바꾸면 확인한다.

보통 점검할 파일:

- `internal/dashboard/view.go`
- `internal/dashboard/api_test.go`

현재 구조에서는 algorithm 문자열을 이미 노출하므로, 새 enum 허용값 추가만으로 충분한 경우도 많다.

### 6. 문서

알고리즘 추가 시 문서는 거의 항상 함께 바뀐다.

보통 확인할 파일:

- `docs/api/dashboard-api.ko.md`
- `docs/architecture/architecture.ko.md`
- `docs/conventions/type-reference.ko.md`

필요 시 추가:

- compose 시나리오 README
- `composes/README.md`

---

## 알고리즘 유형별 고려사항

### cookie 기반

예: `sticky_cookie`

추가로 결정할 것:

- cookie 이름 규칙
- cookie 값 구조
- cookie가 가리키는 target이 사라지거나 unhealthy일 때 fallback 규칙
- 첫 선택 기준

필수 테스트:

- 첫 요청에서 cookie 설정
- 같은 cookie 재사용
- unhealthy fallback
- cookie 갱신

### request metadata hash 기반

예: `5_tuple_hash`

추가로 결정할 것:

- hash 입력 필드
- header 우선순위
- trusted header fallback
- 동일 입력의 stable selection 보장

필수 테스트:

- 동일 입력 재선택
- header 우선순위
- fallback
- unhealthy 제외

### runtime counter 기반

예: `least_connection`

추가로 결정할 것:

- 무엇을 counter로 볼지
- counter 저장 위치
- 증가/감소 시점
- long-lived request를 어떻게 볼지

필수 테스트:

- busy target 회피
- tie-break
- release 한 번만 수행
- unhealthy 제외
- 스트리밍/장기 연결 의미 설명

---

## 구현 절차 권장 순서

### 1. 작업 파일에서 요구사항을 고정한다

`HARNESS-STRICT` 요청이면 아래를 먼저 작성한다.

- 목표
- 성공 기준
- 가정
- 수정할 파일
- 수정할 함수/타입
- 테스트 계획

모호한 요구사항이 있으면 구현 전에 질문한다.

### 2. 알고리즘 정의를 한 문장으로 고정한다

좋은 정의 예:

- “같은 5-tuple 입력은 healthy target 집합이 같으면 같은 backend를 고른다”
- “healthy target 중 현재 in-flight 요청 수가 가장 적은 target을 고른다. 동률이면 round-robin”

이 한 문장이 이후 validation, 문서, 테스트의 기준이 된다.

### 3. validation부터 추가한다

새 enum 값이 schema에 들어가야 이후 구현과 설정 예시가 자연스럽게 연결된다.

권장 순서:

1. `config.go`
2. `validate.go`
3. `validate_test.go`

### 4. upstream 선택 로직을 구현한다

가능하면 선택 자체는 `internal/upstream`에 두고, request-specific metadata 추출만 `internal/proxy`에 둔다.

이유:

- balancing 정책과 request parsing 책임을 분리하기 쉽다
- 테스트도 더 작게 유지할 수 있다

### 5. proxy 수명주기를 연결한다

request 단위 상태가 있으면 `internal/proxy/reverse_proxy.go`에서 증가/감소 수명주기를 묶는다.

권장 방식:

- `target, release, ok := ...`
- `defer release()`

### 6. API/문서를 동기화한다

문서 갱신은 마지막이 아니라 구현이 안정화되는 즉시 같이 맞추는 편이 낫다.

체크 포인트:

- 허용 algorithm 목록
- 동작 설명
- fallback 규칙
- runtime 의미 정의

### 7. compose/tool 시나리오 필요 여부를 판단한다

다음 중 하나에 해당하면 compose/tool 시나리오를 강하게 권장한다.

- unit test만으로 직관적인 동작 확인이 어려움
- request header/cookie/health 상태 조합이 중요함
- 장기 요청이나 unhealthy backend 회피처럼 실제 흐름을 봐야 함

예:

- `sticky_cookie`
- `5_tuple_hash`
- `least_connection`

---

## 공통 테스트 체크리스트

### validation

- 새 algorithm 문자열을 허용한다
- 허용되지 않은 문자열은 계속 거부한다

### target 선택

- healthy target이 있으면 선택된다
- unhealthy target은 후보에서 제외된다
- tie-break 규칙이 테스트로 고정된다

### fallback

- 기존 선택 근거가 무효면 fallback 한다
- fallback 후 결과가 새 규칙과 일치한다

### deterministic or sticky behavior

아래 중 해당하는 항목을 본다.

- 같은 입력이면 같은 결과
- 같은 cookie면 같은 결과
- busy target이 있으면 다른 결과

### runtime 수명주기

상태가 있으면 반드시 본다.

- 증가 시점
- 감소 시점
- error/cancel path에서 누수 없음
- release 중복 호출 안전성

### request metadata

header, cookie, host, port 등을 읽으면 다음을 본다.

- 우선순위
- fallback
- parse 실패 시 동작

### documentation sync

- API 문서 허용값 반영
- architecture/type-reference 의미 반영
- compose README와 tool 사용법 반영

---

## compose/tool 시나리오 플레이북

실제 검증 시나리오가 필요하면 아래 패턴을 따른다.

### 파일 세트

- `composes/<scenario>/compose.yaml`
- `composes/<scenario>/README.md`
- Admin API로 기록할 route/upstream desired state 예시
- `tools/<scenario>.sh`
- 필요 시 `composes/README.md`

### 기본 규칙

- compose는 백엔드만 띄운다
- 프록시는 로컬에서 실행한다
- 프록시 route/upstream은 정적 JSON 파일이 아니라 Admin API나 테스트 스크립트의 API 호출로 만든다
- `/health`와 `/api/info`를 기준으로 동작을 본다
- unhealthy backend를 하나 포함하면 fallback 검증이 쉬워진다

### 스크립트가 최소한 확인해야 할 것

- `server` 필드를 파싱할 수 있어야 한다
- 기대 backend와 실제 backend를 비교해야 한다
- unhealthy backend가 나오면 실패해야 한다
- 실패 메시지는 원인을 바로 알 수 있게 써야 한다

### least_connection 같은 상태 기반 알고리즘 추가 시

- 느린 healthy backend
- 빠른 healthy backend
- unhealthy backend

이 세 조합이 가장 설명력이 높다.

---

## 문서 동기화 기준

새 알고리즘 추가 시 기본적으로 아래를 점검한다.

### `docs/api/dashboard-api.ko.md`

- route `algorithm` 허용값 목록
- runtime route 설명
- 알고리즘별 동작 설명

### `docs/architecture/architecture.ko.md`

- 프록시 설정 예시의 algorithm 목록
- 각 알고리즘의 한 줄 설명

### `docs/conventions/type-reference.ko.md`

- `RouteAlgorithm` enum 값 목록
- runtime route의 algorithm 의미

---

## 하네스 체크리스트

`HARNESS-STRICT`에서는 아래 순서를 지킨다.

1. `plan/tasks/<date>-<slug>.md` 작업 파일 작성
2. 모호한 요구사항 질문
3. 가정이 있으면 승인 받기
4. 수정 파일과 심볼 설명 후 승인 받기
5. 구현
6. `scripts/agent-check.sh fast`
7. 필요 시 compose/tool 수동 검증
8. `scripts/agent-commit.sh`로 커밋

주의:

- 승인받지 않은 파일이 실제 구현에서 필요해지면 작업 파일의 `Approved-Files`를 먼저 갱신해야 한다
- 함수 길이 제한에 걸리면 보조 함수로 먼저 분리한다
- `plan/` 아래 파일은 gitignore 대상이므로 검증 기준 문서이지 커밋 대상은 아니다

---

## 빠른 시작 템플릿

새 알고리즘을 추가할 때 최소한 아래 질문에 답하고 시작한다.

1. 입력은 무엇인가?
2. healthy 집합 위에서 어떻게 선택하는가?
3. unhealthy 또는 무효 입력이면 어떻게 fallback 하는가?
4. tie-break는 무엇인가?
5. runtime 상태가 필요한가?
6. validation, proxy, upstream, docs 중 어디를 바꾸는가?
7. compose/tool 시나리오가 필요한가?

이 질문에 답이 준비되지 않았다면 구현보다 설계부터 다시 고정하는 것이 낫다.
