# Raft VIP Failover OpenStack 중간 타이밍 테스트 - 2026-05-27

## 요약

C `4s/6s/4s/250ms`와 2초 profile `2s/2s/1s/250ms` 사이의 중간 타이밍 후보를 검증했다. 세 후보 모두 clean bootstrap, `node-2`/`node-3` join, leader hard kill, stale VIP cleanup, follower hard kill 무영향성을 통과했다.

가장 좋은 후보는 M3 `2500ms/4s/2s/250ms`였다. 첫 측정에서 leader hard kill 후 HTTP 200 복구가 4초였고, confirmation run에서도 5초로 확인됐다. 두 run 모두 반복 election timeout은 보이지 않았고, killed leader 재시작 후 stale VIP cleanup도 정상 동작했다.

## 테스트 환경

| 항목 | 값 |
| --- | --- |
| 환경 | OpenStack 3노드 |
| SSH 접근 | `ssh node-1`, `ssh node-2`, `ssh node-3` |
| VIP | `192.168.0.100/24` |
| interface | `ens3` |
| 외부 경로 | `https://ajoulb.ajou.app/api/info` |
| 원격 테스트 경로 | `/opt/ajoulb-vip-test` |

## 후보 결과

| Profile | Timing | Bootstrap HTTP 200 | Join 안정성 | Leader kill HTTP 200 | Follower kill 영향 | Churn | 판정 |
| --- | --- | ---: | --- | ---: | --- | --- | --- |
| M1 | `3s/5s/3s/250ms` | 10초 | 통과 | 5초 | 11개 샘플 모두 HTTP 200 | 반복 election timeout 없음 | 통과 |
| M2 | `3s/4s/2s/250ms` | 13초 | 통과 | 5초 | 11개 샘플 모두 HTTP 200 | 반복 election timeout 없음 | 통과 |
| M3 | `2500ms/4s/2s/250ms` | 10초 | 통과 | 4초 | 11개 샘플 모두 HTTP 200 | 반복 election timeout 없음 | 추천 |
| M3 confirm | `2500ms/4s/2s/250ms` | 10초 | 통과 | 5초 | 미반복 | 반복 election timeout 없음 | 확인 통과 |

## Profile M1

적용 설정:

```json
{
  "raftHeartbeatTimeout": "3s",
  "raftElectionTimeout": "5s",
  "raftLeaderLeaseTimeout": "3s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 10초에 확인됐다.

```text
BOOTSTRAP_M1 POLL elapsed=10s status=200 lb_node=node-1 backend=node-1
```

`node-2`, `node-3` join 후 node-1만 VIP를 보유했다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음
```

leader `node-1` hard kill 후 첫 HTTP 200은 5초에 확인됐고, 새 `lb_node`는 `node-2`였다.

```text
LEADER_KILL_M1 POLL elapsed=5s status=200 lb_node=node-2 backend=node-1
```

hard kill 직후에는 killed node와 새 leader에 VIP가 동시에 보였지만, `node-1` 재시작 후 stale VIP는 제거됐다.

```text
node-1 after restart: VIP 없음
node-2 after restart: 192.168.0.100/24
node-3 after restart: VIP 없음
```

follower `node-3` kill 중 11개 샘플은 모두 HTTP 200이었다.

```text
FOLLOWER_KILL_M1_SAMPLE i=0 lb_node=node-2 server=node-1
FOLLOWER_KILL_M1_SAMPLE i=10 lb_node=node-2 server=node-2
```

선거 로그에는 killed node 대상 `requestVote` 실패와 정상 `pre-vote successful`, `entering leader state`가 있었고, 반복 `Election timeout reached`는 없었다.

## Profile M2

적용 설정:

```json
{
  "raftHeartbeatTimeout": "3s",
  "raftElectionTimeout": "4s",
  "raftLeaderLeaseTimeout": "2s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 13초에 확인됐다.

```text
BOOTSTRAP_M2 POLL elapsed=13s status=200 lb_node=node-1 backend=node-1
```

`node-2`, `node-3` join 후 node-1만 VIP를 보유했다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음
```

leader `node-1` hard kill 후 첫 HTTP 200은 5초에 확인됐고, 새 `lb_node`는 `node-2`였다.

```text
LEADER_KILL_M2 POLL elapsed=5s status=200 lb_node=node-2 backend=node-1
```

