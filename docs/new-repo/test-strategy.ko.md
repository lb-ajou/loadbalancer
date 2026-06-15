# 테스트 전략

## 테스트 계층

```text
unit test
integration test
raft cluster test
compose smoke
benchmark
failure injection
```

## Unit Test

대상:

- desired state validation
- route matcher
- route sort
- upstream selector
- target URL normalization
- sticky cookie encoding/validation
- error mapping
- local config validation

## Integration Test

대상:

- desired state to runtime projection
- admin API write to store
- store apply callback
- health checker state transition
- reverse proxy request forwarding
- stale reverse proxy cache pruning

## Raft Test

대상:

- single-node bootstrap
- three-node join
- leader write replication
- follower write rejection or forwarding
- leader failover
- old leader rejoin
- snapshot/restore
- remove voter
- join with existing state rejection
- reset-on-join explicit path

## VIP Test

Linux 환경에서 수행한다.

대상:

- leader owns VIP
- follower does not own VIP
- leader stop 후 새 leader owns VIP
- old leader rejoin 후 VIP 미보유
- GARP 후 client ARP update
- startup stale cleanup

Docker Desktop/macOS만으로는 최종 VIP 검증을 완료했다고 보지 않는다.

## Data Plane Test

대상:

- no matching route
- no healthy target
- round-robin distribution
- least-connection active counter release
- sticky cookie fallback
- five-tuple hash determinism
- response header timeout
- dial timeout
- backend partial response
- WebSocket/SSE if supported

## Security Test

대상:

- unauthenticated write rejection
- role-based authorization
- forbidden upstream target rejection
- forwarded header trust policy
- join API auth
- import replace permission

## Benchmark

필수 시나리오:

- 단일 route 단일 target
- 단일 route 3 target
- 100 routes path prefix
- sticky-cookie
- least-connection
- health check enabled
- Raft write burst와 data plane 동시 부하

각 benchmark는 다음을 기록한다.

- RPS
- p50/p95/p99
- error rate
- CPU
- memory
- fd
- goroutine

## CI Gate

기본 PR gate:

- `go test ./...`
- race-sensitive package `go test -race`
- lint
- API schema validation
- docs link check

Nightly:

- raft cluster integration
- compose smoke
- benchmark regression
- failure injection

## Acceptance Criteria

첫 릴리스 acceptance:

- fresh single-node cluster에서 namespace/route/pool 생성 후 트래픽 성공
- 3노드 cluster에서 leader write가 모든 노드에 반영
- leader kill 후 새 leader 선출과 트래픽 복구
- VIP enabled 환경에서 VIP failover 성공
- admin write는 인증 없이는 실패
- metrics와 access log로 route/target/error를 추적 가능
