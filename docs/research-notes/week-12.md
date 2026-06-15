# 12주차 연구노트

## 진행 목표

11주차에는 로컬 Docker Compose 환경에서 Raft 설정 복제와 VIP ownership 이동을 확인하였다. 이번 주차에는 상태 전파와 VIP failover를 각각 다른 대조군과 비교하기 위한 기준을 정하고, 프로젝트에서 구현한 Raft leader 기반 구조를 더 실제 환경에 가까운 3노드 환경에서 확인하였다.

이번 주차의 비교 대상은 역할에 따라 분리하였다. Syncthing과 CephFS는 설정 파일이나 상태 파일이 여러 노드에 전파되는 시간을 비교하기 위한 대조군이다. Keepalived는 VIP 소유권 전환과 failover를 비교하기 위한 대조군이다. 프로젝트 구현은 Raft 설정 복제와 VIP 점유를 하나의 애플리케이션 안에서 처리하므로, 상태 전파와 VIP failover를 분리해서 측정해야 어떤 부분에서 차이가 나는지 확인할 수 있다.

## 진행 내용

먼저 대조군의 역할을 구분하였다. Syncthing은 노드 간 파일 동기화 방식으로 상태 파일을 전파하는 대조군이다. 이 방식에서는 node-1에 생성한 설정 파일이 node-2와 node-3에 모두 생길 때까지의 시간을 측정한다. CephFS는 여러 노드가 같은 분산 파일 시스템을 mount해서 사용하는 대조군이다. 이 방식에서는 node-1에서 생성한 파일이 node-2와 node-3에서 관측될 때까지의 시간을 측정한다. 두 대조군 모두 상태 전파 시간 비교에 사용하고, VIP failover 비교에는 사용하지 않는다.

원래 상태 전파 대조군으로는 etcd를 고려하였다. etcd는 분산 key-value store이므로 설정 값을 저장하고 여러 노드가 같은 값을 읽는 구조를 만들기에 적합하다. 그러나 이번 비교에서는 별도 key-value API를 사용하는 구조보다, 설정 파일이 여러 노드에 전파되는 방식의 차이를 확인하는 것이 더 직접적인 비교가 된다. 프로젝트의 기존 단일 노드 구조도 설정 파일을 읽어 runtime snapshot을 만드는 방식에서 출발했기 때문에, 파일 동기화 방식인 Syncthing과 공유 파일 시스템 방식인 CephFS를 대조군으로 두었다.

Keepalived 대조군은 VIP failover 기준으로 보았다. Keepalived는 VRRP를 사용해 여러 노드 중 MASTER가 VIP를 소유하고, BACKUP 노드는 MASTER 장애 시 VIP를 이어받는다. 따라서 Keepalived 대조군에서 볼 항목은 MASTER 장애 후 VIP owner가 바뀌는 시간, GARP 이후 client ARP cache가 새 owner를 가리키는지, 장애 중 외부 요청 실패 구간이 얼마나 지속되는지, stale VIP나 중복 VIP가 남는지이다. Keepalived는 상태 전파 대조군이 아니라 VIP 전파와 failover 대조군으로 사용하였다.

측정 환경은 3개 로드밸런서 노드와 3개 backend 서버를 기준으로 설정하였다. 프로젝트 구현 환경에서는 각 노드가 Raft cluster를 구성하고, 현재 leader가 VIP를 보유한다. 외부 요청은 VIP를 통해 들어오며, proxy는 upstream request에 `X-AjouLB-LB-Node` header를 넣어 어떤 로드밸런서 노드가 요청을 처리했는지 확인할 수 있도록 하였다. backend의 `/api/info` 응답에는 backend 이름과 함께 `lb_node` 값을 포함하도록 하여, 외부 요청 하나로 backend와 LB owner를 함께 확인하였다.

Raft 기반 구현의 실환경 확인은 OpenStack 3노드 환경에서 진행하였다. 세 노드는 같은 네트워크 대역에 있고, VIP는 `192.168.0.100/24`로 설정하였다. 각 노드는 backend server를 함께 실행하고, proxy는 host network 기반으로 외부 요청을 처리한다. VIP add/remove와 GARP 송신을 위해 proxy container에는 `NET_ADMIN`, `NET_RAW` 권한이 필요했다. 외부 확인 경로는 VIP로 들어오는 HTTP 요청이었고, 응답의 `lb_node` 값을 통해 현재 요청을 처리한 LB 노드를 확인하였다.

초기 확인에서는 `node-1`을 bootstrap leader로 시작하고, `node-2`, `node-3`을 순차적으로 join하였다. `node-1`은 leader가 된 뒤 VIP를 보유했고, 외부 요청은 HTTP 200으로 응답하였다. 이후 `node-2`, `node-3`을 cluster에 합류시켜 설정 복제를 확인하였다. 이때 route/upstream 설정이 Raft state로 복제되고, VIP는 현재 leader가 보유한다는 점을 확인하였다.

