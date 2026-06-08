# 13주차 연구노트

## 진행 목표

12주차에는 OpenStack 3노드 환경에서 Raft 기반 VIP failover와 상태 전파 시간을 대조군과 비교하였다. 이 과정에서 프로젝트 구현의 VIP failover 시간이 Keepalived보다 길게 나타났고, 일부 leader 중지 케이스에서는 election churn으로 복구 시간이 크게 늘어났다.

이번 주차에는 해당 결과를 바탕으로 Raft timing 값을 조정하고, leader 장애 후 HTTP 200 복구 시간이 어떻게 달라지는지 다시 측정하였다. 또한 프로세스 급사 시 stale VIP가 남는 문제를 실환경에서 확인하고, 재시작 후 local VIP cleanup이 동작하는지 함께 확인하였다.

## 진행 내용

먼저 12주차 측정에서 확인한 문제를 기준으로 개선 대상을 정하였다. 프로젝트 구현은 Raft leader가 VIP owner가 되는 구조이므로, leader 장애 후 새 leader 선출이 늦어지면 VIP 획득도 늦어진다. 12주차 측정에서는 leader stop 기준 첫 HTTP 200 복구가 명령 시작 후 `17초`, leader kill 기준 첫 HTTP 200 복구가 명령 시작 후 `14초`로 확인되었다. 또한 특정 leader 중지 케이스에서는 election timeout이 반복되어 약 `40초` 뒤에야 새 leader와 VIP owner가 수렴하였다.

이 문제를 줄이기 위해 Raft timing 값을 여러 profile로 나누어 측정하였다. 조정 대상은 `raftHeartbeatTimeout`, `raftElectionTimeout`, `raftLeaderLeaseTimeout`, `raftCommitTimeout` 네 가지였다. `HeartbeatTimeout`은 follower가 leader contact를 잃었다고 판단하는 속도에 영향을 주고, `ElectionTimeout`은 선거를 시작하고 quorum vote를 확보할 수 있는 시간 창에 영향을 준다. `LeaderLeaseTimeout`은 leader lease 상실 판단과 관련되고, `CommitTimeout`은 log commit 대기 시간에 영향을 준다.

측정은 OpenStack 3노드 환경에서 같은 절차로 반복하였다. 각 profile마다 기존 Raft state와 VIP를 정리한 뒤 `node-1`을 bootstrap하고, `node-2`, `node-3`을 join하였다. 이후 leader hard kill을 수행하고 외부 경로 `https://ajoulb.ajou.app/api/info`가 다시 HTTP 200을 반환하는 시간을 측정하였다. follower kill은 leader가 아닌 노드를 중단했을 때 외부 요청이 계속 200으로 유지되는지 확인하는 용도로 사용하였다.

초기에는 안정성을 우선한 profile부터 확인하였다. A profile은 `6s/8s/6s/250ms`, B profile은 `5s/7s/5s/250ms`, C profile은 `4s/6s/4s/250ms`로 설정하였다. 세 profile 모두 3노드 join은 통과했지만, failover 복구 시간은 A가 `12초`, B가 `10초`, C가 `8초`였다. C profile이 이 구간에서는 가장 짧았지만, 12주차 측정값과 비교했을 때 아직 충분히 빠른 결과는 아니었다.

이후 더 짧은 timing 후보를 확인하였다. `2s/2s/1s/250ms` profile은 clean state에서는 join까지 통과했지만, leader kill 이후 election timeout이 반복되었고 HTTP 200 복구 시간은 `9초`였다. 단순히 timeout을 짧게 줄이면 장애 감지는 빨라질 수 있지만, vote 수집 시간이 부족하면 선거가 반복되어 전체 failover 시간은 오히려 늘어날 수 있음을 확인하였다.

그 다음 C profile과 2초 profile 사이의 중간값을 측정하였다. M1은 `3s/5s/3s/250ms`, M2는 `3s/4s/2s/250ms`, M3는 `2500ms/4s/2s/250ms`로 설정하였다. M1과 M2는 leader hard kill 후 첫 HTTP 200 복구가 각각 `5초`였고, M3는 첫 측정에서 `4초`, 확인 측정에서 `5초`를 기록하였다. 세 profile 모두 반복 election timeout은 보이지 않았고, follower kill 중 외부 요청도 계속 HTTP 200으로 유지되었다.

