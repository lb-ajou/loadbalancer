# Raft VIP Failover OpenStack 실환경 테스트 - 2026-05-20

## 요약

2026년 5월 20일 OpenStack 기반 3노드 환경에서 Raft leader 기반 VIP failover를 실제 검증했다.

초기 실행에서는 HashiCorp Raft 기본 timing(`HeartbeatTimeout=1s`, `ElectionTimeout=1s`, `LeaderLeaseTimeout=500ms`)으로 node-2를 voter로 추가한 직후 2-voter quorum이 불안정해졌다. 이를 확인한 뒤 테스트용으로 Raft timeout 설정을 노출하고, OpenStack 노드 설정에 `HeartbeatTimeout=2s`, `ElectionTimeout=2s`, `LeaderLeaseTimeout=1s`, `CommitTimeout=250ms`를 적용했다.

조정 후 node-1 bootstrap, node-2/node-3 join, 각 노드 LB 중지, 재합류 흐름을 검증했다. proxy가 upstream으로 `X-AjouLB-LB-Node` request header를 전달하고 test-server가 `/api/info`의 `lb_node`로 이를 출력하도록 보강했다. 이 덕분에 외부 요청 한 번으로 실제 backend와 요청을 처리한 LB node를 함께 확인할 수 있었다.

node-1과 node-2 중지 케이스는 짧은 시간 안에 다음 leader가 VIP를 획득하고 HTTP 200을 유지했다. node-3 leader 중지 케이스는 약 40초 동안 election churn이 발생한 뒤 node-1이 leader/VIP owner로 수렴했다. 최종적으로 세 proxy는 모두 재기동됐고, VIP `192.168.0.100/24`는 node-1 `ens3`에만 존재했다.

## 테스트 환경

| 항목 | 값 |
| --- | --- |
| 실행일 | 2026-05-20 |
| 환경 | OpenStack 3노드 |
| SSH 접근 | `ssh node-1`, `ssh node-2`, `ssh node-3` |
| VIP | `192.168.0.100/24` |
| 외부 경로 | `https://ajoulb.ajou.app/api/info` |
| reverse proxy 대상 | VIP `192.168.0.100:8080` |
| interface | 세 노드 모두 `ens3` |
| 원격 테스트 경로 | `/opt/ajoulb-vip-test` |
| proxy image | `ajoulb-reverseproxy:openstack-vip` |
| backend image | `ajoulb-test-server:openstack-vip` |

## 관측성 보강

실제 테스트 중 기존 `/api/info` 응답은 backend node만 보여줘 어느 LB가 VIP를 소유하고 요청을 처리했는지 알기 어려웠다. 이를 위해 다음 경로를 추가했다.

1. proxy가 upstream request에 `X-AjouLB-LB-Node: <raftNodeId>`를 주입한다.
2. test-server가 `/api/info` 응답에 `lb_node` 필드를 추가한다.

응답 예:

```json
{
  "server": "node-2",
  "scenario": "openstack-vip",
  "hostname": "node-2",
  "port": "18081",
  "version": "v1",
  "health_status": 200,
  "lb_node": "node-1"
}
```

## 노드 정보

| 노드 | ens3 주소 | backend |
| --- | --- | --- |
| node-1 | `192.168.0.202/24` | `192.168.0.202:18081` |
| node-2 | `192.168.0.122/24` | `192.168.0.122:18081` |
| node-3 | `192.168.0.186/24` | `192.168.0.186:18081` |

## 적용한 테스트 설정

세 노드 모두 host network mode를 사용했다. proxy container는 VIP add/remove와 GARP 송신을 위해 `NET_ADMIN`, `NET_RAW` capability를 사용했다.

Raft 설정은 다음 값을 사용했다.

```json
{
  "raftHeartbeatTimeout": "2s",
  "raftElectionTimeout": "2s",
  "raftLeaderLeaseTimeout": "1s",
  "raftCommitTimeout": "250ms"
}
```

