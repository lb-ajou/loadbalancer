# 관측성과 성능

## 관측성 목표

운영자는 다음 질문에 답할 수 있어야 한다.

- 현재 leader는 누구인가?
- 이 요청은 어떤 route와 target으로 갔는가?
- 어떤 upstream이 unhealthy인가?
- 502가 왜 발생했는가?
- Raft write latency가 증가했는가?
- transport connection pool이 포화됐는가?

## Metrics

Prometheus endpoint를 제공한다.

필수 metric:

- request count by route, status
- request duration histogram by route
- upstream request count by target, status
- upstream error count by reason
- selected target count
- active requests by route/pool/target
- health check status
- health check duration
- Raft role
- Raft term
- Raft commit index
- Raft applied index
- Raft apply duration
- admin API latency
- VIP owned gauge
- VIP transition count

가능하면 transport 관련 metric도 추가한다.

- connection dial count
- dial error count
- response header timeout count
- idle connection reuse 추정치

Go 표준 `http.Transport` 내부 pool 상태는 직접 노출되지 않으므로 instrumentation wrapper나 httptrace를 검토한다.

## Logs

Access log는 구조화한다.

필드:

- timestamp
- request id
- method
- host
- path
- route id
- namespace
- upstream pool
- target
- status
- duration
- upstream duration
- error reason
- node id
- raft role

Control log:

- Raft role transition
- membership change
- desired state apply
- projection failure
- health transition
- VIP acquire/release

## Tracing

첫 릴리스에서는 optional로 둔다.

추후 OpenTelemetry trace를 추가할 수 있도록 request context와 request id를 유지한다.

## Performance Targets

초기 목표는 환경별로 측정해 조정한다.

권장 benchmark:

- 단일 route 단일 target
- 단일 route 다중 target
- 다중 route host/path match
- health check enabled
- Raft write burst
- leader failover 중 data plane latency

기본 지표:

- RPS
- p50/p95/p99 latency
- error rate
- CPU
- memory
- fd count
- goroutine count
- backend active connections

## Transport Tuning

초기값은 보수적으로 시작하고 benchmark로 조정한다.

튜닝 값:

- `MaxIdleConns`
- `MaxIdleConnsPerHost`
- `MaxConnsPerHost`
- `IdleConnTimeout`
- `ResponseHeaderTimeout`
- dial timeout

튜닝 원칙:

- `MaxConnsPerHost`는 backend 보호와 throughput 사이의 상한이다.
- `MaxIdleConnsPerHost`는 burst 후 connection reuse에 영향을 준다.
- `ResponseHeaderTimeout`은 tail latency와 false 502 사이의 trade-off다.

## Hot Path 원칙

- 요청마다 Raft를 읽지 않는다.
- 요청마다 JSON decode를 하지 않는다.
- 요청마다 `http.Transport`를 만들지 않는다.
- route table은 precompiled matcher로 유지한다.
- upstream target URL은 registry build 시 parse한다.
- reverse proxy cache는 config generation 기준으로 pruning한다.
