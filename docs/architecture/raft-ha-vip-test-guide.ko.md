# Raft HA VIP 테스트 가이드

이 문서는 `composes/raft-ha-vip` 시나리오로 Raft leader VIP failover를 검증하는 절차를 설명한다. 이 시나리오는 `composes/raft-ha-cluster`를 복사해 만든 전용 환경이며 원본 compose는 수정하지 않는다.

## 로컬 Bridge 토폴로지

로컬 smoke는 Docker user-defined bridge network 하나에서 실행한다.

- network: `raft-ha-vip-net`
- subnet: `172.30.10.0/24`
- gateway: `172.30.10.1`
- proxy fixed IP: `172.30.10.11`, `172.30.10.12`, `172.30.10.13`
- observer IP: `172.30.10.50`
- VIP: `172.30.10.100/24`
- VIP interface: `eth0`

세 proxy는 HashiCorp Raft cluster를 구성한다. leader만 `eth0`에 VIP를 추가하고 follower는 VIP를 갖지 않는다. observer 컨테이너는 같은 bridge network에서 `http://172.30.10.100:8080/api/info`로 접근해 VIP가 실제 proxy route를 통과하는지 확인한다.

proxy 컨테이너에는 Linux capability가 필요하다.

- `NET_ADMIN`: interface address 추가와 제거
- `NET_RAW`: ARP/GARP 송신

## 로컬 실행과 성공 기준

```bash
bash scripts/raft-ha-vip-smoke.sh
```

smoke script는 `loadbalancer cluster bootstrap|join` CLI로 Raft cluster와 VIP lifecycle 입력을 수행한다.

로컬 smoke의 성공 기준은 다음과 같다.

- compose proxy healthcheck가 `/api/node/cluster-status`로 clean node control-plane readiness를 확인한다.
- bootstrap leader인 `node-1`이 VIP `172.30.10.100/24`를 소유한다.
- `node-2`, `node-3` follower는 VIP를 소유하지 않는다.
- observer가 VIP로 `raft.localtest.me` route에 접근해 backend 응답을 받는다.
- leader 중단 후 남은 node 중 새 leader가 선출된다.
- 새 leader가 VIP를 획득하고 다른 survivor는 VIP를 갖지 않는다.
- failover 후 observer의 VIP HTTP 요청이 다시 성공한다.
- 이전 leader 재기동 후 follower가 되면 VIP를 다시 소유하지 않는다.

실패 상태를 보존하려면 다음 변수를 사용한다.

```bash
RAFT_HA_VIP_PROJECT_NAME=loadbalancer-raft-ha-vip-dev \
KEEP_RAFT_HA_VIP_SMOKE=1 \
bash scripts/raft-ha-vip-smoke.sh
```

수동 정리는 다음 명령으로 수행한다.

```bash
docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  down -v --remove-orphans
```

## Docker Desktop 제한

Docker Desktop for macOS는 컨테이너 네트워크를 내부 Linux VM과 host-side network backend로 중계한다. 이 환경에서는 같은 Docker bridge 내부의 VIP 동작은 smoke로 확인할 수 있지만, 외부 LAN 장비의 ARP cache가 GARP에 의해 갱신되는지는 검증할 수 없다.

Docker macvlan driver는 Linux host 전용이다. Docker Desktop for macOS 또는 Windows에서는 macvlan deep test를 수행할 수 없다. Docker Desktop의 host networking 기능도 TCP/UDP 중심의 L4 편의 기능이므로 ARP/GARP 검증 경로로 사용하지 않는다.

## Proxmox VM 사전 조건

Proxmox deep test는 Linux Docker Engine이 설치된 VM에서 수행한다. 권장 VM 환경은 Debian 또는 Ubuntu Server다.

필수 도구:

- Docker Engine과 Docker Compose plugin
- `go`
- `curl`
- `jq`
- `iproute2`

Proxmox VM NIC 예시는 `ens18`이다. Proxmox bridge는 일반적으로 `vmbr0` 또는 테스트 전용 bridge를 사용한다. VLAN을 쓰는 경우 VM NIC와 상위 bridge, switch가 같은 VLAN을 통과시키는지 확인한다.

## Proxmox Bridge와 Macvlan 토폴로지