초기에는 더 긴 값(`5s`, `8s`, `3s`)도 시도했지만, bootstrap node의 seed import가 현재 5초 안에 leader를 기다리는 구조라 `ElectionTimeout=8s`에서는 bootstrap이 먼저 실패했다. 실환경 테스트에는 위 값을 사용했다.

## 실행 결과

| 단계 | 결과 |
| --- | --- |
| interface 확인 | 세 노드 모두 `ens3`, `192.168.0.x/24` 확인 |
| linux/amd64 빌드 | `reverseproxy`, `test-server` 빌드 성공 |
| image 전송 | `docker pussh --platform linux/amd64`로 세 노드 전송 성공 |
| backend 기동 | 세 노드 `:18081` health/info 응답 확인 |
| node-1 bootstrap | leader 선출, seed import, dashboard/proxy 기동 성공 |
| 초기 VIP | node-1 `ens3`에 `192.168.0.100/24` 존재 |
| 초기 외부 경로 | `https://ajoulb.ajou.app/api/info` HTTP 200 |
| node-2 join | 설정 복제 확인 |
| node-3 join | 최초 join HTTP 5초 timeout 후 재기동으로 기존 Raft state catch-up 성공 |
| 관측성 확인 | `/api/info`에 `lb_node` 포함 확인 |
| node-1 LB 중지 | node-2가 VIP 획득, `lb_node:"node-2"` |
| node-2 LB 중지 | node-3가 VIP 획득, `lb_node:"node-3"` |
| node-3 LB 중지 | 약 40초 후 node-1이 VIP 획득, `lb_node:"node-1"` |
| leader 정상 중지 시간 측정 | node-1 leader stop 후 stop 반환 기준 6초, 명령 시작 기준 17초에 HTTP 200 복구 |
| leader SIGKILL 시간 측정 | node-2 leader kill 후 kill 반환 기준 7초, 명령 시작 기준 14초에 HTTP 200 복구 |
| 최종 복구 | 세 proxy 모두 재기동, node-1만 VIP 보유 |

## 주요 관찰

초기 기본 timing에서는 node-1이 node-2를 voter로 추가한 뒤 반복적으로 election churn에 빠졌다. node-1 로그에는 pre-vote는 통과하지만 실제 vote 응답을 제때 확보하지 못해 election timeout이 반복되는 패턴이 보였다. TCP `7001` 연결성은 양방향으로 확인됐으므로 단순 포트 차단 문제는 아니었다.

Raft timeout 설정을 노출하고 OpenStack 환경에 맞게 늘린 뒤에는 node-2 join과 3노드 구성 유지가 가능했다. 다만 node-3의 첫 join 요청은 dashboard HTTP client 5초 제한에 걸려 실패했다. 이때 leader에는 node-3 voter 추가가 이미 반영되어 있었고, node-3을 Raft state 삭제 없이 재시작하자 follower로 시작해 로그를 따라잡았다.

## 노드별 LB 중지 테스트

### node-1 중지

node-1이 VIP owner인 상태에서 `docker compose stop proxy`를 실행했다. 약 10초 후 외부 경로는 HTTP 200을 반환했고, 응답에는 새 LB owner가 node-2로 표시됐다.

```text
HTTP/1.1 200 OK
{"server":"node-1","scenario":"openstack-vip","hostname":"node-1","port":"18081","version":"v1","health_status":200,"lb_node":"node-2"}

NODE1: Exited (0), VIP 없음
NODE2: Up, inet 192.168.0.100/24 ... ens3
NODE3: Up, VIP 없음
```

### node-2 중지

node-1을 재합류시킨 뒤 node-2 proxy를 중지했다. 외부 경로는 HTTP 200을 반환했고, node-3가 VIP를 보유했다.