| Profile | Raft timing | Join | Leader kill 후 HTTP 200 | 로그 상태 | 판정 |
| --- | --- | --- | ---: | --- | --- |
| 2초 | `2s/2s/1s/250ms` | 통과 | `9초` | election timeout 반복 | 비추천 |
| A | `6s/8s/6s/250ms` | 통과 | `12초` | 안정 | 통과 |
| B | `5s/7s/5s/250ms` | 통과 | `10초` | 안정 | 통과 |
| C | `4s/6s/4s/250ms` | 통과 | `8초` | 안정 | 통과 |
| M1 | `3s/5s/3s/250ms` | 통과 | `5초` | 반복 timeout 없음 | 통과 |
| M2 | `3s/4s/2s/250ms` | 통과 | `5초` | 반복 timeout 없음 | 통과 |
| M3 | `2500ms/4s/2s/250ms` | 통과 | `4초` | 반복 timeout 없음 | 권장 |
| M3 확인 | `2500ms/4s/2s/250ms` | 통과 | `5초` | 반복 timeout 없음 | 확인 통과 |

최종 권장값은 M3로 결정하였다. M3는 `HeartbeatTimeout`을 `2500ms`로 줄여 leader 장애 감지를 빠르게 만들면서도, `ElectionTimeout`을 `4s`로 유지해 vote 수집 시간을 확보하였다. `ElectionTimeout`을 `2s`까지 줄인 profile보다 선거가 안정적이었고, C profile보다 복구 시간이 짧았다. `CommitTimeout`을 `150ms`로 낮춘 Optional profile도 확인했지만 leader kill 복구 시간이 C보다 빠르지 않았기 때문에 최종 후보에서 제외하였다.

M3 권장값을 기준으로 5회 반복 측정도 진행하였다. 5회 모두 leader hard kill 후 외부 HTTP 200 복구에 성공했고, 죽었던 proxy를 재시작한 뒤 VIP는 단일 노드로 수렴하였다. Failover 복구 시간은 평균 `3.776초`, 최소 `2.996초`, 최대 `4.916초`, 중앙값 `3.686초`였다. 같은 환경에서 상태 전파 시간은 평균 `1.762초`, 최소 `0.917초`, 최대 `2.874초`, 중앙값 `1.400초`였다. Config write ack는 평균 `1.092초`, 최소 `0.617초`, 최대 `2.041초`, 중앙값 `0.784초`였다.

| 지표 | 평균 | 최소 | 최대 | 중앙값 |
| --- | ---: | ---: | ---: | ---: |
| Failover 복구 시간 | `3.776초` | `2.996초` | `4.916초` | `3.686초` |
| 상태 전파 시간 | `1.762초` | `0.917초` | `2.874초` | `1.400초` |
| Config write ack | `1.092초` | `0.617초` | `2.041초` | `0.784초` |

stale VIP 문제도 함께 확인하였다. leader를 `docker compose kill proxy`로 종료하면 기존 leader는 정상 종료 hook을 실행하지 못하므로 local interface에 VIP가 남을 수 있다. 새 leader가 VIP를 획득하면 외부 트래픽은 GARP 이후 새 leader로 수렴하지만, 죽었던 프로세스가 재시작되기 전까지는 중복 VIP가 관측될 수 있다. 이를 줄이기 위해 proxy 시작 시 local VIP를 한 번 제거하는 cleanup 흐름을 사용하고, 재시작 후 stale VIP가 제거되는지 확인하였다. 이 cleanup은 중복 VIP를 줄이는 안전장치지만, leader 선출 자체를 빠르게 만드는 기능은 아니다. 따라서 failover 시간 개선은 Raft timing 조정으로, stale VIP 해소는 startup cleanup과 재시작 절차로 나누어 보았다.

