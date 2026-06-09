# lb-multi-upstream

하나의 upstream pool에 여러 백엔드 서버를 묶는 시나리오다. 프록시 응답의 `/api/info`를 통해 어떤 서버가 선택되었는지 확인한다.

## 실행

```bash
docker compose -f composes/lb-multi-upstream/compose.yaml up -d
```

## 직접 접근 확인

```bash
curl http://localhost:18181/api/info
curl http://localhost:18182/api/info
curl http://localhost:18183/api/info
```

기대 결과:

- 각 포트는 서로 다른 `server` 값(`lb-a`, `lb-b`, `lb-c`)을 반환
- 모든 `/health`는 `200 OK`

## 로컬 프록시 설정 예시

아래 payload를 Admin API의 단일 proxy config로 기록한다.

```json
{
  "routes": [
    {
      "id": "r-lb",
      "enabled": true,
      "match": {
        "hosts": ["lb.localtest.me"],
        "path": { "type": "prefix", "value": "/" }
      },
      "upstream_pool": "pool-lb"
    }
  ],
  "upstream_pools": {
    "pool-lb": {
      "upstreams": [
        "127.0.0.1:18181",
        "127.0.0.1:18182",
        "127.0.0.1:18183"
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

## 프록시 확인 예시

```bash
for i in 1 2 3 4 5 6; do
  curl -s -H 'Host: lb.localtest.me' http://localhost:8080/api/info
  echo
done
```

기대 결과:

- 응답 JSON의 `server` 값이 `lb-a`, `lb-b`, `lb-c` 중 하나로 나타난다.
- 여러 번 호출했을 때 서로 다른 서버가 응답하면 multi-upstream 구성이 정상이다.

## 대시보드 확인

```bash
curl http://localhost:9090/api/runtime
```

`pool-lb` 안에 세 target이 보이고 모두 healthy면 정상이다.