```text
HTTP/1.1 200 OK
{"server":"node-3","scenario":"openstack-vip","hostname":"node-3","port":"18081","version":"v1","health_status":200,"lb_node":"node-3"}

NODE1: Up, VIP 없음
NODE2: Exited (0), VIP 없음
NODE3: Up, inet 192.168.0.100/24 ... ens3
```

### node-3 중지

node-2를 재합류시킨 뒤 node-3 proxy를 중지했다. 최초 5초 외부 요청은 timeout이었다. node-1/node-2 로그에서는 pre-vote는 통과하지만 실제 vote 응답이 늦어 election timeout이 반복되는 churn이 보였다. 이후 node-1이 term 16에서 leader가 되었고 VIP를 획득했다.

```text
curl: (28) Operation timed out after 5007 milliseconds with 0 bytes received

2026-05-20T06:02:42.369Z [INFO] raft: election won: term=16 tally=2
2026-05-20T06:02:42.369Z [INFO] raft: entering leader state: leader="Node at 192.168.0.202:7001 [Leader]"
```

수렴 후 외부 경로는 정상 복구됐다.

```text
HTTP/1.1 200 OK
{"server":"node-1","scenario":"openstack-vip","hostname":"node-1","port":"18081","version":"v1","health_status":200,"lb_node":"node-1"}
```

## Leader 장애 시 failover 시간

leader 장애 시간은 두 방식으로 별도 측정했다. `docker compose stop proxy`는 정상 종료 경로이므로 proxy가 signal을 받고 종료 훅을 실행할 시간이 포함된다. 반면 `docker compose kill proxy`는 프로세스가 즉시 종료되는 장애에 더 가깝다.

### 정상 종료 기준

node-1이 leader/VIP owner인 상태에서 `docker compose stop proxy`를 실행했다.

```text
START_UTC=2026-05-20T06:10:03Z
STOP_RETURN_UTC=2026-05-20T06:10:14Z
STOP_RETURN_AFTER=11s
POLL elapsed=11s since_stop=0s unavailable
POLL elapsed=13s since_stop=2s unavailable
POLL elapsed=15s since_stop=4s unavailable
POLL elapsed=17s since_stop=6s lb_node=node-2 backend=node-1
```

결과:

| 기준 | 시간 |
| --- | ---: |
| stop 명령 시작부터 첫 HTTP 200까지 | 17초 |
| stop 명령 반환부터 첫 HTTP 200까지 | 6초 |

정상 종료에서는 명령 자체가 약 11초 걸렸고, stop 반환 후 6초 시점에 node-2가 새 LB owner로 응답했다.

### 프로세스 급사 기준

node-2가 leader/VIP owner인 상태에서 `docker compose kill proxy`를 실행했다.

```text
START_UTC=2026-05-20T06:11:07Z
KILL_RETURN_UTC=2026-05-20T06:11:14Z
KILL_RETURN_AFTER=7s
POLL elapsed=7s since_kill=0s 502 Bad Gateway
POLL elapsed=9s since_kill=2s 502 Bad Gateway
POLL elapsed=10s since_kill=3s 502 Bad Gateway
POLL elapsed=11s since_kill=4s 502 Bad Gateway
POLL elapsed=12s since_kill=5s 502 Bad Gateway
POLL elapsed=13s since_kill=6s 502 Bad Gateway
POLL elapsed=14s since_kill=7s lb_node=node-3 backend=node-1
```

결과:

| 기준 | 시간 |
| --- | ---: |
| kill 명령 시작부터 첫 HTTP 200까지 | 14초 |
| kill 명령 반환부터 첫 HTTP 200까지 | 7초 |

프로세스 급사에서는 외부 nginx가 약 7초 동안 502를 반환했고, 이후 node-3가 새 LB owner로 응답했다.