상태 전파 측정은 세 노드가 모두 실행 중인 상태에서 진행하였다. 프로젝트 구현은 leader의 namespace config API에 측정용 route를 `PUT`한 뒤, `node-1`, `node-2`, `node-3`의 `/api/namespaces/default/config`에서 모두 같은 route가 보일 때까지의 시간을 측정하였다. Syncthing과 CephFS는 node-1에 테스트 파일을 생성한 시점부터 node-2와 node-3 양쪽에서 모두 같은 파일이 확인될 때까지의 시간을 측정하였다.

VIP failover 측정은 프로젝트 구현과 Keepalived를 별도로 측정하였다. 프로젝트 구현은 현재 `lb_node`로 확인된 Raft leader/VIP owner의 proxy 컨테이너를 중단하거나 hard kill한 뒤, 외부 경로가 다시 HTTP 200과 새 `lb_node`를 반환하는 첫 시점까지를 failover time으로 보았다. Keepalived는 `docker compose down`으로 현재 VIP owner를 중단하고, 외부 HTTP 요청이 다시 200을 반환하는 첫 시점을 failover time으로 측정하였다. 두 측정은 모두 VIP owner 장애 후 외부 client 관점의 복구 시간을 확인하기 위한 것이다.

비교 메트릭은 두 범주로 나누었다. 상태 전파 비교에서는 프로젝트 구현의 Raft 설정 전파 시간과 파일 기반 대조군의 동기화 시간을 비교한다. VIP failover 비교에서는 leader 또는 VIP owner가 중단된 시점부터 외부 client가 다시 HTTP 200 응답을 받을 때까지의 시간, VIP owner 전환 여부, stale VIP 또는 중복 VIP 발생 여부를 기록한다.

상태 전파 측정에서는 Syncthing과 CephFS 대조군의 동기화 시간을 먼저 확인하였다. Syncthing은 10회 측정에서 평균 `9.512초`, 최소 `8.536초`, 최대 `12.580초`, 중앙값 `9.178초`를 기록하였다. CephFS는 10회 측정에서 평균 `0.401245초`, 최소 `0.150861초`, 최대 `0.788487초`, 중앙값 `0.352762초`를 기록하였다. 프로젝트 구현의 Raft 설정 전파 시간은 leader write 이후 세 노드 API에서 같은 desired state가 보이는 시점을 기준으로 측정하기로 하였고, timing 개선 후 같은 기준으로 반복 측정한다.

| 대조군 | 측정 기준 | 평균 | 최소 | 최대 | 중앙값 |
| --- | --- | ---: | ---: | ---: | ---: |
| Syncthing | node-1 파일 생성 후 node-2/node-3 모두 생성 확인 | `9.512초` | `8.536초` | `12.580초` | `9.178초` |
| CephFS | node-1 파일 생성 후 node-2/node-3 모두 생성 확인 | `0.401245초` | `0.150861초` | `0.788487초` | `0.352762초` |

Raft 기반 구현의 VIP failover는 OpenStack 실환경 측정 결과를 기준으로 확인하였다. 정상 종료 시나리오에서는 `node-1` leader를 `docker compose stop proxy`로 중단했고, 명령 시작 후 `17초`, stop 명령 반환 후 `6초`에 외부 경로가 HTTP 200으로 복구되었다. 프로세스 급사 시나리오에서는 `node-2` leader를 `docker compose kill proxy`로 중단했고, 명령 시작 후 `14초`, kill 명령 반환 후 `7초`에 외부 경로가 HTTP 200으로 복구되었다. 추가로 `node-3` leader 중지 케이스에서는 election churn이 발생하여 약 `40초` 뒤 node-1이 leader와 VIP owner로 수렴하였다.

Keepalived 대조군은 10회 측정에서 첫 200 복구 시간 평균 `4.090초`, 최소 `2.552초`, 최대 `5.940초`, 중앙값 `4.053초`를 기록하였다.

| 대상 | 장애 조건 | Failover time |
| --- | --- | ---: |
| 프로젝트 구현 Raft/VIP | leader stop 명령 시작 후 첫 HTTP 200 | `17초` |
| 프로젝트 구현 Raft/VIP | leader stop 명령 반환 후 첫 HTTP 200 | `6초` |
| 프로젝트 구현 Raft/VIP | leader kill 명령 시작 후 첫 HTTP 200 | `14초` |
| 프로젝트 구현 Raft/VIP | leader kill 명령 반환 후 첫 HTTP 200 | `7초` |
| 프로젝트 구현 Raft/VIP | leader 중지 후 election churn 발생 케이스 | 약 `40초` |
| Keepalived | `docker compose down` 후 첫 HTTP 200 평균 | `4.090초` |

테스트 중 Raft timing 문제도 확인하였다. 짧은 heartbeat/election 설정에서는 join 직후 voter quorum이 불안정해지거나 election churn이 반복될 수 있었다. 이 관찰은 프로젝트 구현의 상태 전파와 VIP failover 양쪽에 영향을 준다. 상태 전파 관점에서는 Raft quorum이 불안정하면 설정 write와 복제가 지연되거나 실패할 수 있다. VIP failover 관점에서는 leader 선출이 늦어지면 VIP owner 전환도 늦어진다. 프로젝트 구현은 프록시 설정 복제와 VIP owner 전환을 같은 Raft leader 상태에 묶기 때문에, Raft timing이 두 기능에 동시에 영향을 준다.

