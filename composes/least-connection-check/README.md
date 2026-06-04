# least-connection-check

`least_connection` 알고리즘이 실제로 동작하는지 확인하는 시나리오다. 느린 정상 서버 1개, 빠른 정상 서버 1개, health check 실패 서버 1개를 띄우고, 이미 처리 중인 느린 요청이 있을 때 다음 요청이 다른 healthy upstream으로 우회되는지 확인한다.

## 실행

```bash
docker compose -f composes/least-connection-check/compose.yaml up -d
```

## 직접 접근 확인

```bash
curl http://localhost:18681/api/info
curl http://localhost:18682/api/info
curl http://localhost:18683/api/info
curl -i http://localhost:18683/health
```

기대 결과:

- `18681`는 약 2.5초 지연 후 정상 응답
- `18682`는 즉시 정상 응답
- `18683`는 `/health`에서 `503`

## 로컬 프록시 설정 예시

아래 payload를 `default` namespace에 Admin API로 기록한다.

```json
{
  "name": "least-connection-check",
  "routes": [
    {
      "id": "r-least-connection",
      "enabled": true,
      "algorithm": "least_connection",
      "match": {
        "hosts": ["least.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-lc"
    }
  ],
  "upstream_pools": {
    "pool-lc": {
      "upstreams": [
        "127.0.0.1:18681",
        "127.0.0.1:18682",
        "127.0.0.1:18683"
      ],
      "health_check": {
        "path": "/health",
        "interval": "5s",
        "timeout": "2s",
        "expect_status": 200
      }
    }
  }
}
```

## 검증 스크립트

```bash
tools/least-connection-check.sh
```

기본값:

- endpoint: `http://localhost:8080/api/info`
- host header: `least.localtest.me`
- health settle wait: `6`초

## 수동 확인 순서

1. health check가 settle되도록 몇 초 기다린다.
2. 첫 요청을 보낸다.
3. 첫 요청은 느린 서버 `lc-slow`로 가고 잠시 점유 상태를 유지한다.
4. 직후 두 번째 요청은 `lc-fast`로 가야 한다.
5. unhealthy 서버 `lc-unhealthy`는 선택되면 안 된다.

## 종료

```bash
docker compose -f composes/least-connection-check/compose.yaml down
```