중요한 차이도 확인됐다. SIGKILL은 기존 leader의 VIP 정리 로직을 실행하지 않으므로 node-2의 `ens3`에 stale VIP가 남았다. node-3도 새 leader로 VIP를 획득해 일시적으로 두 노드에 `192.168.0.100/24`가 동시에 존재했다. 외부 트래픽은 GARP 이후 node-3로 라우팅됐지만, 운영 관점에서는 프로세스 급사 또는 호스트 alive 상태의 proxy crash에 대해 follower 시작 시 stale VIP reconciliation이 필요하다. 테스트 후 node-2의 stale VIP는 수동으로 제거했고, 최종적으로 node-3만 VIP를 보유하는 상태를 확인했다.

## Startup stale VIP cleanup 재검증

위 SIGKILL 결과를 바탕으로 proxy 시작 시 local VIP를 먼저 idempotent하게 제거하도록 수정했다. 주기적인 reconciliation은 넣지 않았다. 의도는 process restart 경로에서 남아 있던 stale VIP만 제거하고, 운영 중 VIP flapping을 유발할 수 있는 반복 제거 루프는 피하는 것이다.

적용한 controller 규칙:

- `Run()` 시작 직후 `ReleaseTimeout`으로 제한된 startup cleanup을 1회 수행한다.
- leader event를 받아도 `VerifyLeader()`가 성공하기 전에는 VIP를 add하지 않는다.
- VIP add가 성공한 뒤에만 GARP를 전송한다.
- follower event를 받으면 `owned` 여부와 무관하게 local VIP remove를 시도한다.

linux/amd64 이미지를 재빌드해 세 노드에 배포한 뒤 node-2가 leader/VIP owner인 상태에서 다시 `docker compose kill proxy`를 실행했다.

```text
START_UTC=2026-05-20T06:48:23Z
KILL_RETURN_UTC=2026-05-20T06:48:34Z
POLL i=0s..60s 502 Bad Gateway 또는 timeout

NODE2_BEFORE_RESTART
inet 192.168.0.100/24 brd 192.168.0.255 scope global secondary ens3
ajoulb-proxy Exited (137)
```

node-2 proxy가 죽은 뒤 node-2 host에는 예상대로 stale VIP가 남았다. 이 상태에서 node-2 proxy를 재시작하자 startup cleanup이 실행되어 node-2의 stale VIP는 제거됐다.

```text
NODE2_AFTER_RESTART
inet 192.168.0.122/24 metric 100 brd 192.168.0.255 scope global dynamic ens3
VIP 없음
```

다만 이 재검증에서는 node-2가 죽은 뒤 60초 동안 다른 노드가 새 leader/VIP owner로 수렴하지 못했고, node-1/node-3 로그에서 pre-vote는 통과하지만 실제 vote 단계가 election timeout에 걸리는 churn이 반복됐다. 세 proxy를 동시에 재시작한 뒤 25초 지점에 node-2가 다시 leader/VIP owner가 되며 외부 경로가 복구됐다.

```text
RECOVER i=25s lb_node=node-2
{"server":"node-1","scenario":"openstack-vip","hostname":"node-1","port":"18081","version":"v1","health_status":200,"lb_node":"node-2"}
```

해석:

- startup cleanup은 process restart 경로에서 stale VIP 제거에 성공했다.
- cleanup은 VIP 중복 상태를 해소하지만, Raft election churn 자체를 해결하지는 않는다.
- 새 leader가 아직 없는 상태에서 stale VIP를 제거하면 일시적으로 VIP가 어느 노드에도 없는 상태가 될 수 있다.
- 따라서 이 변경은 stale VIP 안전장치이며, failover 시간 안정화는 별도의 Raft timing/join/voter 관리 문제로 남는다.

### 최종 상태

node-3를 재기동해 세 proxy를 모두 복구했다. 이후 leader failover 시간 측정과 startup stale VIP cleanup 재검증을 추가로 수행했다. 최종 상태는 node-2가 VIP를 단독 보유했다.

