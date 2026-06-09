# round-robin-check

round-robin 분산이 실제로 돌아가는지 확인하기 위한 전용 시나리오다. 동일한 역할의 백엔드 3개를 띄우고, 로컬 프록시가 각 서버를 순환 선택하는지 검사한다.

## 실행

```bash
docker compose -f composes/round-robin-check/compose.yaml up -d
```

## 직접 접근 확인

```bash
curl http://localhost:18381/api/info
curl http://localhost:18382/api/info
curl http://localhost:18383/api/info
```

기대 결과:

- 각 포트는 `rr-a`, `rr-b`, `rr-c`를 반환한다.
- 모든 `/health`는 `200 OK`다.

## 로컬 프록시 설정 예시

아래 payload를 Admin API의 단일 proxy config로 기록한다.

```json
{
  "routes": [
    {
      "id": "r-round-robin",
      "enabled": true,
      "match": {
        "hosts": ["rr.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-rr"
    }
  ],
  "upstream_pools": {
    "pool-rr": {
      "upstreams": [
        "127.0.0.1:18381",
        "127.0.0.1:18382",
        "127.0.0.1:18383"
      ],
      "health_check": {
        "path": "/health",
        "interval": "10s",
        "timeout": "3s",
        "expect_status": 200
      }
    }
  }
}
```

## 자동 검증 스크립트

```bash
tools/round-robin-check.sh
```

기본값:

- 프록시 URL: `http://localhost:8080/api/info`
- Host 헤더: `rr.localtest.me`
- 요청 횟수: `9`

## 인자 예시

```bash
tools/round-robin-check.sh http://localhost:8080/api/info rr.localtest.me 12
```

## 기대 결과

- 각 서버(`rr-a`, `rr-b`, `rr-c`)가 최소 1회 이상 응답한다.
- 요청 횟수를 3의 배수로 주면 각 서버 응답 횟수가 거의 균등해야 한다.
- 현재 구현은 healthy target 배열에 대해 atomic index 기반 round-robin이므로, 모두 healthy라면 순서대로 순환하는 것이 정상이다.

## 수동 확인 예시

```bash
for i in 1 2 3 4 5 6; do
  curl -s -H 'Host: rr.localtest.me' http://localhost:8080/api/info
  echo
done
```

## 종료

```bash
docker compose -f composes/round-robin-check/compose.yaml down
```
