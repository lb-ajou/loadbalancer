# VIP Failover

## 기본 모델

VIP owner는 항상 현재 Raft leader다.

```text
Raft leader 획득
  -> VerifyLeader
  -> VIP add
  -> GARP announce

Raft leader 상실
  -> VIP remove
```

VIP 자체가 leader를 선출하지 않는다.

## Local Config

VIP 설정은 노드 로컬 config다.

예:

- enabled
- interface
- address CIDR
- garp count
- garp interval
- acquire delay
- release on shutdown

VIP 설정은 네트워크 interface와 subnet에 종속되므로 Raft desired state에 넣지 않는다.

## Acquire 절차

권장 절차:

1. `LeaderCh`에서 leader 획득 이벤트 수신
2. `AcquireDelay` 대기
3. `VerifyLeader`로 아직 leader인지 확인
4. interface에 VIP add
5. GARP 송신
6. owned state 기록

`AcquireDelay`는 이전 leader의 lease 만료와 network convergence를 고려한 완충 시간이다.

## Release 절차

다음 상황에서 VIP를 제거한다.

- follower 전환
- leader lease 상실
- 정상 shutdown
- startup stale cleanup

프로세스 시작 시 stale VIP가 local interface에 있으면 먼저 제거한다. 이전 비정상 종료로 VIP가 남아 있을 수 있기 때문이다.

## 선점 정책

첫 구현은 비선점형으로 둔다.

```text
node-1 leader
node-1 장애
node-3 leader
node-1 복구
  -> node-1은 follower
  -> node-3이 계속 VIP 보유
```

priority/preemptive failback은 별도 기능이다. 이 기능은 Raft leadership transfer, flapping 방지, operator intent를 함께 설계해야 한다.

## Linux 요구사항

VIP 기능은 Linux network capability가 필요하다.

- `CAP_NET_ADMIN`: address add/remove
- `CAP_NET_RAW`: gratuitous ARP

컨테이너 실행 시 해당 capability를 명시한다.

## 검증 항목

- bootstrap leader가 VIP를 보유한다.
- follower는 VIP를 보유하지 않는다.
- leader stop 후 새 leader가 VIP를 보유한다.
- 이전 leader 재기동 후 follower면 VIP를 보유하지 않는다.
- client ARP table이 GARP 후 새 MAC으로 갱신된다.
- 어떤 시점에도 살아 있는 follower가 VIP를 동시에 보유하지 않는다.

## 위험과 대응

- 같은 VIP를 외부 시스템이 이미 사용 중이면 충돌한다.
- GARP가 네트워크 장비 정책에 의해 무시될 수 있다.
- Docker Desktop/macOS 환경은 L2 VIP 검증에 적합하지 않다.
- 실제 검증은 Linux host 또는 Linux VM의 같은 L2 segment에서 수행한다.
