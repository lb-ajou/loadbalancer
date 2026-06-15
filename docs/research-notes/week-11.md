# 11주차 연구노트

## 진행 목표

9주차에는 Raft 기반 설정 복제 구조를 구현하였고, 10주차에는 Raft leader 상태를 VIP 점유와 연결하는 구조를 구현하였다. 이번 주차에는 두 기능을 멀티 노드 환경에서 함께 실행하며, 설정 복제와 VIP ownership 이동이 의도대로 동작하는지 확인하였다.

테스트는 크게 두 가지로 나누어 진행하였다. 첫 번째는 3개 proxy 노드가 Raft cluster를 구성하고 route/upstream 설정을 복제하는지 확인하는 `raft-ha-cluster` 테스트다. 두 번째는 leader가 VIP를 보유하고 leader 장애 후 새 leader가 VIP를 획득하는지 확인하는 `raft-ha-vip` 테스트다.

## 진행 내용

먼저 Docker Compose 기반 멀티 노드 환경을 구성하였다. `raft-ha-cluster` 시나리오는 3개의 proxy 노드와 3개의 backend server로 구성된다. proxy 노드는 각각 proxy HTTP port, dashboard/admin HTTP port, Raft transport address를 가진다. backend server는 `/health`와 `/api/info`를 제공하며, proxy routing 확인 시 `/api/info` 응답의 `server` 값이 `backend-a`, `backend-b`, `backend-c` 중 하나인지 확인한다.

`scripts/raft-ha-cluster-smoke.sh`는 클러스터 생성부터 failover와 persistence 확인까지 자동으로 수행한다. 먼저 Linux용 `reverseproxy`와 `test-server` binary를 빌드하고, backend와 `proxy-1`을 시작한다. 이후 `proxy-1`에 bootstrap 명령을 보내 single-node Raft cluster를 만들고, Admin API로 route `r-raft`와 upstream pool `pool-raft`를 생성한다. 이 초기 설정은 Raft desired state에 기록되고, `proxy-1`의 proxy port를 통해 `raft.localtest.me` 요청이 backend로 전달되는지 확인한다.

다음으로 `proxy-2`와 `proxy-3`을 시작하고 join 명령으로 cluster에 합류시켰다. join 이후 각 노드의 `/api/namespaces/default/config` 응답을 확인하여 `r-raft`와 `pool-raft`가 복제되었는지 확인하였다. 또한 각 proxy port로 같은 host header 요청을 보내 모든 노드에서 route가 backend로 연결되는지 확인하였다. 이 과정은 Raft log를 통해 설정이 follower 노드에도 적용되고, 각 노드가 자신의 runtime snapshot을 만들어 요청을 처리하는지 확인하기 위한 것이다.

leader-only write 정책도 확인하였다. leader인 `proxy-1`에는 새 route와 upstream pool을 추가할 수 있지만, follower 노드에 같은 설정 쓰기 요청을 보내면 `409 not_raft_leader` 응답이 발생해야 한다. smoke script는 follower 후보에 쓰기 요청을 보내고, 응답 code가 `not_raft_leader`인지와 `leader_address`가 포함되는지를 확인한다. 이 검사는 9주차에 구현한 Raft store의 `ensureLeaderWrite()`와 오류 변환이 실제 API 흐름에서 동작하는지 확인하는 절차다.

Raft failover는 leader였던 `proxy-1`을 중단하는 방식으로 확인하였다. `proxy-1`이 중단되면 남은 `proxy-2`, `proxy-3` 중 하나가 새 leader가 되어야 한다. smoke script는 두 노드에 설정 쓰기를 시도하면서 어느 노드가 leader가 되었는지 확인하고, 새 leader에 `r-failover`와 `pool-failover`를 생성한다. 이후 살아 있는 노드들이 새 설정을 갖고 있고, `raft-failover.localtest.me` 요청을 backend로 전달하는지 확인한다. 이 절차를 통해 leader 장애 후에도 quorum이 유지되면 설정 변경과 요청 처리가 계속될 수 있음을 확인하였다.

중단된 `proxy-1`의 재합류와 persistence도 확인하였다. `proxy-1`을 다시 시작하면 기존 Raft data volume에 남아 있는 Raft state와 node-local metadata를 사용해 cluster에 다시 합류한다. 이후 failover 중 생성된 `r-failover`, `pool-failover` 설정을 따라왔는지 확인한다. 또한 모든 proxy 노드를 stop/start한 뒤에도 Raft volume이 유지되면 설정 상태가 복원되는지 확인하였다. 이 테스트는 Raft log와 snapshot이 단순 실행 중 메모리 상태가 아니라 재시작 후 복구에도 사용되는지 확인하기 위한 것이다.

