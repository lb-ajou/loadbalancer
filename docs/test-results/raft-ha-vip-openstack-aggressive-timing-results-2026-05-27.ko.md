# Raft VIP Failover OpenStack 공격적 타이밍 테스트 - 2026-05-27

## 요약

OpenStack 3노드 환경에서 기존 안정 baseline `8s/10s/8s/250ms`는 재실행하지 않고, 더 공격적인 Profile A, B, C, Optional을 순서대로 검증했다. 이후 초기 실험값이었던 `2s/2s/1s/250ms`도 같은 clean Raft state 절차로 추가 재검증했다. 테스트는 매 profile마다 Raft state를 clean하게 새로 만들고, `node-1` bootstrap, `node-2`/`node-3` join, leader hard kill, stale VIP cleanup, follower hard kill 무영향성을 확인하는 방식으로 진행했다.

가장 좋은 결과는 Profile C `4s/6s/4s/250ms`였다. Profile C는 join churn 없이 3노드 구성이 되었고, leader hard kill 후 외부 `https://ajoulb.ajou.app/api/info` HTTP 200 복구가 측정 시작 기준 8초에 확인됐다. Optional `4s/5s/4s/150ms`와 추가 `2s/2s/1s/250ms`는 모두 leader hard kill 복구가 9초로 C보다 느려 추천값에서 제외했다.

## 테스트 환경

| 항목 | 값 |
| --- | --- |
| 환경 | OpenStack 3노드 |
| SSH 접근 | `ssh node-1`, `ssh node-2`, `ssh node-3` |
| VIP | `192.168.0.100/24` |
| interface | `ens3` |
| 외부 경로 | `https://ajoulb.ajou.app/api/info` |
| 원격 테스트 경로 | `/opt/ajoulb-vip-test` |
| proxy image | `ajoulb-reverseproxy:openstack-vip` |
| backend image | `ajoulb-test-server:openstack-vip` |

## 사전 정리

초기 Profile A 실행에서 `docker compose down -v`만으로는 clean Raft state가 만들어지지 않는 문제가 확인됐다. 원격 compose는 Docker named volume이 아니라 bind mount `/opt/ajoulb-vip-test/data/raft:/app/data/raft`를 사용한다. 따라서 `down -v` 이후에도 기존 membership이 남아 있었고, node-1이 이전 3-voter configuration을 들고 단독 부팅되어 `node-2`, `node-3` vote를 기다리며 선출에 실패했다.

이후 모든 profile은 다음 절차로 clean state를 만들었다.

```text
docker compose down -v --remove-orphans
sudo ip addr del 192.168.0.100/24 dev ens3
mv /opt/ajoulb-vip-test/data/raft /opt/ajoulb-vip-test/data/raft.bak.<timestamp>
mkdir -p /opt/ajoulb-vip-test/data/raft
```

기존 Raft data는 삭제하지 않고 timestamp backup으로 이동했다.

## 결과 표

| Profile | Timing | Bootstrap HTTP 200 | Join 안정성 | Leader kill HTTP 200 | Follower kill 영향 | Stale VIP cleanup | 판정 |
| --- | --- | ---: | --- | ---: | --- | --- | --- |
| 2초 | `2s/2s/1s/250ms` | 7초 | 통과 | 9초 | 미측정 | 미측정 | 통과, 비추천 |
| A | `6s/8s/6s/250ms` | 13초 | 통과 | 12초 | 11개 샘플 모두 HTTP 200 | 통과 | 통과 |
| B | `5s/7s/5s/250ms` | 12초 | 통과 | 10초 | 11개 샘플 모두 HTTP 200 | 통과 | 통과 |
| C | `4s/6s/4s/250ms` | 13초 | 통과 | 8초 | 11개 샘플 모두 HTTP 200 | 통과 | 추천 |
| Optional | `4s/5s/4s/150ms` | 12초 | 통과 | 9초 | 11개 샘플 모두 HTTP 200 | 통과 | 통과, 비추천 |

## 2초 Profile

적용 설정:

```json
{
  "raftHeartbeatTimeout": "2s",
  "raftElectionTimeout": "2s",
  "raftLeaderLeaseTimeout": "1s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 7초에 확인됐다.

```text
BOOTSTRAP_2S POLL elapsed=7s status=200 lb_node=node-1 backend=node-1
```

이번 clean run에서는 과거와 달리 `node-2`, `node-3` join까지 통과했다. node-1은 VIP를 유지했고, AddVoter 직후 replication도 시작됐다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음

updating configuration: command=AddVoter server-id=node-2
pipelining replication: peer="{Voter node-2 192.168.0.122:7001}"
updating configuration: command=AddVoter server-id=node-3
pipelining replication: peer="{Voter node-3 192.168.0.186:7001}"
```