```text
NODE1: Up, VIP 없음
NODE2: Up, inet 192.168.0.100/24 ... ens3
NODE3: Up, VIP 없음

HTTP/1.1 200 OK
{"server":"node-2","scenario":"openstack-vip","hostname":"node-2","port":"18081","version":"v1","health_status":200,"lb_node":"node-2"}
```

## 추가 안정성 재검증: OpenStack timing 프로파일

위 재검증 이후 bootstrap seed import 경로의 `waitForRaftLeader`도 기존 5초 고정값이 아니라 `raftJoinTimeout` 30초를 사용하도록 맞췄다. 5초 고정값에서는 clean bootstrap 중 단일 노드 leader 선출이 조금만 늦어져도 seed import 단계에서 프로세스가 종료될 수 있었다.

수정된 linux/amd64 이미지를 다시 빌드해 세 노드에 배포했다.

```text
reverseproxy image digest: sha256:9862ac51204c3326fd5bbf9caf2774558fd5db9d1f5669ccce6053dedb3b2541
```

### 기존 2s/2s/1s 설정 재현

기존 OpenStack 원격 설정은 아래와 같았다.

```json
{
  "raftHeartbeatTimeout": "2s",
  "raftElectionTimeout": "2s",
  "raftLeaderLeaseTimeout": "1s",
  "raftCommitTimeout": "250ms"
}
```

seed import 대기 시간을 30초로 늘린 뒤 node-1 단독 bootstrap은 성공했다.

```text
2026-05-20T08:01:43.748Z [INFO] raft: election won: term=2 tally=1
2026-05-20T08:01:43.748Z [INFO] raft: entering leader state: leader="Node at 192.168.0.202:7001 [Leader]"

HTTP 200
{"server":"node-1","scenario":"openstack-vip","hostname":"node-1","port":"18081","version":"v1","health_status":200,"lb_node":"node-1"}
```

하지만 node-2를 voter로 추가하는 순간 node-1이 quorum contact 실패로 leader에서 내려갔다. 이후 pre-vote는 통과하지만 실제 vote가 2초 election timeout 전에 완료되지 못해 churn이 반복됐고, VIP가 어느 노드에도 없는 상태가 됐다.

```text
2026-05-20T08:04:02.051Z [INFO] raft: updating configuration: command=AddVoter server-id=node-2
2026-05-20T08:04:07.906Z [WARN] raft: failed to contact quorum of nodes, stepping down
2026-05-20T08:04:10.016Z [INFO] raft: pre-vote successful, starting election: term=3 tally=2 refused=0 votesNeeded=2
2026-05-20T08:04:14.184Z [WARN] raft: Election timeout reached, restarting election
```

결론적으로 2s/2s/1s 프로파일은 이 OpenStack 환경에서 join 안정성 테스트를 통과하지 못했다. `AddVoter` 직후 새 follower가 catch-up되기 전에 2노드 voter quorum이 형성되고, 1초 leader lease 및 2초 election window가 너무 짧게 작용했다.

### 8s/10s/8s 설정 재검증

원격 테스트 config만 아래처럼 조정하고 다시 clean bootstrap부터 진행했다.

```json
{
  "raftHeartbeatTimeout": "8s",
  "raftElectionTimeout": "10s",
  "raftLeaderLeaseTimeout": "8s",
  "raftCommitTimeout": "250ms"
}
```

node-1 단독 bootstrap은 약 12초 후 leader가 되었고 VIP를 획득했다.

```text
2026-05-20T08:08:56.753Z initial configuration
2026-05-20T08:09:08.838Z election won: term=2
NODE1: ens3 192.168.0.202/24 192.168.0.100/24
```

이후 node-2, node-3를 순차적으로 join했다. node-2 join 시 node-1은 VIP를 유지했고 복제도 `pipelining replication` 상태로 수렴했다. node-3 join 이후에도 VIP는 node-1 단독 보유였다.

