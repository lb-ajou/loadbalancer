# benchmark-check

자체 로드밸런서와 비교군 `Caddy`, `Nginx`, `HAProxy`를 함께 띄우고 `wrk`, `vegeta`, `k6`로 성능을 측정하는 전용 시나리오다.

## 목적

- `wrk`로 포화 지점과 최대 처리량 후보를 찾는다.
- `vegeta`로 고정 RPS에서 `p95/p99`, `error rate`를 비교한다.
- `k6`로 램프업과 유지 구간이 있는 운영형 시나리오를 검증한다.
- `docker stats`로 프록시 컨테이너의 `CPU/메모리`를 샘플링한다.
- 동일한 백엔드 3개에 대해 각 프록시를 round robin 기준으로 비교한다.

## 실행

```bash
docker compose -f composes/benchmark-check/compose.yaml up -d --build
```

`proxy-init` 서비스가 자체 로드밸런서의 Admin API에 단일 proxy desired state를 기록한다. 로드밸런서용 route/upstream 설정은 정적 JSON 파일로 마운트하지 않는다.

## 직접 확인

```bash
curl -s -H 'Host: benchmark.localtest.me' http://localhost:18080/api/info
curl -s http://localhost:19090/api/runtime
curl -s http://localhost:18881/api/info
```

기대 결과:

- 자체 로드밸런서는 `18080`
- Caddy는 `18081`
- Nginx는 `18082`
- HAProxy는 `18083`
- 대시보드는 `19090`
- 백엔드는 `18881`, `18882`, `18883`
- 각 프록시 응답의 `server` 값이 `benchmark-a`, `benchmark-b`, `benchmark-c` 사이에서 분산된다.

## 권장 실험 순서

1. `BENCHMARK_TARGET=proxy|caddy|nginx|haproxy`로 대상 프록시를 고른다.
2. `tools/benchmark-wrk.sh`로 커넥션 단계별 포화 지점을 찾는다.
3. `tools/benchmark-vegeta.sh`로 고정 RPS 구간별 `p95/p99`, `error rate`를 비교한다.
4. `tools/benchmark-k6.sh`로 램프업과 스파이크 구간을 검증한다.
5. 각 실험과 병행해 `tools/benchmark-stats.sh`를 실행해 `CPU/메모리`를 기록한다.

모든 스크립트의 기본 결과 경로는 `plan/benchmarks/<tool>-<timestamp>/`다.

## 5회 반복 측정

단일 측정으로 결론을 내리지 않으려면 `tools/benchmark-matrix.sh`를 기본 진입점으로 사용한다.

```bash
tools/benchmark-matrix.sh
```

기본 동작:

- 대상 프록시 `proxy`, `caddy`, `nginx`, `haproxy`
- 벤치마크 도구 `wrk`, `vegeta`, `k6`
- 각 조합 `5회` 반복 측정
- 각 반복마다 `tools/benchmark-stats.sh`를 함께 실행
- 결과 세션 디렉토리 아래에 `manifest.csv`, raw 결과, `summary/summary.csv`, `summary/summary.md` 생성

주요 환경변수:

- `BENCHMARK_REPEATS=5`
- `BENCHMARK_MATRIX_TARGETS=proxy,caddy`
- `BENCHMARK_MATRIX_TOOLS=wrk,vegeta`
- `BENCHMARK_SESSION_NAME=report-baseline`
- `BENCHMARK_WRK_ARGS="30s 4 50,100,200,400"`
- `BENCHMARK_VEGETA_ARGS="300,600,900,1200 60s"`
- `BENCHMARK_K6_ARGS="30s 60s 30s 1200"`

예시:

```bash
BENCHMARK_SESSION_NAME=baseline-20260419 tools/benchmark-matrix.sh
```

이미 측정이 끝난 세션을 다시 요약하려면 아래 명령을 사용한다.

```bash
tools/benchmark-summary.sh plan/benchmarks/baseline-20260419
```

## 예시

```bash
BENCHMARK_TARGET=proxy tools/benchmark-stats.sh 120 2 &
BENCHMARK_TARGET=proxy tools/benchmark-wrk.sh
wait
```

```bash
BENCHMARK_TARGET=caddy tools/benchmark-vegeta.sh 400,800,1200 90s
```

```bash
BENCHMARK_TARGET=haproxy tools/benchmark-k6.sh 30s 60s 30s 1500
```

## 종료

```bash
docker compose -f composes/benchmark-check/compose.yaml down
```