VIP 점유 테스트는 `scripts/raft-ha-vip-smoke.sh`를 기준으로 진행하였다. 이 시나리오는 `raft-ha-cluster`와 비슷하지만, observer 컨테이너와 VIP `172.30.10.100/24`를 추가로 사용한다. bootstrap 시 `node-1`에는 VIP interface `eth0`, VIP address, GARP 설정, acquire delay를 전달한다. 초기 route를 만든 뒤 `node-1`이 VIP를 보유하는지 확인하고, observer가 `http://172.30.10.100:8080/api/info`로 요청했을 때 backend 응답을 받는지 확인한다.

join 이후에는 `node-2`, `node-3`이 follower 상태에서 VIP를 보유하지 않는지 확인하였다. 각 노드의 status 응답에서는 `vip.owned` 값을 확인하고, 컨테이너 내부에서는 `ip -4 addr show dev eth0` 결과를 확인한다. 이 단계는 10주차에 구현한 VIP controller가 follower에서 VIP를 추가하지 않고, leader인 노드만 VIP를 보유하는지 확인하는 절차다.

VIP failover는 `proxy-1`을 중단한 뒤 확인하였다. leader가 중단되면 남은 노드 중 새 leader가 선출되고, 해당 노드가 VIP를 획득해야 한다. smoke script는 새 leader를 찾은 뒤 그 노드가 VIP `172.30.10.100/24`를 보유하는지 확인하고, 다른 survivor 노드는 VIP를 보유하지 않는지 확인한다. 이후 observer가 VIP 주소로 다시 요청을 보내 backend 응답을 받는지 확인한다. 마지막으로 이전 leader인 `proxy-1`을 재기동했을 때 follower로 재합류하면 VIP를 보유하지 않는지 확인하였다.

## 확인 및 결과

`raft-ha-cluster` 테스트에서는 bootstrap, join, 설정 복제, follower write 거절, leader failover, 재합류, persistence 흐름을 확인하였다. 특히 route와 upstream pool 설정이 leader에서 생성된 뒤 join node에도 복제되었고, 각 노드의 proxy port에서 동일한 host 기반 routing이 동작하였다. follower write가 `409 not_raft_leader`로 거절된 것도 확인하였다.

`raft-ha-vip` 테스트에서는 Raft leader와 VIP owner가 같은 노드로 유지되는지 확인하였다. bootstrap leader인 `node-1`이 VIP를 보유하고, `node-2`, `node-3`은 follower 상태에서 VIP를 보유하지 않았다. leader 중단 후 새 leader가 VIP를 획득했고, 살아 있는 다른 follower는 VIP를 동시에 보유하지 않았다. 이전 leader가 재기동 후 follower로 재합류했을 때도 VIP를 보유하지 않았다.

이번 테스트에서 확인한 범위는 로컬 Docker bridge network 내부의 VIP ownership과 routing이다. observer 컨테이너가 같은 bridge network 안에서 VIP로 요청을 보내는 것은 확인했지만, 외부 LAN 장비의 ARP cache 갱신이나 switch MAC learning까지 검증한 것은 아니다. GARP가 실제 운영 네트워크에서 얼마나 빠르게 반영되는지는 Linux VM이나 실제 L2 segment에서 추가로 확인해야 한다.

## 다음 주차 계획

12주차에는 프로젝트의 Raft/VIP 구조를 외부 대조군과 비교할 준비를 진행한다. 상태 전파는 파일 동기화 또는 공유 파일 시스템 대조군과 비교하고, VIP failover는 Keepalived 환경과 비교한다. 비교 기준은 단순 RPS보다 failover 시간, 요청 실패 구간, leader 전환 안정성, 복구 후 정상 처리 여부에 맞춘다.

또한 이번 주차에서 확인한 로컬 bridge 테스트의 한계를 바탕으로, 더 실제 운영 환경에 가까운 네트워크 조건에서 VIP 이동과 ARP/GARP 전파를 확인할 방법을 정리한다.

## 관련 문서

- [Raft HA 클러스터 테스트 가이드](../architecture/raft-ha-test-guide.ko.md)
- [Raft HA VIP 테스트 가이드](../architecture/raft-ha-vip-test-guide.ko.md)
- [Raft VIP Failover 테스트 결과](https://github.com/lb-ajou/loadbalancer/blob/main/docs/architecture/raft-ha-vip-test-results-2026-05-19.ko.md)
- [Raft HA cluster smoke script](../../scripts/raft-ha-cluster-smoke.sh)
- [Raft HA VIP smoke script](../../scripts/raft-ha-vip-smoke.sh)