## 확인 및 결과

이번 주차 측정 결과, 프로젝트 구현의 VIP failover 시간은 12주차보다 크게 줄었다. 12주차에서는 leader kill 기준 첫 HTTP 200 복구가 명령 시작 후 `14초`였고, election churn 케이스에서는 약 `40초`까지 늘어났다. M3 권장값 적용 후 5회 반복 측정에서는 평균 `3.776초`, 최대 `4.916초`로 줄었다.

중요한 점은 가장 짧은 timeout이 가장 좋은 결과를 만들지는 않았다는 것이다. `2s/2s/1s/250ms`는 감지 자체는 빠르지만 vote 수집 시간이 부족해 election timeout이 반복되었다. 반면 M3는 heartbeat를 충분히 줄이면서 election window를 `4s`로 유지해 선거가 한 번에 끝날 가능성을 높였다. 이 결과를 통해 failover time은 장애 감지 시간뿐 아니라 leader 선출 안정성까지 함께 봐야 한다는 점을 확인하였다.

Keepalived 대조군과 비교하면 M3 적용 후 프로젝트 구현은 수 초 단위 failover 범위에 들어왔다. Keepalived는 12주차 측정에서 첫 HTTP 200 복구 평균 `4.090초`, 최대 `5.940초`를 기록하였다. 프로젝트 구현의 M3 5회 측정은 평균 `3.776초`, 최대 `4.916초`였으므로, 같은 절대 조건의 실험은 아니지만 failover 시간 범위는 대조군과 비교 가능한 수준으로 개선되었다.

다만 운영상 한계도 남았다. hard kill 상황에서는 기존 leader가 VIP를 직접 반납하지 못하므로 stale VIP가 남을 수 있다. startup cleanup은 재시작 시 stale VIP를 제거하지만, 프로세스가 죽은 채 오래 남아 있는 상황을 바로 해결하지는 못한다. 따라서 운영 환경에서는 proxy 재시작 정책, 장시간 down된 voter 처리, stale VIP 관측과 알림이 함께 필요하다.

## 다음 주차 계획

14주차에는 클러스터링 기능에서 아직 부족한 부분을 보완한다. 특히 Raft timing 값을 운영 설정으로 관리하는 방식, join node가 기존 cluster의 timing을 가져오는 흐름, VIP와 Raft runtime 상태를 dashboard/API에서 확인하는 기능을 보강한다.

또한 이번 주차에서 확인한 운영 한계를 바탕으로, node 재시작과 membership 변경 시나리오를 더 명확하게 만든다. 장시간 down된 node가 다시 들어올 때 기존 Raft state를 어떻게 처리할지, stale VIP를 어떻게 확인할지, follower kill과 leader kill을 어떤 기준으로 구분해 테스트할지도 함께 다룬다.

## 관련 문서

- [Raft VIP Failover OpenStack 실환경 테스트](https://github.com/lb-ajou/reverseproxy-poc/blob/main/docs/architecture/raft-ha-vip-openstack-live-test-2026-05-20.ko.md)
- [Raft VIP Failover OpenStack 공격적 타이밍 테스트](https://github.com/lb-ajou/reverseproxy-poc/blob/main/docs/architecture/raft-ha-vip-openstack-aggressive-timing-results-2026-05-27.ko.md)
- [Raft VIP Failover OpenStack 중간 타이밍 테스트](https://github.com/lb-ajou/reverseproxy-poc/blob/main/docs/architecture/raft-ha-vip-openstack-midpoint-timing-results-2026-05-27.ko.md)
- [Raft VIP Failover OpenStack Timing 종합 정리](https://github.com/lb-ajou/reverseproxy-poc/blob/main/docs/architecture/raft-ha-vip-openstack-timing-summary-2026-05-27.ko.md)
- [VIP Failover](https://github.com/lb-ajou/reverseproxy-poc/blob/main/docs/new-repo/vip-failover.ko.md)
- [Raft and Membership](https://github.com/lb-ajou/reverseproxy-poc/blob/main/docs/new-repo/raft-and-membership.ko.md)
