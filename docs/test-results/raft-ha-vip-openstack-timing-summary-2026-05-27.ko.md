# Raft VIP Failover OpenStack Timing 종합 정리 - 2026-05-27

## 요약

OpenStack 3노드 환경에서 Raft leader 기반 VIP failover timing을 여러 차례 검증했다. 초기 실험은 기능 동작과 문제 지점 확인에 가까웠고, 이후 `8s/10s/8s/250ms` 안정 baseline에서 더 공격적인 profile로 좁혀가며 failover 시간을 줄였다.

최종 추천값은 M3 `2500ms/4s/2s/250ms`다.

```json
{
  "raftHeartbeatTimeout": "2500ms",
  "raftElectionTimeout": "4s",
  "raftLeaderLeaseTimeout": "2s",
  "raftCommitTimeout": "250ms"
}
```

M3는 첫 측정에서 leader hard kill 후 HTTP 200 복구 4초, confirmation run에서 5초를 기록했다. 두 run 모두 반복 election timeout 없이 leader를 선출했고, killed leader 재시작 후 stale VIP cleanup도 정상 동작했다.

## 테스트 환경

| 항목 | 값 |
| --- | --- |
| 환경 | OpenStack 3노드 |
| 노드 | `node-1`, `node-2`, `node-3` |
| VIP | `192.168.0.100/24` |
| VIP interface | `ens3` |
| 외부 검증 경로 | `https://ajoulb.ajou.app/api/info` |
| 원격 테스트 경로 | `/opt/ajoulb-vip-test` |
| proxy image | `ajoulb-reverseproxy:openstack-vip` |
| backend image | `ajoulb-test-server:openstack-vip` |

## 전체 실험 흐름

1. 초기 OpenStack live test에서 Raft VIP failover 동작을 확인했다.
2. 기본 Raft timing과 짧은 `2s/2s/1s` 설정에서 join/election churn 문제를 확인했다.
3. 안정 baseline `8s/10s/8s/250ms`로 3노드 join과 per-node hard kill을 검증했다.
4. A/B/C/Optional profile로 timeout을 단계적으로 줄였다.
5. 초기 2초 조건을 clean Raft state 기준으로 다시 측정했다.
6. C와 2초 profile 사이의 중간값 M1/M2/M3를 추가 검증했다.
7. 최종 추천값을 M3로 갱신하고 원격 클러스터도 M3로 clean start했다.

## 결과 요약표

| 단계 | Timing | Join | Leader kill HTTP 200 | 로그 안정성 | 판정 |
| --- | --- | --- | ---: | --- | --- |
| 기본값 | `1s/1s/500ms` | 불안정 | 미측정 | AddVoter 후 churn | 탈락 |
| 초기 2초 재현 | `2s/2s/1s/250ms` | 과거 실패, 이후 clean run 통과 | 9초 | kill 후 election timeout 반복 | 비추천 |
| 안정 baseline | `8s/10s/8s/250ms` | 안정 | 15-16초 | 안정 | 너무 느림 |
| A | `6s/8s/6s/250ms` | 안정 | 12초 | 안정 | 통과 |
| B | `5s/7s/5s/250ms` | 안정 | 10초 | 안정 | 통과 |
| C | `4s/6s/4s/250ms` | 안정 | 8초 | 안정 | 기존 추천 |
| Optional | `4s/5s/4s/150ms` | 안정 | 9초 | 안정 | C보다 느림 |
| M1 | `3s/5s/3s/250ms` | 안정 | 5초 | 반복 election timeout 없음 | 통과 |
| M2 | `3s/4s/2s/250ms` | 안정 | 5초 | 반복 election timeout 없음 | 통과 |
| M3 | `2500ms/4s/2s/250ms` | 안정 | 4초 | 반복 election timeout 없음 | 추천 |
| M3 confirmation | `2500ms/4s/2s/250ms` | 안정 | 5초 | 반복 election timeout 없음 | 확인 통과 |

## 주요 관찰

### 1. 단순히 timeout을 낮추면 빨라지지 않는다

`2s/2s/1s/250ms`는 bootstrap이 빠르고 clean run에서는 join도 통과했다. 그러나 leader hard kill 이후 `Election timeout reached, restarting election`이 여러 번 나타났고, HTTP 200 복구는 9초였다. C `4s/6s/4s/250ms`보다 더 공격적인 설정인데도 복구는 더 느렸다.

이 결과는 failover 시간이 단순히 failure detection timeout에만 비례하지 않음을 보여준다. 선거를 빠르게 시작해도 vote 수집과 leader 전환이 한 번에 끝나지 않으면 전체 복구 시간은 오히려 늘어난다.

### 2. `raftElectionTimeout`은 가장 민감한 축이다

`raftElectionTimeout`은 leader가 사라졌다고 판단하고 선거를 시작하는 시간인 동시에, OpenStack 네트워크 지연, pre-vote, vote RPC, quorum 확보가 모두 버텨야 하는 시간 창이다.

`ElectionTimeout=2s`에서는 이 창이 너무 좁아 leader kill 후 election timeout 재시작이 반복됐다. 반면 M3는 `HeartbeatTimeout=2500ms`로 감지는 빠르게 하되 `ElectionTimeout=4s`를 유지해 vote 수집 여유를 확보했다. 그 결과 복구 시간이 4-5초로 가장 짧았다.