## 확인 및 결과

이번 주차에는 프로젝트 구현과 외부 대조군을 비교할 때 볼 기준을 정하였다. 단순히 RPS나 평균 latency만 비교하면 고가용성 기능의 차이를 설명하기 어렵다. 따라서 Syncthing/CephFS와의 비교에서는 파일 기반 상태 전파 시간을 주요 지표로 두었고, Keepalived와의 비교에서는 failover 시간, VIP owner 전환, stale VIP 처리를 주요 지표로 두었다.

Raft 기반 구현의 3노드 실환경 확인에서는 leader 기반 VIP failover가 동작하는 것을 확인하였다. leader가 바뀌면 새 leader가 VIP를 획득하고, 외부 요청은 다시 HTTP 200으로 복구되었다. 또한 `lb_node` 관측값을 통해 외부 요청이 실제 어느 LB 노드를 통과했는지 확인할 수 있었다.

한계도 확인하였다. kill 시나리오에서는 기존 leader가 정상 종료 절차를 실행하지 못하기 때문에 stale VIP가 남을 수 있었다. 또한 Raft timing이 짧으면 join과 election 과정에서 불안정성이 나타날 수 있었다. 프로젝트 구현은 설정 복제와 VIP failover를 한 애플리케이션에서 처리한다는 장점이 있지만, Raft timing과 membership 변경 방식이 상태 전파 안정성과 VIP failover 품질에 직접 영향을 준다.

상태 전파 대조군에서는 CephFS가 가장 짧은 전파 시간을 보였다. CephFS는 공유 파일 시스템을 통해 같은 파일 시스템 상태를 관측하므로 평균 `0.401245초`로 1초 미만의 결과가 나왔다. Syncthing은 파일 변경을 감지하고 다른 노드로 전송하는 방식이므로 평균 `9.512초`의 초 단위 지연이 발생했다. 다만 CephFS는 별도 storage cluster와 mount 환경이 필요하고, Syncthing은 상대적으로 구성은 단순하지만 동기화 지연이 크다는 차이가 있다. 프로젝트 구현의 Raft 설정 전파는 단순 파일 동기화가 아니라 leader write 합의와 각 노드 API에서의 desired state 관측을 기준으로 보아야 하므로, 같은 기준의 반복 측정은 다음 주차 리팩토링 이후 수행한다.

VIP failover 비교에서는 프로젝트 구현의 복구 시간이 Keepalived보다 더 길고 변동이 큰 것을 확인하였다. 프로젝트 구현은 정상 종료 기준 명령 시작 후 `17초`, 프로세스 급사 기준 명령 시작 후 `14초`에 외부 HTTP 200으로 복구되었고, election churn이 발생한 케이스에서는 약 `40초`까지 늘어났다. 반면 Keepalived는 10회 측정에서 첫 200 복구 시간 평균 `4.090초`, 최대 `5.940초`를 기록하였다. 이 결과를 통해 Raft leader 선출과 VIP 획득을 하나로 묶은 구조에서는 Raft timing과 election 안정성이 failover time에 직접적인 영향을 준다는 점을 확인하였다.

## 다음 주차 계획

13주차에는 이번 주차에서 확인한 Raft timing 문제와 stale VIP 문제를 바탕으로 리팩토링을 진행한다. 특히 heartbeat timeout, election timeout, leader lease timeout, commit timeout이 failover 시간과 join 안정성에 어떤 영향을 주는지 확인한다.

또한 프로세스 급사 후 stale VIP가 남는 문제를 줄이기 위해 startup cleanup과 follower 전환 시 VIP 제거 흐름을 보강하고, 실환경에서 재시작 후 stale VIP가 제거되는지 확인한다. 리팩토링 후에는 같은 성격의 failover 시나리오로 2차 측정을 진행한다.

## 관련 문서

- [테스트 전략](https://github.com/lb-ajou/loadbalancer/blob/main/docs/new-repo/test-strategy.ko.md)
- [관측성과 성능](https://github.com/lb-ajou/loadbalancer/blob/main/docs/new-repo/observability-performance.ko.md)
- [VIP Failover](https://github.com/lb-ajou/loadbalancer/blob/main/docs/new-repo/vip-failover.ko.md)
- [Raft VIP Failover OpenStack 실환경 테스트](https://github.com/lb-ajou/loadbalancer/blob/main/docs/architecture/raft-ha-vip-openstack-live-test-2026-05-20.ko.md)
- [Syncthing Documentation](https://docs.syncthing.net/)
- [Ceph File System](https://docs.ceph.com/en/latest/cephfs/)
- [Keepalived Introduction](https://keepalived.readthedocs.io/en/latest/introduction.html)
- [Keepalived Failover using VRRP](https://keepalived.readthedocs.io/en/latest/case_study_failover.html)
