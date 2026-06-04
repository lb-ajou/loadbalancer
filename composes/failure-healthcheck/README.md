# failure-healthcheck

일부 upstream이 `/health`에서 실패하는 상태를 재현하는 시나리오다. 정상 서버 2개와 unhealthy 서버 1개를 함께 띄운다.

## 실행

```bash
docker compose -f composes/failure-healthcheck/compose.yaml up -d
```

## 직접 접근 확인

```bash
curl -i http://localhost:18281/health
curl -i http://localhost:18282/health
curl -i http://localhost:18283/health
```

기대 결과:

- `18281`, `18282`는 `200 OK`
- `18283`는 `503 Service Unavailable`

각 서버의 식별 정보:

```bash
curl http://localhost:18281/api/info
curl http://localhost:18282/api/info
curl http://localhost:18283/api/info
```

## 로컬 프록시 설정 예시

아래 payload를 `default` namespace에 Admin API로 기록한다.

```json
{
  "name": "failure-healthcheck",
  "routes": [
    {
      "id": "r-failure",
      "enabled": true,
      "match": {
        "hosts": ["health.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-health"
    }
  ],
  "upstream_pools": {
    "pool-health": {
      "upstreams": [
        "127.0.0.1:18281",
        "127.0.0.1:18282",
        "127.0.0.1:18283"
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

## 프록시 확인 예시

로컬 프록시를 실행한 뒤:

```bash
for i in 1 2 3 4 5 6; do
  curl -s -H 'Host: health.localtest.me' http://localhost:8080/api/info
  echo
done
```

기대 결과:

- 응답 `server` 값은 `healthy-a` 또는 `healthy-b`
- `unhealthy-c`는 health check 실패 이후 선택되지 않아야 한다

## 대시보드 확인

```bash
curl http://localhost:9090/api/runtime
```

기대 결과:

- `pool-health`의 target 세 개가 보인다
- `127.0.0.1:18283`는 unhealthy 상태로 표시된다

## 종료

```bash
docker compose -f composes/failure-healthcheck/compose.yaml down
```