leader `node-1` hard kill 후 첫 HTTP 200은 9초에 확인됐고, 새 `lb_node`는 `node-3`였다.

```text
LEADER_KILL_2S POLL elapsed=9s status=200 lb_node=node-3 backend=node-1
```

다만 leader kill 이후 선거 과정에서는 `node-2`, `node-3` 로그에 election timeout 재시작이 여러 번 보였다.

```text
node-2: Election timeout reached, restarting election
node-3: Election timeout reached, restarting election
node-3: pre-vote successful, starting election: term=4 tally=2 refused=1 votesNeeded=2
node-3: entering leader state: leader="Node at 192.168.0.186:7001 [Leader]"
```

hard kill 직후에는 기존 leader `node-1`과 새 leader `node-3`에 VIP가 동시에 보였다. 이 추가 측정은 사용자 요청에 따른 2초 조건 재검증에 초점을 맞췄기 때문에 follower kill과 killed leader restart cleanup은 별도 반복하지 않았다. 이전 A/B/C/Optional에서 startup cleanup은 이미 반복 확인했다.

2초 profile은 bootstrap이 빠르고 이번 clean run에서는 join도 통과했지만, leader kill 복구가 C보다 느렸고 선거 과정에서 churn 신호가 더 컸다. 따라서 추천값으로는 선택하지 않는다.

## Profile A

적용 설정:

```json
{
  "raftHeartbeatTimeout": "6s",
  "raftElectionTimeout": "8s",
  "raftLeaderLeaseTimeout": "6s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 13초에 확인됐다.

```text
BOOTSTRAP POLL elapsed=13s status=200 lb_node=node-1 backend=node-1
```

`node-2`, `node-3` join 이후 node-1만 VIP를 보유했고, join churn은 보이지 않았다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음

updating configuration: command=AddVoter server-id=node-2
pipelining replication: peer="{Voter node-2 192.168.0.122:7001}"
updating configuration: command=AddVoter server-id=node-3
pipelining replication: peer="{Voter node-3 192.168.0.186:7001}"
```

leader `node-1` hard kill 후 첫 HTTP 200은 12초에 확인됐고, 새 `lb_node`는 `node-2`였다.

```text
LEADER_KILL_A POLL elapsed=12s status=200 lb_node=node-2 backend=node-1
```

hard kill 직후에는 `node-1`과 `node-2`에 stale/new VIP가 동시에 보였으나, `node-1` proxy 재시작 후 startup cleanup이 실행되어 `node-1`의 stale VIP가 제거됐다.

follower kill은 `node-3` 대상으로 재측정했고, 0초부터 10초까지 모든 샘플이 HTTP 200이었다.

```text
FOLLOWER_KILL_A_SAMPLE i=0 lb_node=node-2 server=node-3
FOLLOWER_KILL_A_SAMPLE i=10 lb_node=node-2 server=node-1
```

## Profile B

적용 설정:

```json
{
  "raftHeartbeatTimeout": "5s",
  "raftElectionTimeout": "7s",
  "raftLeaderLeaseTimeout": "5s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 12초에 확인됐다.

```text
BOOTSTRAP_B POLL elapsed=12s status=200 lb_node=node-1 backend=node-1
```

join은 churn 없이 통과했고, node-1만 VIP를 보유했다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음
```

leader `node-1` hard kill 후 첫 HTTP 200은 10초에 확인됐고, 새 `lb_node`는 `node-3`였다.

```text
LEADER_KILL_B POLL elapsed=10s status=200 lb_node=node-3 backend=node-1
```

`node-1` 재시작 후 stale VIP는 제거됐고, node-3만 VIP를 보유했다. follower `node-1` kill 중 11개 샘플은 모두 HTTP 200이었다.

```text
FOLLOWER_KILL_B_SAMPLE i=0 lb_node=node-3 server=node-1
FOLLOWER_KILL_B_SAMPLE i=10 lb_node=node-3 server=node-2
```

## Profile C

적용 설정:

```json
{
  "raftHeartbeatTimeout": "4s",
  "raftElectionTimeout": "6s",
  "raftLeaderLeaseTimeout": "4s",
  "raftCommitTimeout": "250ms"
}
```

clean bootstrap 후 첫 HTTP 200은 13초에 확인됐다.

```text
BOOTSTRAP_C POLL elapsed=13s status=200 lb_node=node-1 backend=node-1
```

join은 churn 없이 통과했고, node-1만 VIP를 보유했다.

```text
node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음

updating configuration: command=AddVoter server-id=node-2
pipelining replication: peer="{Voter node-2 192.168.0.122:7001}"
updating configuration: command=AddVoter server-id=node-3
pipelining replication: peer="{Voter node-3 192.168.0.186:7001}"
```

