# sticky-cookie-check

`sticky_cookie` 알고리즘이 실제로 동작하는지 확인하는 시나리오다. 정상 서버 2개와 health check 실패 서버 1개를 띄우고, 첫 요청은 round-robin으로 고르고 이후 요청은 cookie 기준으로 같은 upstream을 재사용하는지 확인한다.

## 실행

```bash
docker compose -f composes/sticky-cookie-check/compose.yaml up -d
```

## 직접 접근 확인

```bash
curl http://localhost:18481/api/info
curl http://localhost:18482/api/info
curl http://localhost:18483/api/info
curl -i http://localhost:18483/health
```

기대 결과:

- `18481`, `18482`는 정상 응답
- `18483`는 `/health`에서 `503`

## 로컬 프록시 설정 예시

아래 payload를 `default` namespace에 Admin API로 기록한다.

```json
{
  "name": "sticky-cookie-check",
  "routes": [
    {
      "id": "r-sticky",
      "enabled": true,
      "algorithm": "sticky_cookie",
      "match": {
        "hosts": ["sticky.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-sticky"
    }
  ],
  "upstream_pools": {
    "pool-sticky": {
      "upstreams": [
        "127.0.0.1:18481",
        "127.0.0.1:18482",
        "127.0.0.1:18483"
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
tools/sticky-cookie-check.sh
```

기본값:

- endpoint: `http://localhost:8080/api/info`
- host header: `sticky.localtest.me`

## 수동 확인 순서

1. 첫 요청으로 cookie를 받는다.
2. 같은 cookie jar로 다시 요청한다.
3. 응답 `server` 값이 같아야 한다.
4. 다른 cookie jar로 요청하면 다른 초기 배정이 가능하다.
5. unhealthy 서버(`sticky-c`)는 선택되면 안 된다.

## 종료

```bash
docker compose -f composes/sticky-cookie-check/compose.yaml down
```