`node-1` 재시작 후 stale VIP는 제거됐고, node-2만 VIP를 보유했다. follower `node-3` kill 중 11개 샘플은 모두 HTTP 200이었다.

```text
FOLLOWER_KILL_M2_SAMPLE i=0 lb_node=node-2 server=node-1
FOLLOWER_KILL_M2_SAMPLE i=10 lb_node=node-2 server=node-2
```

M2는 M1과 같은 leader kill 복구 시간이었지만 bootstrap이 더 느렸고, M3보다 빠르지 않았다.

## Profile M3

적용 설정:

```json
{
  "raftHeartbeatTimeout": "2500ms",
  "raftElectionTimeout": "4s",
  "raftLeaderLeaseTimeout": "2s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 10초에 확인됐다.

```text
BOOTSTRAP_M3 POLL elapsed=10s status=200 lb_node=node-1 backend=node-1
```

`node-2`, `node-3` join 후 node-1만 VIP를 보유했다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음
```

leader `node-1` hard kill 후 첫 HTTP 200은 4초에 확인됐고, 새 `lb_node`는 `node-2`였다.

```text
LEADER_KILL_M3 POLL elapsed=4s status=200 lb_node=node-2 backend=node-1
```

`node-1` 재시작 후 stale VIP는 제거됐고, node-2만 VIP를 보유했다. follower `node-3` kill 중 11개 샘플은 모두 HTTP 200이었다.

```text
FOLLOWER_KILL_M3_SAMPLE i=0 lb_node=node-2 server=node-1
FOLLOWER_KILL_M3_SAMPLE i=10 lb_node=node-2 server=node-2
```

선거 로그에는 killed node 대상 `requestVote` 실패가 있었지만 반복 election timeout은 없었다.

## M3 Confirmation

M3를 한 번 더 clean state에서 적용해 leader hard kill만 재확인했다.

```text
BOOTSTRAP_M3_CONFIRM POLL elapsed=10s status=200 lb_node=node-1 backend=node-1
LEADER_KILL_M3_CONFIRM POLL elapsed=5s status=200 lb_node=node-3 backend=node-1
```

confirmation run에서도 `node-1` 재시작 후 stale VIP는 제거됐고, node-3만 VIP를 보유했다.

```text
node-1 after restart: VIP 없음
node-2 after restart: VIP 없음
node-3 after restart: 192.168.0.100/24
```

로그에는 반복 election timeout이 없었다.

## 최종 추천

최종 추천 profile은 M3다.

```json
{
  "raftHeartbeatTimeout": "2500ms",
  "raftElectionTimeout": "4s",
  "raftLeaderLeaseTimeout": "2s",
  "raftCommitTimeout": "250ms"
}
```

선정 이유:

- 기존 추천 C의 8초보다 빠른 4초, confirmation 5초를 기록했다.
- M1/M2도 5초로 좋았지만 M3가 첫 run에서 가장 빨랐다.
- M3는 두 번의 clean run 모두 반복 election timeout 없이 leader를 선출했다.
- follower kill은 외부 응답에 영향을 주지 않았다.
- killed leader 재시작 후 stale VIP cleanup이 정상 동작했다.

## 최종 원격 상태

테스트 종료 후 원격 세 노드는 M3로 clean start했다.

```text
FINAL_M3_BOOTSTRAP POLL elapsed=8s status=200 lb_node=node-1 backend=node-1

node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음
```

최종 설정은 세 노드 모두 다음 값이다.

```json
{
  "raftHeartbeatTimeout": "2500ms",
  "raftElectionTimeout": "4s",
  "raftLeaderLeaseTimeout": "2s",
  "raftCommitTimeout": "250ms"
}
```

## 해석

이번 결과는 `ElectionTimeout`을 2초까지 줄이는 것보다, heartbeat를 2.5~3초대로 낮추면서 election window를 4초 정도 확보하는 쪽이 더 안정적이고 빠르다는 신호다. `2s/2s/1s`는 bootstrap과 join은 가능했지만 leader kill 이후 election timeout churn이 있었고 복구도 9초였다. 반면 M3는 detection을 충분히 빠르게 만들면서도 vote 수집을 완료할 여유를 남겨 전체 복구 시간이 더 짧았다.

## 종합 정리

초기 OpenStack live test, 공격적 timing test, 중간값 test를 합친 전체 정리는 `docs/test-results/raft-ha-vip-openstack-timing-summary-2026-05-27.ko.md`에 기록했다.