leader `node-1` hard kill 후 첫 HTTP 200은 8초에 확인됐고, 새 `lb_node`는 `node-3`였다.

```text
LEADER_KILL_C POLL elapsed=8s status=200 lb_node=node-3 backend=node-1
```

`node-1` 재시작 후 stale VIP는 제거됐고, node-3만 VIP를 보유했다. follower `node-1` kill 중 11개 샘플은 모두 HTTP 200이었다.

```text
FOLLOWER_KILL_C_SAMPLE i=0 lb_node=node-3 server=node-1
FOLLOWER_KILL_C_SAMPLE i=10 lb_node=node-3 server=node-2
```

## Optional

적용 설정:

```json
{
  "raftHeartbeatTimeout": "4s",
  "raftElectionTimeout": "5s",
  "raftLeaderLeaseTimeout": "4s",
  "raftCommitTimeout": "150ms"
}
```

clean bootstrap 후 첫 HTTP 200은 12초에 확인됐다.

```text
BOOTSTRAP_OPTIONAL POLL elapsed=12s status=200 lb_node=node-1 backend=node-1
```

join은 churn 없이 통과했고, node-1만 VIP를 보유했다. leader `node-1` hard kill 후 첫 HTTP 200은 9초에 확인됐고, 새 `lb_node`는 `node-3`였다.

```text
LEADER_KILL_OPTIONAL POLL elapsed=9s status=200 lb_node=node-3 backend=node-1
```

follower kill 중 11개 샘플은 모두 HTTP 200이었다.

```text
FOLLOWER_KILL_OPTIONAL_SAMPLE i=0 lb_node=node-3 server=node-1
FOLLOWER_KILL_OPTIONAL_SAMPLE i=10 lb_node=node-3 server=node-2
```

Optional은 통과했지만, C보다 failover 복구가 1초 느렸다. `CommitTimeout`을 250ms에서 150ms로 낮춘 변경은 leader failure detection과 election 완료 시간에는 유의미한 개선을 만들지 못한 것으로 해석한다.

## 추천값

추천 profile은 C다.

```json
{
  "raftHeartbeatTimeout": "4s",
  "raftElectionTimeout": "6s",
  "raftLeaderLeaseTimeout": "4s",
  "raftCommitTimeout": "250ms"
}
```

선정 이유:

- 모든 profile 중 leader hard kill HTTP 200 복구가 가장 빨랐다.
- `node-2`, `node-3` join 중 반복 election timeout이나 quorum churn이 보이지 않았다.
- killed leader 재시작 후 stale VIP cleanup이 정상 동작했다.
- follower kill은 외부 응답에 영향을 주지 않았다.
- Optional과 2초 profile은 더 공격적인 설정이었지만 C보다 복구가 느려 이점이 없었다.

## 최종 원격 상태

테스트 종료 후 원격 세 노드에는 Profile C를 적용하고 clean start했다.

```text
RESTORE_C_BOOTSTRAP POLL elapsed=15s status=200 lb_node=node-1 backend=node-1

node-1: 192.168.0.100/24
node-2: VIP 없음
node-3: VIP 없음
```

세 노드 config는 모두 다음 값이다.

```json
{
  "raftHeartbeatTimeout": "4s",
  "raftElectionTimeout": "6s",
  "raftLeaderLeaseTimeout": "4s",
  "raftCommitTimeout": "250ms"
}
```

## 후속 후보

이번 라운드에서는 `4s/6s/4s/250ms`가 가장 좋은 지점이었다. 더 줄이고 싶다면 다음은 timeout만 더 낮추기보다 membership 변경 방식을 개선하는 쪽이 우선이다.

- join 시 즉시 voter로 추가하지 않고 non-voter catch-up 후 voter 승격
- join HTTP timeout과 retry/leader redirect 정리
- down voter remove/rejoin 운영 절차 추가
- supervisor restart 정책 명시

## 후속 중간값 테스트

추가로 C와 2초 profile 사이의 중간 timing을 검증했다. 상세 결과는 `docs/test-results/raft-ha-vip-openstack-midpoint-timing-results-2026-05-27.ko.md`에 기록했다.

중간값 테스트 후 추천값은 M3 `2500ms/4s/2s/250ms`로 갱신됐다. M3는 첫 run에서 leader hard kill 후 HTTP 200 복구 4초, confirmation run에서 5초를 기록했고 반복 election timeout 없이 통과했다.

## 종합 정리

초기 OpenStack live test, 공격적 timing test, 중간값 test를 합친 전체 정리는 `docs/test-results/raft-ha-vip-openstack-timing-summary-2026-05-27.ko.md`에 기록했다.
