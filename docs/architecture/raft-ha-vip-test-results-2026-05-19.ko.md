# Raft VIP Failover 테스트 결과 - 2026-05-19

## 요약

2026년 5월 19일 로컬 Docker 환경에서 `raft-ha-vip` bridge smoke를 실제 실행했다. 테스트는 Raft leader가 VIP `172.30.10.100/24`를 획득하고, leader 중단 후 새 leader가 VIP를 이어받으며, 이전 leader가 follower로 재합류할 때 VIP를 보유하지 않는 흐름을 확인했다.

Proxmox Linux VM 기반 macvlan deep test는 이번 세션에서 VM 접속 정보와 실제 LAN IP 예약 정보가 없어 실행하지 않았다. 대신 macvlan compose override 렌더링과 실행 전 체크리스트를 검증했다.

## 테스트 환경

| 항목 | 값 |
| --- | --- |
| 실행일 | 2026-05-19 |
| 로컬 OS | Darwin 24.6.0 arm64 |
| Go | `go1.26.3 darwin/arm64` |
| Docker | `Docker version 29.4.0, build 9d7ad9f` |
| Docker Compose | `Docker Compose version v5.1.2` |
| 테스트 compose | `composes/raft-ha-vip/compose.yaml` |
| smoke script | `scripts/raft-ha-vip-smoke.sh` |
| smoke project | `reverseproxy-raft-ha-vip-33217` |

## 실행한 검증

| 검증 | 명령 | 결과 |
| --- | --- | --- |
| smoke script 문법 | `bash -n scripts/raft-ha-vip-smoke.sh` | 통과 |
| bridge compose 렌더링 | `docker compose -f composes/raft-ha-vip/compose.yaml config` | 통과 |
| macvlan compose 렌더링 | `docker compose -f composes/raft-ha-vip/compose.yaml -f composes/raft-ha-vip/compose.macvlan.yaml config` | 통과 |
| Go 테스트 | `go test ./...` | 통과 |
| 로컬 VIP smoke | `bash scripts/raft-ha-vip-smoke.sh` | 통과 |
| 작업트리 확인 | `git status --short --untracked-files=all` | 기존 `docs/harness/*` 삭제 3건만 표시 |

`scripts/agent-check.sh fast`는 이 macOS 환경의 기본 Bash가 3.2라서 `mapfile`을 찾지 못해 실행 중단된다. 따라서 위 명령을 개별로 실행해 하네스가 확인하려는 주요 검증 신호를 확보했다.

## 로컬 VIP Smoke 관찰 결과

smoke는 다음 순서로 진행됐다.

1. 기존 동일 project compose 환경을 `down -v --remove-orphans`로 초기화했다.
2. Linux용 `reverseproxy`, `test-server` binary를 `composes/raft-ha-vip/.out`에 빌드했다.
3. `backend-a`, `backend-b`, `backend-c`, `observer`, `proxy-1`을 시작했다.
4. `proxy-1` dashboard가 응답한 뒤 seed route `r-raft`와 pool `pool-raft`를 확인했다.
5. `node-1`이 VIP `172.30.10.100/24`를 보유하는 것을 확인했다.
6. `proxy-2`, `proxy-3`을 join시킨 뒤 두 node가 seed 상태를 따라오는지 확인했다.
7. `node-2`, `node-3`이 VIP를 보유하지 않는 것을 확인했다.
8. follower write가 `not_raft_leader`로 거절되는 것을 확인했다.
9. `proxy-1`을 중단했다.
10. failover 후 새 leader는 `node-3`으로 관찰됐다.
11. `node-3`이 VIP `172.30.10.100/24`를 보유하고, `node-2`는 VIP를 보유하지 않는 것을 확인했다.
12. `proxy-1`을 재기동했다.
13. `node-1`은 follower로 재합류했고 VIP를 보유하지 않는 것을 확인했다.
14. smoke는 `success` 로그를 출력하고 정상 종료했다.

핵심 로그:

```text
[raft-ha-vip-smoke] node-1 owns VIP 172.30.10.100/24
[raft-ha-vip-smoke] node-2 does not own VIP 172.30.10.100/24
[raft-ha-vip-smoke] node-3 does not own VIP 172.30.10.100/24
[raft-ha-vip-smoke] verified follower write rejection on node-2
[raft-ha-vip-smoke] new leader after failover: node-3
[raft-ha-vip-smoke] node-3 owns VIP 172.30.10.100/24
[raft-ha-vip-smoke] node-2 does not own VIP 172.30.10.100/24
[raft-ha-vip-smoke] node-1 rejoined as follower
[raft-ha-vip-smoke] node-1 does not own VIP 172.30.10.100/24
[raft-ha-vip-smoke] success
```

## 검증된 사항

- bootstrap leader가 VIP를 획득한다.
- join node는 follower 상태에서 VIP를 보유하지 않는다.
- observer 컨테이너가 같은 bridge network 안에서 VIP로 proxy route에 접근할 수 있다.
- follower에 대한 write 요청은 leader-only 규칙에 따라 거절된다.
- leader 중단 후 남은 node 중 새 leader가 선출된다.
- 새 leader가 VIP를 획득한다.
- 살아 있는 follower는 VIP를 동시에 보유하지 않는다.
- 이전 leader가 follower로 재합류하면 VIP를 보유하지 않는다.
- smoke 종료 후 compose 환경은 기본 cleanup 경로로 정리된다.