### 3. `raftHeartbeatTimeout` 단축은 효과가 있었다

`8s/10s/8s`에서 `6s/8s/6s`, `5s/7s/5s`, `4s/6s/4s`로 줄이면서 leader hard kill 복구 시간이 15-16초에서 12초, 10초, 8초로 줄었다. 이후 M1/M2/M3에서 heartbeat를 3초~2.5초대로 낮추자 4-5초대까지 내려갔다.

다만 heartbeat를 줄일 때 election window도 함께 지나치게 줄이면 churn 위험이 커진다. 현재 OpenStack 환경에서는 `HeartbeatTimeout=2500ms`, `ElectionTimeout=4s`의 조합이 가장 좋은 균형점이었다.

### 4. `raftCommitTimeout`은 failover 개선에 큰 영향을 주지 않았다

Optional profile은 `4s/5s/4s/150ms`로 `CommitTimeout`을 250ms에서 150ms로 낮췄다. 하지만 leader kill 복구는 9초로 C보다 느렸다. 이 값은 config write latency에는 영향을 줄 수 있지만, leader failure detection과 VIP failover의 주된 레버는 아니었다.

### 5. clean Raft state 절차가 중요하다

원격 compose는 named volume이 아니라 bind mount `/opt/ajoulb-vip-test/data/raft:/app/data/raft`를 사용한다. 따라서 `docker compose down -v`만으로는 Raft state가 초기화되지 않는다. 초기 재실행에서 이 때문에 이전 3-voter membership이 남아 node-1 단독 bootstrap이 실패했다.

이후 모든 profile은 다음 방식으로 clean state를 만들었다.

```text
docker compose down -v --remove-orphans
sudo ip addr del 192.168.0.100/24 dev ens3
mv /opt/ajoulb-vip-test/data/raft /opt/ajoulb-vip-test/data/raft.bak.<timestamp>
mkdir -p /opt/ajoulb-vip-test/data/raft
```

기존 data는 삭제하지 않고 timestamp backup으로 이동했다.

### 6. stale VIP cleanup은 필요하지만 election churn 해결책은 아니다

SIGKILL 방식에서는 기존 leader가 VIP release hook을 실행하지 못하므로 killed node에 stale VIP가 남는다. 새 leader가 GARP와 함께 VIP를 획득하면 외부 트래픽은 새 leader로 수렴하지만, 일시적으로 두 노드에 같은 VIP가 존재할 수 있다.

startup cleanup 추가 후에는 killed proxy를 재시작할 때 local stale VIP가 제거됐다. 다만 cleanup은 VIP 중복 상태를 해소하는 안전장치이지, Raft election churn 자체를 해결하지는 않는다.

## 최종 추천값

운영 후보 timing은 M3다.

```json
{
  "raftHeartbeatTimeout": "2500ms",
  "raftElectionTimeout": "4s",
  "raftLeaderLeaseTimeout": "2s",
  "raftCommitTimeout": "250ms"
}
```

추천 이유:

- 기존 안정 baseline 15-16초 대비 4-5초대로 크게 개선됐다.
- 기존 추천 C 8초보다도 확실히 빠르다.
- 2초 profile보다 election churn이 적고 복구도 빠르다.
- 두 번의 clean run에서 반복 election timeout 없이 통과했다.
- follower kill은 외부 응답에 영향을 주지 않았다.
- killed leader 재시작 후 stale VIP cleanup도 정상 동작했다.

## 운영 해석

현재 기대 failover 시간은 보수적으로 4-5초대로 보는 것이 적절하다. 첫 M3 run은 4초였지만 confirmation run은 5초였으므로, 운영 SLO나 안내 문구에는 5초 수준으로 잡는 편이 안전하다.

`ElectionTimeout`은 너무 낮추지 않는 것이 좋다. 2초까지 줄이면 빠르게 선거를 시작할 수는 있지만, vote 수집이 한 번에 끝나지 않아 전체 복구가 느려질 수 있다. 지금 환경에서는 heartbeat를 2.5초까지 줄이고 election은 4초로 남기는 방식이 더 좋은 결과를 냈다.

## 후속 개선 제안

타이밍 튜닝만으로는 일정 수준 이상의 개선에 한계가 있다. 다음 단계에서는 Raft membership과 운영 절차를 개선하는 쪽이 효과적일 가능성이 높다.

- 새 노드를 즉시 voter로 추가하지 않고 non-voter catch-up 후 voter로 승격한다.
- join HTTP timeout과 retry, current leader redirect를 정리한다.
- 장시간 down된 voter의 remove/rejoin 절차를 문서화하거나 자동화한다.
- proxy process crash 시 supervisor가 빠르게 재시작하도록 운영 정책을 명시한다.
- host-alive/process-dead 상황에서 stale VIP가 오래 남지 않도록 restart 정책과 모니터링을 붙인다.

## 관련 문서

- `docs/test-results/raft-ha-vip-openstack-live-test-2026-05-20.ko.md`
- `docs/test-results/raft-ha-vip-openstack-aggressive-timing-results-2026-05-27.ko.md`
- `docs/test-results/raft-ha-vip-openstack-midpoint-timing-results-2026-05-27.ko.md`
