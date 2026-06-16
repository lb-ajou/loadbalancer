# Raft와 Membership

## 기본 정책

서비스는 HashiCorp Raft를 사용한다. 리더 선출은 기본 Raft election 정책을 따른다.

우선순위 기반 leader election, preferred leader, preemptive failback은 첫 버전에 넣지 않는다.

## 단일 노드

단일 노드도 Raft cluster다.

시작 조건:

- 기존 Raft state 없음
- `bootstrap=true`
- `joinAddr` 없음

동작:

- single-node cluster bootstrap
- 자신이 leader가 됨
- admin write는 Raft log에 기록됨

## Join

새 노드는 기존 leader의 admin endpoint로 join 요청을 보낸다.

join 전제:

- join node의 Raft data dir이 비어 있어야 한다.
- node id는 cluster 전체에서 영구적으로 unique해야 한다.
- advertise address는 다른 노드가 접근 가능한 stable address여야 한다.

기존 state가 있으면 기본적으로 join을 거부한다.

## Membership API

필수 API:

- list servers
- join voter
- remove server

선택 API:

- leadership transfer
- demote voter to non-voter
- promote non-voter to voter

## 리더 선출 방식

요약:

```text
follower가 leader heartbeat/contact를 잃음
  -> 1x~2x heartbeat timeout 범위의 랜덤 timeout
  -> 먼저 timeout 된 voter가 candidate
  -> pre-vote
  -> request vote
  -> 과반수 획득 시 leader
```

표를 받으려면:

- voter여야 한다.
- cluster configuration에 포함되어야 한다.
- candidate log가 voter보다 뒤처지지 않아야 한다.
- 과반수 voter와 통신 가능해야 한다.

## Timeout 설정

권장 기본값은 환경별로 검증해야 한다.

초기값:

```text
HeartbeatTimeout:   1s
ElectionTimeout:    1s
LeaderLeaseTimeout: 500ms
CommitTimeout:      50ms
```

느린 가상화 환경 또는 WAN-like 환경에서는 더 길게 잡는다.

검증 규칙:

- `ElectionTimeout >= HeartbeatTimeout`
- `LeaderLeaseTimeout <= HeartbeatTimeout`
- 모든 timeout은 양수

## Leader Write

admin write는 leader에서만 성공한다.

Follower 처리 정책은 둘 중 하나를 선택한다.

1. `409 not_raft_leader`와 leader hint 반환
2. follower가 leader admin endpoint로 forwarding

제품 UX는 2번이 좋지만, 첫 구현은 1번으로 시작해도 된다. 단 dashboard는 leader 이동 또는 재시도 UX를 제공해야 한다.

## Quorum 상실

quorum을 잃으면:

- admin write 중단
- membership change 중단
- 기존 runtime snapshot으로 data plane 처리 계속
- VIP는 leader lease 상실 시 release

운영자는 quorum 복구 또는 수동 복구 runbook을 따라야 한다.

## Snapshot

Raft snapshot은 desired state를 복원할 수 있어야 한다.

FSM snapshot 내용:

- DesiredState 전체
- Version
- AppliedAt

복원 후에는 runtime projection을 다시 수행한다.

## Reset 정책

기존 Raft state를 삭제하는 행위는 destructive operation이다.

필수 안전장치:

- explicit CLI 또는 config flag
- startup log warning
- 가능한 경우 export backup
- node id/address 충돌 검증
