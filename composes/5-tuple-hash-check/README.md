# 5-tuple-hash-check

`5_tuple_hash` 알고리즘이 실제로 동작하는지 확인하는 시나리오다. 정상 서버 2개와 health check 실패 서버 1개를 띄우고, 같은 client 식별 헤더로 반복 요청하면 같은 upstream을 재사용하는지 확인한다. `X-Forwarded-For`, `Forwarded` 경로와 샘플 분포 요약을 함께 볼 수 있다.

## 실행

```bash
docker compose -f composes/5-tuple-hash-check/compose.yaml up -d
```

## 직접 접근 확인

```bash
curl http://localhost:18581/api/info
curl http://localhost:18582/api/info
curl http://localhost:18583/api/info
curl -i http://localhost:18583/health
```

기대 결과:

- `18581`, `18582`는 정상 응답
- `18583`는 `/health`에서 `503`

## 로컬 프록시 설정 예시

아래 payload를 `default` namespace에 Admin API로 기록한다.

```json
{
  "name": "5-tuple-hash-check",
  "routes": [
    {
      "id": "r-5-tuple-hash",
      "enabled": true,
      "algorithm": "5_tuple_hash",
      "match": {
        "hosts": ["tuple.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-tuple"
    }
  ],
  "upstream_pools": {
    "pool-tuple": {
      "upstreams": [
        "127.0.0.1:18581",
        "127.0.0.1:18582",
        "127.0.0.1:18583"
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
tools/5-tuple-hash-check.sh
```

기본값:

- endpoint: `http://localhost:8080/api/info`
- host header: `tuple.localtest.me`

## 수동 확인 순서

1. `X-Forwarded-For: 203.0.113.10`으로 요청한다.
2. 같은 헤더로 다시 요청한다.
3. 응답 `server` 값이 같아야 한다.
4. 같은 값의 `Forwarded: for=...` 요청도 같은 backend로 유지돼야 한다.
5. 여러 client 샘플 분포 요약에서 unhealthy 서버(`tuple-c`)는 선택되면 안 된다.

## 종료

```bash
docker compose -f composes/5-tuple-hash-check/compose.yaml down
```
