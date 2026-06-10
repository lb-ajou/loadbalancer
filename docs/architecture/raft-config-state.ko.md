# Raft 설정 상태 운영 규칙

이 문서는 HA 모드에서 어떤 상태가 Raft로 복제되고, 어떤 상태가 노드 로컬에 남는지 운영자가 빠르게 확인하기 위한 기준이다.

## 핵심 규칙

- Raft는 cluster desired state를 관리한다. route, upstream pool 같은 프록시 설정의 목표 상태와 cluster-wide VIP address/GARP 정책이 합의 대상이다.
- `configs/app.json`은 노드 로컬 설정으로 남는다. listen 주소, dashboard 주소, Raft data dir처럼 프로세스별로 달라질 수 있는 값만 `fileConfig`로 읽고 Raft에는 넣지 않는다. 로더는 unknown field를 거부하므로 Raft identity/timing 같은 lifecycle 입력은 bootstrap/join API나 Raft data dir local metadata를 통해서만 관리한다.
- `boot.AppConfig`는 listen 주소와 `raftDataDir` 같은 process-local 정적 설정만 담는다. Raft identity/timing은 `internal/config`의 공유 실행 설정 DTO로 표현하고, runtime read path는 `Snapshot.RaftIdentity`, `Snapshot.RaftTiming`을 사용한다. VIP runtime 값도 `AppConfig`에 두지 않고 lifecycle local VIP 입력과 Raft desired state를 합성한 `Snapshot.VIP`로만 노출한다.
- cluster-wide VIP 값은 `state.NormalizeClusterVIP()`로 기본 GARP/acquire 정책을 채운 뒤 `state.ValidateClusterVIP()`으로 검증해 `state.ClusterVIPConfig`로 Raft desired state에 저장한다. `internal/config.VIPConfig`는 runtime에 적용할 합성 VIP 값을 표현하며, `boot` 패키지는 파일 입력용 VIP DTO나 VIP policy 기본값을 제공하지 않는다.
- Raft snapshot restore와 runtime projection 경계에서도 VIP policy를 `state.NormalizeClusterVIP()`로 정규화한다. 과거 snapshot에 address만 남아 있어도 runtime은 동일한 기본 GARP/acquire 정책을 사용한다.
- Raft node ID, bind address, advertise address는 bootstrap/join 입력과 Raft data dir local metadata로 관리한다.
- Raft heartbeat/election/leader lease/commit timeout은 bootstrap 요청의 `raft_timing`으로 입력받고 `state.ValidateClusterRaftTiming()`에서 검증한 뒤 Raft desired state에 cluster-wide 정책으로 저장한다. Join node는 Raft node를 시작하기 전에 peer 후보의 `GET /api/cluster`에서 timing을 조회해 같은 timing을 적용한다.
- `boot.Load()`와 `app.New()`는 `boot.Normalize()`로 local process 기본값을 채운 뒤 control-plane을 먼저 띄운다. 기존 Raft state가 없으면 cluster를 자동 bootstrap하지 않는다. 웹/CLI가 `POST /api/cluster/bootstrap` 또는 `POST /api/node/join-cluster`를 호출해야 Raft node가 생성된다. 기존 Raft state가 있으면 재시작 복원을 위해 Raft node를 자동으로 연다.
- clean node의 local process config에는 Raft node ID, bind address, advertise address 기본값을 채우지 않는다.
- bootstrap/join 요청의 `node_id`, `raft_bind_addr`, `raft_advertise_addr`는 Raft data dir 내부의 node-local metadata로 저장된다. 기존 Raft state가 있고 metadata가 있으면 재시작 시 app config보다 이 값을 우선 사용한다.
- 웹/CLI는 `GET /api/node/cluster-status`의 `state`로 `unconfigured`, `clustered`, `existing_state`, `check_error`를 구분한다. `unconfigured`일 때만 bootstrap/join 입력을 제공한다.
- `loadbalancer cluster ...` CLI는 dashboard API의 얇은 클라이언트다. 로컬 설정 파일이나 프록시 설정 JSON을 편집하지 않고, `status`, `bootstrap`, `join` 명령으로 각 노드의 control-plane에 HTTP 요청을 보낸다.
- 단일 노드도 single-node Raft cluster로 취급한다.
- VIP failover 설정은 cluster-wide 값과 node-local 값으로 나뉜다. `vip.address`, GARP 송신 횟수/간격, 획득 지연, 종료 시 release 정책은 Raft desired state에 들어갈 cluster 값이다. `vip.interface`는 노드가 속한 Linux 네트워크 환경에 종속되므로 Raft에 넣지 않고 bootstrap/join 시 각 노드가 제공하는 local 값으로 다룬다.
- `vip.enabled` 입력은 설정 모델에서 제거됐다. 현재 `/api/status`의 `vip.enabled` 응답은 `vip.address` 존재 여부에서 계산되는 상태 필드이며, 설정 입력에서는 VIP address가 있으면 VIP가 활성인 것으로 본다.
- 프록시 route/upstream JSON은 더 이상 앱 부팅 입력이 아니다. 새 Raft bootstrap node는 빈 desired state로 시작하고, 초기 route/upstream도 Admin API 쓰기를 통해 Raft log에 기록한다.
- Raft log에는 proxy config 전체 교체, cluster VIP, Raft timing 단위의 command만 기록한다. route/upstream pool 개별 변경 command나 JSON seed/import 전용 command는 없다.
- 기존 Raft data dir이 있으면 재시작한 노드는 남아 있는 Raft log/snapshot에서 desired config를 복원한다. 로컬 프록시 JSON 파일을 다시 읽어 클러스터 상태를 덮어쓰지 않는다.
- Join 모드는 로컬 프록시 JSON이나 app config의 join 주소를 읽지 않는다. 새 노드는 `/api/node/join-cluster`로 받은 peer 후보를 순서대로 시도해 leader의 `POST /api/cluster/join`에 자신의 `node_id`와 Raft advertise address를 등록한 뒤 leader가 가진 Raft 상태를 따라간다.
- `/api/cluster/join`은 admin/control-plane endpoint다. 이 POC에는 내장 인증이 없으므로 보호된 admin network에만 노출하거나 외부 인증, network policy 뒤에 둔다.
- Health 상태와 `least_connection` 카운터는 로컬 노드 상태다. Raft로 복제하지 않으며 `GET /api/runtime`도 응답한 노드의 로컬 관측값을 보여준다.
- `GET /api/status`는 현재 노드 요약, `GET /api/cluster`는 Raft leader/membership, `GET /api/runtime`은 적용된 route/upstream과 target별 local health를 보여준다.
- VIP 소유권은 Raft leader transition을 로컬 Linux interface 조작으로 반영한 결과다. Raft가 leader election과 quorum 판정을 담당하며, 애플리케이션은 leader 획득 시 VIP를 추가하고 GARP를 송신하며 leader 상실 또는 정상 종료 시 VIP를 제거한다.
- Follower에 설정 쓰기 요청이 도착하면 leader forward를 하지 않고 `409 Conflict`를 반환한다. JSON body에는 `code: "not_raft_leader"`와 가능한 경우 `leader_address`가 포함된다. 이 값은 HashiCorp Raft가 보고한 leader의 Raft advertise address이며, dashboard/admin HTTP URL과 다를 수 있으므로 별도 매핑이 없으면 직접 재시도 URL이 아니라 leader 힌트로만 사용한다.
