# raft-ha-vip

`raft-ha-vip`는 기존 `composes/raft-ha-cluster`를 기반으로 만든 VIP failover 검증용 시나리오다. Raft leader가 VIP를 소유하고 failover 시 새 leader로 VIP가 이동하는 흐름을 검증한다. 초기 route/upstream은 프록시 JSON 파일이 아니라 smoke script의 dashboard/Admin API 쓰기로 생성한다.

## 로컬 Bridge Smoke

로컬 검증은 Docker bridge network `172.30.10.0/24` 안에서 실행한다. proxy 컨테이너는 `NET_ADMIN`, `NET_RAW` capability를 받아 `eth0`에 VIP `172.30.10.100/24`를 붙이고, observer 컨테이너가 VIP로 HTTP 요청을 보낸다.

```bash
bash scripts/raft-ha-vip-smoke.sh
```

smoke script는 실행 시 Linux binary를 `composes/raft-ha-vip/.out`에 빌드하고 compose project를 올린 뒤, 성공하면 기본적으로 컨테이너와 volume을 정리한다. cluster bootstrap/join은 이 binary의 `reverseproxy cluster ...` CLI로 실행한다.

proxy service healthcheck는 clean node에서도 응답하는 `/api/node/cluster-status`를 사용한다. Raft node identity, VIP address, 초기 route/upstream은 app config 파일이 아니라 smoke script의 lifecycle/Admin API 호출로 설정한다. Raft node identity는 성공 후 Raft data dir의 local metadata에 저장되어 재시작 복원에 사용된다.

실패한 환경을 보존하려면 다음처럼 실행한다.

```bash
KEEP_RAFT_HA_VIP_SMOKE=1 bash scripts/raft-ha-vip-smoke.sh
```

compose project name을 고정하면 보존된 컨테이너를 반복 진단하기 쉽다.

```bash
RAFT_HA_VIP_PROJECT_NAME=reverseproxy-raft-ha-vip-dev \
  KEEP_RAFT_HA_VIP_SMOKE=1 \
  bash scripts/raft-ha-vip-smoke.sh
```

수동 정리는 같은 project name으로 수행한다.

```bash
docker compose -p reverseproxy-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  down -v --remove-orphans
```

## Proxmox Macvlan

`compose.macvlan.yaml`은 Linux/Proxmox 전용 override다. macvlan은 compose network 전체를 LAN L2 segment에 직접 붙이므로 proxy와 backend 컨테이너 IP가 같은 subnet의 endpoint로 노출된다. 이 경로에서는 host mode가 필요하지 않다.

기본 예시는 Proxmox VM의 NIC가 `ens18`이고 LAN이 `192.168.50.0/24`인 환경이다.

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

macvlan 실행 전 bootstrap API에 전달하는 VIP CIDR을 선택한 LAN subnet에 맞춰야 한다. 기본 설계값은 Proxmox 예시에서 `192.168.50.210/24`다. app config 파일에는 VIP address를 넣지 않는다.

macvlan에서는 container IP가 직접 노출되므로 override에서 proxy service의 host port publish를 비운다. 실행 전 VIP, proxy IP, backend IP가 모두 DHCP pool과 기존 장비 주소에서 벗어났는지 확인해야 한다. Docker Desktop/macOS는 Linux macvlan을 지원하지 않고 외부 L2 ARP cache 변경도 검증할 수 없으므로, Proxmox 검증은 Linux Docker Engine이 설치된 VM에서 수행한다.

## 정리

```bash
docker compose -p reverseproxy-raft-ha-vip-macvlan \
  -f composes/raft-ha-vip/compose.yaml \
  -f composes/raft-ha-vip/compose.macvlan.yaml \
  down -v --remove-orphans
```
