# Compose Test Environments

`composes/`는 로드밸런서를 로컬에서 실행할 때 붙여볼 수 있는 백엔드 테스트 환경 모음이다.

대부분의 compose는 로드밸런서 앱 자체를 띄우지 않는다. 로드밸런서는 로컬에서 실행하고, compose는 외부 접근 가능한 백엔드 서버들과 health check 대상만 제공한다. `raft-ha-cluster`는 예외로, backend와 3-node 로드밸런서 cluster를 함께 띄운다.

## 공통 규칙

- 모든 서버는 호스트 포트로 직접 접근 가능하다.
- 모든 서버는 `/health`와 `/api/info`를 제공한다.
- Docker Compose 자체 healthcheck도 `/health` 기준으로 설정한다.
- 시나리오별 프록시 설정 예시는 각 하위 디렉토리 `README.md`에 적어 둔다.

## 시나리오 목록

- `route-basic`
  - 서로 다른 두 서버를 직접 라우팅 대상으로 쓰는 기본 시나리오
- `lb-multi-upstream`
  - 같은 역할의 여러 서버를 하나의 upstream pool로 묶는 시나리오
- `failure-healthcheck`
  - 일부 서버가 `/health`에서 실패하는 상태를 재현하는 시나리오
- `round-robin-check`
  - round-robin 분산을 자동 스크립트로 검증하는 시나리오
- `sticky-cookie-check`
  - sticky cookie 기반 라우팅을 실제 cookie jar로 검증하는 시나리오
- `5-tuple-hash-check`
  - `X-Forwarded-For` 기반 5-tuple hash 라우팅을 검증하는 시나리오
- `least-connection-check`
  - 느린 in-flight 요청이 있을 때 다른 healthy backend로 우회되는지 검증하는 시나리오
- `raft-ha-cluster`
  - 3-node Raft 로드밸런서 cluster와 3-backend 환경에서 bootstrap, join, replication, failover, persistence를 검증하는 시나리오

## 실행 예시

```bash
docker compose -f composes/route-basic/compose.yaml up -d
docker compose -f composes/route-basic/compose.yaml down
```

## 공용 테스트 서버 API

### `GET /health`

- 정상 서버: `200 OK`
- 실패 시나리오 서버: `HEALTH_STATUS` 환경변수에 지정한 상태 반환

### `GET /api/info`

아래 형태의 JSON을 반환한다.

```json
{
  "server": "route-alpha",
  "scenario": "route-basic",
  "hostname": "container-hostname",
  "port": "8080",
  "version": "v1",
  "health_status": 200
}
```

## 로컬 프록시와 함께 테스트하는 기본 순서

1. 원하는 compose 시나리오를 띄운다.
2. 로컬 `configs/app.json`으로 로드밸런서 앱을 실행한다.
3. 해당 시나리오 `README`의 route/upstream 예시를 Admin API나 대시보드 저장 API로 기록한다.
4. 프록시 주소와 백엔드 주소를 각각 호출해 동작을 비교한다.
5. 필요하면 대시보드 `GET /api/runtime`로 현재 라우트와 upstream 상태를 확인한다.