권장 순서는 먼저 Linux VM 안에서 로컬 bridge smoke를 실행해 baseline을 확인한 뒤, macvlan override로 외부 L2 segment 검증을 수행하는 것이다.

Proxmox macvlan 예시 값:

- LAN subnet: `192.168.50.0/24`
- gateway: `192.168.50.1`
- macvlan parent: `ens18`
- proxy fixed IP: `192.168.50.211`, `192.168.50.212`, `192.168.50.213`
- VIP: `192.168.50.210/24`
- client: 같은 Proxmox bridge 또는 같은 LAN에 있는 별도 Linux VM

macvlan 실행 전에는 bootstrap API에 전달하는 VIP CIDR을 `192.168.50.210/24`처럼 선택한 LAN subnet에 맞춰 조정한다. app config 파일에는 VIP address를 넣지 않는다.

실행 예시는 다음과 같다.

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
docker compose -p loadbalancer-raft-ha-vip-macvlan \
  -f composes/raft-ha-vip/compose.yaml \
  -f composes/raft-ha-vip/compose.macvlan.yaml \
  up -d --build
```

외부 client VM에서는 다음 흐름을 확인한다.

```bash
curl -H 'Host: raft.localtest.me' http://192.168.50.210:8080/api/info
ip neigh show 192.168.50.210
```

leader 컨테이너를 중단한 뒤 다시 요청하고, `ip neigh show` 결과의 MAC이 새 leader 쪽으로 바뀌는지 확인한다.

## IP 충돌 체크리스트

macvlan override는 compose network 전체를 macvlan으로 바꾸므로 proxy뿐 아니라 backend 컨테이너도 실제 LAN에 노출된다. 실행 전 다음을 확인한다.

- `192.168.50.210` VIP가 이미 다른 장비에서 사용 중이지 않다.
- `192.168.50.211`부터 `192.168.50.213`까지 proxy fixed IP가 사용 중이지 않다.
- `192.168.50.221`부터 `192.168.50.223`까지 backend fixed IP가 사용 중이지 않다.
- 선택한 주소가 DHCP pool과 겹치지 않는다.
- Proxmox bridge와 상위 switch가 하나의 VM NIC 뒤 여러 MAC address 학습을 허용한다.
- 무선 uplink가 아니라 유선 bridge 또는 VLAN-aware bridge를 사용한다.
- client VM이 proxy 컨테이너와 같은 L2 segment에 있다.
- 방화벽이 client에서 VIP `8080` 접근을 차단하지 않는다.

## Host Mode와 Macvlan 판단

host mode는 이 테스트의 필수 조건이 아니다. Linux host mode에서는 여러 proxy가 같은 host port를 동시에 바인딩하기 어렵고, Docker Desktop host mode는 ARP/GARP 검증에 적합하지 않다.

macvlan은 외부 client가 VIP를 같은 LAN endpoint처럼 접근해야 할 때 사용한다. 로컬 개발과 빠른 회귀 검증은 bridge smoke로 충분하며, 외부 L2 ARP cache 갱신 확인은 Proxmox Linux VM과 macvlan 조합에서 수행한다.

## 문제 해결 명령

compose 상태와 로그를 확인한다.

```bash
docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  ps

docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  logs proxy-1 proxy-2 proxy-3
```

각 proxy의 VIP 보유 여부를 확인한다.

```bash
docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  exec proxy-1 ip -4 addr show dev eth0

docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  exec proxy-2 ip -4 addr show dev eth0

docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  exec proxy-3 ip -4 addr show dev eth0
```

observer 또는 client의 neighbor table을 확인한다.

```bash
docker compose -p loadbalancer-raft-ha-vip-dev \
  -f composes/raft-ha-vip/compose.yaml \
  exec observer ip neigh

ip neigh show 192.168.50.210
```

macvlan network 정의를 확인한다.

```bash
docker network inspect loadbalancer-raft-ha-vip-macvlan_raft-ha-vip-net
```

정리는 다음 명령으로 수행한다.

```bash
docker compose -p loadbalancer-raft-ha-vip-macvlan \
  -f composes/raft-ha-vip/compose.yaml \
  -f composes/raft-ha-vip/compose.macvlan.yaml \
  down -v --remove-orphans
```