```text
NODE1: ens3 192.168.0.202/24 192.168.0.100/24
NODE2: ens3 192.168.0.122/24
NODE3: ens3 192.168.0.186/24

HTTP 200
{"server":"node-1","scenario":"openstack-vip","hostname":"node-1","port":"18081","version":"v1","health_status":200,"lb_node":"node-1"}
```

### per-node hard kill 안정성 테스트

3노드 기준선에서 각 proxy를 한 번씩 `docker kill`로 종료했다.

| 장애 대상 | 당시 역할 | 결과 |
| --- | --- | --- |
| node-1 | leader, VIP owner | node-2가 새 leader/VIP owner가 됨 |
| node-3 | follower | 외부 응답 영향 없음 |
| node-2 | leader, VIP owner | node-3가 새 leader/VIP owner가 됨 |

node-1 leader kill 후 외부 polling에서는 첫 정상 응답이 측정 시작 기준 `15.320s`에 확인됐다. node-2 로그 기준으로는 heartbeat timeout 이후 leader 선출까지 약 `4.016s`가 걸렸다.

```text
2026-05-20T08:12:26.792Z heartbeat timeout reached, starting election
2026-05-20T08:12:30.808Z election won: term=3
2026-05-20T08:12:30.808Z entering leader state: leader="Node at 192.168.0.122:7001 [Leader]"

15.320 status=200 lb_node=node-2
```

node-3 follower kill은 0초부터 10.919초까지 모든 샘플이 HTTP 200이었고 `lb_node=node-2`가 유지됐다.

```text
0.000 status=200 lb_node=node-2
10.919 status=200 lb_node=node-2
```

node-2 leader kill 후에는 polling 시작 기준 `16.353s`에 첫 HTTP 200이 확인됐고, node-3가 새 LB owner가 됐다. node-3 로그 기준으로는 heartbeat timeout 이후 leader 선출까지 약 `3.483s`가 걸렸다.

```text
2026-05-20T08:16:59.514Z heartbeat timeout reached, starting election
2026-05-20T08:17:02.997Z election won: term=4
2026-05-20T08:17:02.997Z entering leader state: leader="Node at 192.168.0.186:7001 [Leader]"

16.353 status=200 lb_node=node-3
```

hard kill에서는 기존 leader가 VIP release hook을 실행하지 못하므로 killed node에 stale VIP가 남았다. 다만 새 leader가 GARP와 함께 VIP를 획득하면서 외부 경로는 새 leader로 수렴했다. 이후 killed node를 재시작하면 startup cleanup이 local stale VIP를 제거했다.

```text
NODE1_BEFORE_RESTART: ens3 192.168.0.202/24 192.168.0.100/24
NODE1_AFTER_RESTART:  ens3 192.168.0.202/24

NODE2_BEFORE_RESTART: ens3 192.168.0.122/24 192.168.0.100/24
NODE2_AFTER_RESTART:  ens3 192.168.0.122/24
```

최종 복구 상태는 세 proxy가 모두 실행 중이고 node-3만 VIP를 보유한다.

```text
NODE1: Up, VIP 없음
NODE2: Up, VIP 없음
NODE3: Up, inet 192.168.0.100/24 ... ens3

HTTP 200
{"server":"node-1","scenario":"openstack-vip","hostname":"node-1","port":"18081","version":"v1","health_status":200,"lb_node":"node-3"}
```

### 추가 결론

OpenStack 실환경에서는 단순히 stale VIP cleanup만으로는 충분하지 않았다. cleanup은 process restart 시 중복 VIP를 해소하지만, join/election churn은 Raft timing과 membership 변경 방식의 문제였다.

이번 테스트에서 `8s/10s/8s` 프로파일은 3노드 join과 per-node hard kill 안정성 테스트를 통과했다. 단, leader 장애 시 외부 200 복구까지는 약 15~16초 수준이었고, 이는 더 짧은 timeout을 쓰던 이전 6~7초 결과보다 느리다. 안정성과 failover 지연 사이의 trade-off가 확인됐다.

