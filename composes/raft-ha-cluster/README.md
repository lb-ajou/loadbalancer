# raft-ha-cluster

3개 로드밸런서 컨테이너가 HashiCorp Raft로 설정 상태를 공유하고, 3개 backend server로 요청을 분산 전달하는 HA 검증 시나리오다.

## 구성

- `backend-a`, `backend-b`, `backend-c`: `/health`, `/api/info`를 제공하는 테스트 서버
- `proxy-1`: bootstrap node
  - proxy: `http://localhost:18080`
  - dashboard: `http://localhost:19090`
  - raft host port: `17001`
- `proxy-2`: `proxy-1` dashboard join endpoint로 합류하는 node
  - proxy: `http://localhost:18081`
  - dashboard: `http://localhost:19091`
  - raft host port: `17002`
- `proxy-3`: `proxy-1` dashboard join endpoint로 합류하는 node
  - proxy: `http://localhost:18082`
  - dashboard: `http://localhost:19092`
  - raft host port: `17003`

각 로드밸런서 노드는 별도 named volume에 `/app/data/raft`를 보관한다. app config 파일에는 Raft node ID, bind address, advertise address를 넣지 않는다. clean node의 identity는 smoke script가 `loadbalancer cluster ...` CLI로 전달하고, 성공 후에는 Raft data dir의 local metadata에 저장되어 재시작 복원에 사용된다. `proxy-1`은 빈 desired state로 bootstrap하고, 초기 route/upstream은 smoke script가 dashboard/Admin API 쓰기로 생성한다. join node들은 Raft log를 통해 같은 설정 state를 따라온다.

## 실행

이 시나리오는 compose가 backend와 로드밸런서를 함께 띄운다. 현재 compose build는 `composes/raft-ha-cluster/.out`에 있는 Linux binary를 `busybox:1.31.1` 기반 local image로 복사한다. 수동으로 compose만 실행하려면 먼저 binary를 준비해야 한다.

```bash
mkdir -p composes/raft-ha-cluster/.out
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o composes/raft-ha-cluster/.out/loadbalancer ./main.go
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o composes/raft-ha-cluster/.out/test-server ./composes/test-server
docker compose -f composes/raft-ha-cluster/compose.yaml up -d --build
```

proxy service healthcheck는 clean node에서도 응답하는 `/api/node/cluster-status`를 사용한다. config API는 bootstrap/join 이후 Raft store가 열린 뒤 검증용으로 조회한다.

clean bootstrap부터 다시 시작하려면 volume까지 삭제한다.

```bash
docker compose -f composes/raft-ha-cluster/compose.yaml down -v
```

## Smoke test

```bash
scripts/raft-ha-cluster-smoke.sh
```

필요한 command:

- `curl`
- `docker`
- `go`
- `jq`

smoke script는 실행할 때마다 먼저 local Linux binary를 `composes/raft-ha-cluster/.out`에 빌드한 뒤, 이 binary를 포함한 busybox 기반 local image를 compose로 빌드한다. cluster bootstrap/join은 이 binary의 `loadbalancer cluster ...` CLI로 실행하고, route/upstream 설정 쓰기는 dashboard/Admin API로 수행한다. 기본 동작은 종료 시 `docker compose down -v --remove-orphans`로 컨테이너와 volume을 정리하는 것이다.

실패 상태나 Raft data를 직접 확인해야 하면 정리를 건너뛸 수 있다.

```bash
KEEP_RAFT_HA_SMOKE=1 scripts/raft-ha-cluster-smoke.sh
```

검증 범위:

- bootstrap node가 빈 desired state로 시작한 뒤 Admin API 쓰기를 Raft state로 기록하는지
- join node가 leader를 통해 cluster에 합류하고 Raft 상태를 따라오는지
- leader write가 모든 node에 복제되는지
- follower write가 `409 not_raft_leader`로 거절되는지
- join API가 잘못된 `node_id`, `raft_address`를 거절하는지
- leader 중단 후 남은 node에서 write가 가능한지
- 중단됐던 node가 재기동 후 최신 state로 catch-up 하는지
- Raft volume 유지 재시작 후 설정 state가 유지되는지

## 수동 확인 예시

```bash
curl http://localhost:19090/api/node/cluster-status
curl http://localhost:19091/api/node/cluster-status
curl http://localhost:19092/api/node/cluster-status

curl http://localhost:19090/api/config
curl http://localhost:19091/api/config
curl http://localhost:19092/api/config

curl -H 'Host: raft.localtest.me' http://localhost:18080/api/info
curl -H 'Host: raft.localtest.me' http://localhost:18081/api/info
curl -H 'Host: raft.localtest.me' http://localhost:18082/api/info
```

## 운영 전제

`/api/cluster/join`은 admin/control-plane endpoint다. 이 POC에는 내장 인증이 없으므로 이 compose는 로컬 검증용으로만 사용한다.

`not_raft_leader` 응답의 `leader_address`는 Raft advertise address다. dashboard HTTP URL이 아닐 수 있으므로 client가 직접 retry URL로 사용하면 안 된다.