## 이번 테스트의 한계

로컬 bridge smoke는 Docker network 내부의 VIP ownership과 routing을 검증한다. macOS/Docker Desktop 계열 환경에서는 외부 LAN 장비의 ARP cache 갱신이나 실제 switch MAC learning까지 검증하지 않는다.

이번 테스트에서 직접 확인하지 않은 항목은 다음과 같다.

- Proxmox Linux VM에서 Docker macvlan으로 proxy/backend 컨테이너를 실제 LAN에 노출하는 흐름
- 외부 client VM에서 VIP `192.168.50.210`으로 접근하는 흐름
- failover 후 client VM의 `ip neigh show 192.168.50.210` 결과가 새 leader MAC으로 바뀌는지 여부
- 실제 운영 LAN에서 DHCP pool, 방화벽, switch MAC learning 정책과 충돌하지 않는지 여부

## Proxmox Deep Test 준비 상태

macvlan compose override는 렌더링 검증을 통과했다. Proxmox에서 실제 deep test를 진행하려면 실행 전에 다음 값을 환경에 맞게 확정해야 한다.

| 항목 | 예시값 |
| --- | --- |
| macvlan parent | `ens18` |
| LAN subnet | `192.168.50.0/24` |
| gateway | `192.168.50.1` |
| VIP | `192.168.50.210/24` |
| proxy IPs | `192.168.50.211`, `192.168.50.212`, `192.168.50.213` |
| backend IPs | `192.168.50.221`, `192.168.50.222`, `192.168.50.223` |
| client | 같은 Proxmox bridge/L2 segment의 별도 Linux VM |

`configs/node-*/app.json`의 `vip.address`는 로컬 smoke 기본값인 `172.30.10.100/24`다. macvlan deep test 전에는 선택한 LAN VIP, 예: `192.168.50.210/24`, 로 바꾼 테스트용 설정을 사용해야 한다.

## Proxmox Deep Test 후속 절차

Proxmox Linux VM에서 다음 순서로 추가 검증한다.

1. Linux VM에 Docker Engine, Docker Compose, Go, curl, jq, iproute2를 준비한다.
2. VIP, proxy IP, backend IP가 DHCP pool과 기존 장비 주소와 겹치지 않는지 확인한다.
3. `composes/raft-ha-vip/configs/node-*/app.json`의 `vip.address`를 LAN VIP로 맞춘다.
4. compose image가 복사할 Linux binary를 준비한다.

```bash
mkdir -p composes/raft-ha-vip/.out
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o composes/raft-ha-vip/.out/reverseproxy ./main.go
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o composes/raft-ha-vip/.out/test-server ./composes/test-server
```

5. macvlan override로 compose를 실행한다.

```bash
RAFT_HA_VIP_NODE1_IP=192.168.50.211 \
RAFT_HA_VIP_NODE2_IP=192.168.50.212 \
RAFT_HA_VIP_NODE3_IP=192.168.50.213 \
RAFT_HA_VIP_BACKEND_A_IP=192.168.50.221 \
RAFT_HA_VIP_BACKEND_B_IP=192.168.50.222 \
RAFT_HA_VIP_BACKEND_C_IP=192.168.50.223 \
RAFT_HA_VIP_MACVLAN_PARENT=ens18 \
RAFT_HA_VIP_MACVLAN_SUBNET=192.168.50.0/24 \
RAFT_HA_VIP_MACVLAN_GATEWAY=192.168.50.1 \
docker compose -p reverseproxy-raft-ha-vip-macvlan \
  -f composes/raft-ha-vip/compose.yaml \
  -f composes/raft-ha-vip/compose.macvlan.yaml \
  up -d --build
```

6. client VM에서 VIP 요청과 ARP table을 확인한다.

```bash
curl -H 'Host: raft.localtest.me' http://192.168.50.210:8080/api/info
ip neigh show 192.168.50.210
```

7. leader container를 중단하고 새 leader 선출 후 VIP 요청이 복구되는지 확인한다.
8. client VM의 ARP table에서 VIP MAC이 새 leader 쪽으로 바뀌었는지 확인한다.
9. 테스트 후 compose 환경을 정리한다.

```bash
docker compose -p reverseproxy-raft-ha-vip-macvlan \
  -f composes/raft-ha-vip/compose.yaml \
  -f composes/raft-ha-vip/compose.macvlan.yaml \
  down -v --remove-orphans
```

## 결론

현재 로컬에서 가능한 범위의 Raft VIP Failover 검증은 통과했다. 이 결과는 Raft leadership transition과 VIP controller wiring, Linux netlink 기반 VIP add/remove, bridge network 내부 VIP routing이 함께 동작함을 보여준다.

운영 환경에 가까운 외부 L2 ARP/GARP 효과는 아직 미검증이다. 이 부분은 Proxmox Linux VM과 macvlan override를 사용해 별도 deep test로 검증해야 한다.