운영 설계상 가장 좋은 방향은 timeout을 무작정 줄이는 것이 아니라, 새 노드를 즉시 voter로 넣는 현재 join 흐름을 개선하는 것이다. non-voter catch-up 후 voter로 승격하는 staged join, join retry의 current leader redirect, down voter remove/rejoin 절차가 있으면 더 짧은 timeout에서도 안정성을 확보할 가능성이 높다.

## 로컬 코드 검증

실환경에서 확인된 Raft timing 문제와 관측성 문제를 반영하기 위해 다음 변경을 추가했다.

- `raftHeartbeatTimeout`
- `raftElectionTimeout`
- `raftLeaderLeaseTimeout`
- `raftCommitTimeout`
- proxy의 upstream request `X-AjouLB-LB-Node` header
- test-server `/api/info`의 `lb_node` 응답 필드
- VIP controller startup stale cleanup
- follower event의 idempotent local VIP remove

검증 결과:

| 검증 | 결과 |
| --- | --- |
| RED 테스트 | timeout 필드/매핑 미구현 상태에서 컴파일 실패 확인 |
| RED 테스트 | proxy upstream header 미구현 상태에서 header test 실패 확인 |
| RED 테스트 | test-server `lb_node` 미구현 상태에서 compile 실패 확인 |
| RED 테스트 | VIP startup cleanup 미구현 상태에서 remove signal timeout 확인 |
| `go test ./internal/vip -count=1` | 통과 |
| `go test -race ./internal/vip -count=1` | 통과 |
| `go test ./...` | 통과 |
| `scripts/agent-check.sh fast` | 직접 실행은 permission denied, `bash scripts/agent-check.sh fast`는 macOS `stat`의 `-c` 미지원으로 task file 탐지 실패 |

## 결론

OpenStack port security가 비활성화된 3노드 환경에서 Raft leader 기반 VIP failover는 동작했다. 최신 재검증 기준으로는 `8s/10s/8s` timing 프로파일에서 node-1, node-2, node-3 LB를 각각 hard kill했을 때 클러스터가 VIP를 단일 node로 수렴시키고 외부 경로를 복구했다.

기존 `2s/2s/1s` 프로파일은 clean bootstrap은 가능했지만 node-2 voter join 직후 leader stepdown과 election churn으로 실패했다. OpenStack 환경에서는 현재 join 방식과 짧은 timeout 조합이 안정적이지 않다.

leader hard kill 기준 외부 HTTP 200 복구는 약 15~16초 수준이었다. Raft 로그 기준으로는 heartbeat timeout 이후 실제 election 완료까지 약 3.5~4.0초가 걸렸다. follower hard kill은 외부 응답에 영향을 주지 않았다.

또한 SIGKILL 방식에서는 기존 leader가 VIP를 정리하지 못해 stale VIP가 남았다. startup cleanup 추가 후 process restart 경로에서는 이 stale VIP가 제거됨을 확인했다. VM 자체가 죽는 장애라면 해당 host가 VIP에 응답하지 않겠지만, 프로세스만 죽고 host network는 살아 있는 장애에서는 supervisor가 proxy를 재시작해야 cleanup 코드가 실행된다. 운영 후보 설정에서는 Raft timeout, join flow, voter 제거/재합류 정책을 추가로 다듬어야 한다.

추가 개선 후보는 다음과 같다.

- join HTTP timeout을 조정 가능하게 노출
- 새 노드를 voter로 즉시 추가하지 않고 non-voter catch-up 후 voter로 승격하는 staged join 흐름 검토
- 장시간 down된 voter에 대한 operator remove/rejoin 절차 또는 자동화 검토
- proxy process crash 시 supervisor restart 정책 명시
