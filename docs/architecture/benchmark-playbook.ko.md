# 벤치마크 플레이북

이 문서는 `composes/benchmark-check` 시나리오에서 리버스프록시와 비교군 프록시 성능을 어떻게 측정할지 정리한다.

## 목표

- `wrk`로 포화 지점과 최대 처리량 후보를 찾는다.
- `vegeta`로 고정 RPS에서 `p95/p99`, `error rate`를 비교한다.
- `k6`로 램프업, 유지, 스파이크 구간에서의 안정성을 본다.
- `docker stats`로 프록시 컨테이너의 `CPU/메모리`를 함께 기록한다.
- 동일한 백엔드 풀에 대해 `reverseproxy`, `Caddy`, `Nginx`, `HAProxy`를 round robin 기준으로 비교한다.

## 도구별 역할

### `wrk`

- 질문: 프록시가 어디서 포화되기 시작하는가
- 방식: thread와 connection을 높여 가며 짧은 구간 부하를 준다
- 핵심 지표: `requests/sec`, 평균 latency, tail latency, timeout 징후
- 해석: `RPS` 증가가 둔화되는데 `p99`가 급등하면 포화 구간 후보로 본다

권장 기본값:

- duration: `30s`
- threads: `4`
- connections: `50,100,200,400`

### `vegeta`

- 질문: 특정 RPS를 고정했을 때 얼마나 안정적인가
- 방식: 여러 단계의 rate를 고정하고 각 구간을 일정 시간 유지한다
- 핵심 지표: 실제 throughput, `p95`, `p99`, `error rate`, status code 분포
- 해석: 허용 가능한 `p99`와 `error rate`를 만족하는 최대 rate를 운영 가능한 최고 성능으로 본다

권장 기본값:

- rates: `300,600,900,1200`
- duration: `60s`

### `k6`

- 질문: 램프업과 스파이크가 있을 때 성능이 어떻게 흔들리는가
- 방식: steady rate를 만든 뒤 spike를 올리고 다시 내려온다
- 핵심 지표: 시나리오 단계별 latency 변화, 실패율, threshold 충족 여부
- 해석: steady 상태는 괜찮아도 spike에서 무너지면 운영 위험으로 본다

권장 기본값:

- ramp up: `30s`
- steady: `60s`
- spike: `30s`
- peak rate: `1200`

### `docker stats`

- 질문: 부하 동안 프록시가 CPU와 메모리를 얼마나 쓰는가
- 방식: 실험 시간 동안 샘플링한 CSV를 남긴다
- 핵심 지표: 평균 CPU, 피크 CPU, 평균 메모리, 피크 메모리, pids
- 해석: latency 상승과 동시에 CPU가 고정되면 CPU 포화 가능성이 높다. 메모리가 선형 증가하면 버퍼링이나 누수 가능성을 의심한다.

## 권장 실험 순서

1. `docker compose -f composes/benchmark-check/compose.yaml up -d --build`
2. `BENCHMARK_TARGET=proxy`, `caddy`, `nginx`, `haproxy` 순서로 대상 프록시를 바꿔 같은 절차를 반복
3. `tools/benchmark-wrk.sh`로 connection 단계별 한계점 탐색
4. `tools/benchmark-vegeta.sh`로 후보 구간 재측정
5. `tools/benchmark-k6.sh`로 운영형 시나리오 검증
6. 각 단계마다 `tools/benchmark-stats.sh`를 병행 실행

예시:

```bash
BENCHMARK_TARGET=nginx tools/benchmark-stats.sh 120 2 &
BENCHMARK_TARGET=nginx tools/benchmark-wrk.sh 30s 4 50,100,200,400
wait
```

```bash
BENCHMARK_TARGET=caddy tools/benchmark-stats.sh 240 2 &
BENCHMARK_TARGET=caddy tools/benchmark-vegeta.sh 400,800,1200,1600 90s
wait
```

```bash
BENCHMARK_TARGET=haproxy tools/benchmark-stats.sh 180 2 &
BENCHMARK_TARGET=haproxy tools/benchmark-k6.sh 30s 60s 30s 1600
wait
```

## 반복 측정 기본 원칙

- 각 비교군 x 각 벤치마크 조합은 `1회`가 아니라 `5회` 반복 측정을 기본으로 한다.
- 5회 중 일부 수치가 튄다고 해도 바로 제외하지 않고, 먼저 환경 흔들림인지 실제 불안정성인지 구분한다.
- 최종 비교는 단일 최고값이 아니라 `중앙값`, `평균`, `최소/최대`, `error rate`를 함께 본다.
- 보고서 본문에는 대표값으로 `median`, 부록이나 상세 표에는 `all 5 runs`와 `avg/min/max`를 함께 남긴다.

반복 측정 자동화는 아래 명령을 기본 경로로 사용한다.

```bash
tools/benchmark-matrix.sh
```

이 스크립트는 기본적으로 아래 조합을 수행한다.

- 대상: `proxy`, `caddy`, `nginx`, `haproxy`
- 도구: `wrk`, `vegeta`, `k6`
- 반복 수: `5`
- 산출물: 세션 디렉토리 아래 `manifest.csv`, 각 반복 raw 결과, `summary/raw-results.csv`, `summary/summary.csv`, `summary/summary.md`

예시:

```bash
BENCHMARK_SESSION_NAME=baseline-20260419 tools/benchmark-matrix.sh
```

이미 저장된 세션을 다시 집계할 때는 아래 명령을 사용한다.

```bash
tools/benchmark-summary.sh plan/benchmarks/baseline-20260419
```

실제 반복 측정 예시는 아래 문서를 참고한다.

- `docs/architecture/benchmark-session-2026-04-19.ko.md`
- `docs/architecture/rps-scenarios.ko.md`

## 결과 정리 기준

결과 비교 표는 아래 열을 기본으로 둔다.

- tool
- scenario
- target rate or connections
- actual rps
- p95
- p99
- error rate
- avg cpu
- peak cpu
- avg memory
- peak memory
- notes

반복 측정 표는 아래 두 단계로 나눠 보관한다.

1. raw table

- repeat
- target
- tool
- scenario
- actual rps
- avg latency
- p95
- p99
- max latency
- error rate
- success rate
- dropped iterations
- avg cpu
- peak cpu
- avg memory
- peak memory

2. summary table

- target
- tool
- scenario
- actual rps count
- actual rps avg
- actual rps median
- actual rps min
- actual rps max
- p95 avg
- p95 median
- error avg
- error max
- avg cpu avg
- avg memory avg

## 보고서 작성용 해석 가이드

보고서에서는 아래 순서로 판단하는 편이 안전하다.

1. 안정성 먼저

- `error rate`, `success rate`, `dropped iterations`가 불안정하면 해당 조합은 최고 처리량이 높아도 우선순위를 낮춘다.
- 특히 `k6`에서 dropped iteration이 반복되면 운영형 burst 대응력이 부족하다고 본다.

2. 대표값은 중앙값 우선

- `5회` 중 최고값 하나만 인용하지 않는다.
- `actual_rps_median`, `p95_median`을 본문 대표값으로 사용하고, `avg/min/max`는 변동성 설명에 사용한다.
- `avg`와 `median` 차이가 크면 환경 잡음 또는 내부 불안정성 가능성을 같이 적는다.

3. 벤치마크별 판정 질문을 분리

- `wrk`: 어디서 포화되고 headroom이 얼마나 남는가
- `vegeta`: 목표 RPS를 안정적으로 유지하는가
- `k6`: 램프업과 스파이크에서 SLA가 무너지는가
- `docker stats`: 같은 성능을 내기 위해 CPU/메모리를 얼마나 쓰는가

4. 변동성 자체도 결과로 본다

- 같은 조합에서 `min-max` 폭이 크면 운영 안정성이 낮다고 기록한다.
- 평균이 좋아도 특정 반복에서만 심하게 무너지면 "조건부 우수"가 아니라 "재현성 부족"으로 적는다.

5. 최종 결론은 한 줄 비교가 아니라 두 축 비교로 쓴다

- 성능 축: `throughput`, `p95/p99`, 포화 지점
- 운영 축: `error rate`, spike 안정성, CPU/메모리 비용

권장 서술 예시:

- `HAProxy`는 최대 처리량 headroom이 가장 컸지만, 본 실험 배치에서는 `vegeta 900` 변동성이 상대적으로 컸다.
- `Caddy`는 고정 RPS 안정성이 좋았고 반복 간 편차도 작아 운영형 기본값으로 해석하기 쉬웠다.
- `reverseproxy`는 최고치보다 `median`과 `p95` 개선 여부를 중심으로 추적해야 한다.

## 이상치 판단 기준

- 컨테이너 재기동, 호스트 부하 급증, 네트워크 오류처럼 외부 이벤트가 확인되면 해당 run을 별도 표시하고 재측정한다.
- 외부 이벤트가 없는데도 특정 비교군만 반복적으로 튄다면 그것은 제거 대상이 아니라 결과 본문에 포함할 불안정성 신호다.
- `wrk`는 순간 포화 탐색 성격이 강하므로 단건 최고값보다 반복 간 `RPS` 분산과 timeout 존재 여부를 더 중요하게 본다.
- `vegeta`, `k6`는 성공률이 무너지면 throughput 수치를 낮게 평가한다.

## 현재 시나리오 특성

- 프록시는 `round_robin`으로 `backend-a`, `backend-b`, `backend-c`에 분산한다.
- 각 백엔드는 `5ms` 응답 지연을 넣어 프록시 overhead와 tail latency 변화를 관찰하기 쉽게 한다.
- 기본 엔드포인트는 `GET /api/info`다.
- 기본 Host 헤더는 `benchmark.localtest.me`다.
- 기본 포트는 `reverseproxy=18080`, `caddy=18081`, `nginx=18082`, `haproxy=18083`다.

## 본측정 결과

아래 표는 `2026-04-19`에 `plan/benchmarks/final/` 아래 산출된 결과를 요약한 것이다.

### `wrk` 15s, 4 threads, `50/100/200` connections

| Target | 50 conn | 100 conn | 200 conn | 메모 |
| --- | ---: | ---: | ---: | --- |
| `reverseproxy` | `1622.79 RPS / 31.79ms` | `2671.52 / 41.02ms` | `3235.09 / 69.31ms` | 처리량이 가장 낮고 connection 증가에 따라 평균 latency 상승폭이 큼 |
| `caddy` | `2322.84 / 22.52ms` | `5871.89 / 20.69ms` | `6979.00 / 33.28ms` | 높은 처리량과 안정적인 평균 latency를 보임 |
| `nginx` | `3199.82 / 15.46ms` | `3195.77 / 32.18ms` | `3035.18 / 81.49ms` | `100` connection 이후 처리량이 거의 늘지 않았고 timeout `2`회 발생 |
| `haproxy` | `5246.96 / 10.02ms` | `6717.92 / 16.96ms` | `12081.42 / 18.96ms` | 최대 처리량과 평균 latency 모두 가장 강하게 나옴 |

### `vegeta` 30s, `300/600/900 RPS`

| Target | 300 RPS | 600 RPS | 900 RPS | 메모 |
| --- | --- | --- | --- | --- |
| `reverseproxy` | `p95 6.99ms / p99 13.13ms` | `p95 86.15ms / p99 236.32ms` | `p95 551.17ms / p99 760.85ms` | `600 RPS`부터 tail latency가 크게 증가 |
| `caddy` | `p95 7.48ms / p99 12.51ms` | `p95 15.43ms / p99 55.34ms` | `p95 29.45ms / p99 222.40ms` | 고정 RPS 안정성이 가장 좋음 |
| `nginx` | `p95 10.11ms / p99 18.86ms` | `p95 1.051s / p99 1.496s` | `p95 313.16ms / p99 483.77ms` | `600 RPS` 구간에서 크게 흔들림 |
| `haproxy` | `p95 14.54ms / p99 26.20ms` | `p95 22.67ms / p99 240.48ms` | `throughput 830.76 RPS`, `p95 363.33ms / p99 590.85ms` | `900 RPS`에서 목표 처리율을 유지하지 못함 |

### `k6` 15s ramp, 30s steady, 15s spike, peak `900`

| Target | Req Rate | Avg | p95 | Max | Dropped | 메모 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| `reverseproxy` | `483.23/s` | `9.41ms` | `23.65ms` | `164.16ms` | `34` | spike 구간에서 dropped iterations가 발생 |
| `caddy` | `483.65/s` | `6.57ms` | `7.96ms` | `100.67ms` | `0` | 운영형 시나리오에서도 매우 안정적 |
| `nginx` | `483.64/s` | `6.37ms` | `7.65ms` | `46.77ms` | `0` | `vegeta`와 달리 이 시나리오에서는 매우 안정적 |
| `haproxy` | `483.70/s` | `6.04ms` | `6.64ms` | `33.03ms` | `0` | 운영형 시나리오 기준으로 가장 우수 |

### 2차 튜닝 후 재측정 `reverseproxy` vs `haproxy`

아래 표는 1차 hot path 최적화와 2차 transport tuning 이후 `2026-04-19`에 다시 측정한 결과다. 산출물은 `plan/benchmarks/tuning2-20260419/` 아래에 저장했다.

| Scenario | `reverseproxy` | `haproxy` | HAProxy 대비 | 메모 |
| --- | --- | --- | ---: | --- |
| `vegeta 600 RPS` throughput | `599.87` | `599.91` | `99.99%` | 처리량은 사실상 동일 |
| `vegeta 600 RPS` `p95/p99` | `8.25ms / 13.84ms` | `11.10ms / 31.17ms` | latency 우세 | tail latency가 더 낮게 측정됨 |
| `vegeta 900 RPS` throughput | `899.85` | `899.84` | `100.00%` | 2차 전의 비정상 종료가 사라짐 |
| `vegeta 900 RPS` `p95/p99` | `8.74ms / 28.28ms` | `24.89ms / 46.59ms` | latency 우세 | 고정 고부하에서도 안정화됨 |
| `k6 peak 900` req rate | `483.70/s` | `481.25/s` | `100.51%` | 운영형 시나리오에서 동급 이상 |
| `k6 peak 900` `p95` | `8.18ms` | `27.19ms` | latency 우세 | dropped `0` vs `183` |
| `wrk 200 conn` RPS | `7830.25` | `9210.34` | `85.02%` | 목표였던 `80%`를 넘김 |
| `wrk 200 conn` avg latency | `26.86ms` | `22.98ms` | `116.88%` | 평균 latency는 아직 약간 높음 |

### 튜닝 전후 변화

`reverseproxy` 기준으로 보면 이번 두 단계 최적화의 변화폭은 아래와 같다.

| Scenario | 튜닝 전 | 1차 후 | 2차 후 | 변화 |
| --- | --- | --- | --- | --- |
| `vegeta 600 RPS` `p95` | `86.15ms` | `65.23ms` | `8.25ms` | 크게 개선 |
| `vegeta 900 RPS` | `p95 551.17ms / p99 760.85ms` | 비정상 종료 | `p95 8.74ms / p99 28.28ms` | 병목 제거 |
| `k6 peak 900` `p95` | `23.65ms` | `11.69ms` | `8.18ms` | 단계적 개선 |
| `k6 peak 900` dropped | `34` | `0` | `0` | 안정성 회복 유지 |
| `wrk 200 conn` RPS | `3235.09` | `2898.35` | `7830.25` | 2차에서 크게 상승 |

## 해석 요약

- 최대 처리량은 `HAProxy`가 가장 강했다.
- 고정 RPS 안정성은 `Caddy`가 가장 좋았다.
- 운영형 시나리오는 `HAProxy`, `Nginx`, `Caddy`가 모두 안정적이었고, `reverseproxy`는 상대적으로 불리했다.
- 현재 `reverseproxy`는 비교군 대비 `vegeta 600+ RPS`와 `k6` spike 구간에서 최적화 여지가 있다.
- 다만 2차 튜닝 후 재측정 기준으로는 `reverseproxy`가 목표였던 `HAProxy 대비 80%`를 넘어섰다.
- 특히 `vegeta 600/900`, `k6 peak 900`에서는 이번 배치 기준 `HAProxy`와 동급 이상으로 측정됐다.
- 남은 차이는 주로 `wrk 200`의 평균 latency와 최대 처리량 headroom 쪽이다.

## 측정 의미

- 이 문서의 `RPS`는 `클라이언트 -> 프록시 -> 업스트림 -> 프록시 -> 클라이언트` 전체 왕복이 끝난 뒤의 응답 기준이다.
- 즉, 업스트림 서버의 `GET /api/info`가 실제로 `200 OK`를 반환하고, 프록시가 그 응답을 받아 다시 클라이언트에 전달한 요청만 집계된다.
- `health check` 트래픽이나 프록시 내부에서만 끝나는 가상 응답은 포함되지 않는다.
- 현재 벤치마크 시나리오는 각 업스트림에 `SLEEP_MS=5`를 넣어 두었기 때문에, 결과에는 백엔드 처리시간 `5ms`와 프록시 오버헤드가 함께 반영된다.
- 따라서 이 문서의 수치는 순수 프록시 단독 성능이라기보다, `5ms` 응답 지연을 가진 업스트림을 둔 end-to-end 성능 비교 결과로 해석해야 한다.
- 다만 `RPS`는 일반적으로 `200 OK`만의 처리량을 뜻하지 않는다. 도구에 따라 `400`, `500` 같은 비정상 상태코드도 완료된 응답이면 처리량 집계에 포함될 수 있다.
- 그래서 성능 비교는 `RPS`만 단독으로 보지 않고, 반드시 `error rate`, `success ratio`, `status code` 분포를 함께 해석해야 한다.
- 이 문서의 비교 결과는 각 도구에서 `Status Codes`, `Success`, `http_req_failed`, `checks`를 함께 확인해 `200 응답 중심의 정상 처리`가 유지되는지 같이 검토한 값이다.

### 도구별 해석 기준

| Tool | 처리량 지표 | 같이 봐야 하는 지표 | 해석 포인트 |
| --- | --- | --- | --- |
| `wrk` | `Requests/sec` | `Latency`, `Socket errors`, timeout 여부 | `Requests/sec`만으로는 정상 응답 비율을 확정할 수 없으므로 latency 악화와 socket error를 함께 본다 |
| `vegeta` | `throughput` | `Success [ratio]`, `Status Codes`, `Error Set`, `p95/p99` | 고정 RPS를 준 상태에서 실제 처리율과 성공률이 유지되는지 본다 |
| `k6` | `http_reqs`, `iterations` | `http_req_failed`, `checks`, threshold, `p95/p99` | 시나리오형 부하에서 처리량뿐 아니라 SLA 기준을 만족하는지 본다 |

## reverseproxy 병목 분석과 개선 방향

이 섹션은 현재 코드 구조를 기준으로 `reverseproxy`가 왜 `HAProxy` 대비 불리한지, 그리고 어떤 순서로 줄여 나가야 할지를 설명한다. 이 단계의 목적은 곧바로 "어떤 최적화를 넣을지"보다 먼저 "어떤 비용이 매 요청 hot path에 붙어 있는지"를 분명히 하는 것이다.

### 목표 기준을 왜 `vegeta 600 RPS`와 `k6 peak 900`에 두는가

- `wrk`는 포화 지점을 빠르게 찾는 데 유용하지만, 운영에서 중요한 `p95/p99`, 성공률, steady 상태의 tail latency는 상대적으로 덜 잘 드러난다.
- 현재 결과에서 `reverseproxy`는 `wrk`보다 `vegeta 600+ RPS`와 `k6` spike 구간에서 더 명확하게 무너진다.
- 특히 `vegeta 600 RPS`에서 `reverseproxy`는 `p95 86.15ms`, `p99 236.32ms`로 급격히 악화된다. 같은 구간의 `HAProxy`는 `p95 22.67ms`, `p99 240.48ms`다. 즉 평균 처리량 문제가 아니라 queueing과 tail latency 관리가 핵심이라는 뜻이다.
- `k6 peak 900`에서는 `reverseproxy`만 dropped iteration `34`건이 발생했다. 이는 단순히 느린 것이 아니라, 부하 변화에 즉시 따라가지 못해 시나리오 수준의 안정성이 깨진다는 신호다.
- 그래서 1차 목표는 `HAProxy`의 최대 RPS를 그대로 따라가는 것이 아니라, `latency guardrail`을 지킨 상태에서 `vegeta 600 RPS`와 `k6 peak 900`의 안정성을 먼저 HAProxy 대비 `80%` 수준까지 끌어올리는 것이다.

### 병목 후보 1: 요청마다 프록시 객체와 upstream URL을 다시 준비하는 비용

현재 `internal/proxy/reverse_proxy.go`의 흐름상 요청을 업스트림으로 전달할 때, 업스트림 주소를 다시 해석하고 reverse proxy 관련 준비 작업을 요청 단위로 반복할 가능성이 높다.

이 패턴이 병목 후보인 이유는 아래와 같다.

- URL 파싱과 reverse proxy 생성은 기능적으로는 간단하지만, 요청 수가 커지면 작은 할당이 매우 많이 누적된다.
- 이 비용은 업스트림 I/O를 기다리는 동안 한 번만 드는 비용이 아니라, 모든 요청이 프록시를 타는 순간마다 반드시 지불하는 고정 비용이다.
- 고정 비용은 낮은 부하에서는 잘 보이지 않다가, `vegeta 600+`처럼 큐가 생기기 시작하는 구간에서 GC 압박과 scheduler 지연으로 tail latency를 빠르게 키운다.
- `k6` spike처럼 짧은 시간에 동시 요청이 몰릴 때는, 요청마다 같은 준비를 반복하는 구조가 순간적인 allocation burst를 만들고 dropped iteration으로 이어지기 쉽다.

왜 이것이 `HAProxy` 대비 약점으로 이어지느냐도 분명하다.

- `HAProxy`, `Nginx`, `Caddy`는 프록시 경로에서 필요한 라우팅 정보와 업스트림 연결 전략을 대체로 초기화 단계에서 준비하고, 런타임 hot path에서는 가능한 한 재사용한다.
- 반대로 애플리케이션 레벨 reverse proxy가 요청마다 같은 메타데이터를 재구성하면, 같은 기능을 수행하더라도 Go 런타임의 할당, escape, GC 영향을 더 직접적으로 받는다.

개선 방향은 요청 경로에서 "결정"만 하고, "준비"는 미리 끝내는 것이다.

- 업스트림별 정규화된 URL과 프록시 메타데이터를 초기화 시점에 준비한다.
- 요청 처리 시에는 선택된 target의 사전 계산된 정보를 바로 참조해 director 혹은 transport 입력만 최소 비용으로 갱신한다.
- 이렇게 하면 요청당 allocation 수와 문자열/URL 재가공 비용이 줄고, 결과적으로 `p95/p99`와 spike 복원력이 좋아질 가능성이 높다.

### 병목 후보 2: 요청마다 healthy target 목록을 다시 계산하는 비용

현재 `internal/upstream/balancer.go`는 target 선택 시점마다 healthy target을 다시 스캔하고, 그 결과를 새 슬라이스로 만드는 구조일 가능성이 높다. `round_robin`, `hash`, `least_connection`이 모두 이 기초 비용을 공유한다면, 로드밸런서 알고리즘이 단순해도 선택 전에 드는 비용이 커질 수 있다.

이 패턴이 병목 후보인 이유는 아래와 같다.

- health 상태는 보통 요청 수만큼 자주 바뀌지 않는다. 그런데 요청마다 전체 target을 재검사하면 변화 빈도보다 계산 빈도가 훨씬 높아진다.
- healthy target 인덱스를 매번 새 슬라이스로 만들면 lock 구간과 allocation이 같이 생긴다.
- target 수가 지금은 `3`개여도, 이 코드는 모든 요청이 반드시 지나가는 경로이기 때문에 작은 상수 비용이 전체 RPS를 직접 깎는다.
- `vegeta 600 RPS` 이후 tail latency가 커지는 현상은, 업스트림이 완전히 포화되지 않았더라도 hot path에서 lock 경쟁과 짧은 할당이 겹칠 때 흔히 보이는 형태다.

왜 이 구조가 `round_robin`에서 특히 아깝냐도 중요하다.

- `round_robin`은 본질적으로 가장 싼 정책이어야 한다. 다음 인덱스를 하나 고르고 modulo만 적용하면 끝나야 한다.
- 그런데 healthy target 재계산 때문에 정책보다 준비 비용이 더 비싸지면, 가장 단순한 알고리즘의 장점이 사라진다.
- 이번 비교 실험이 모두 `RR` 기준인 만큼, 여기서 줄인 비용은 바로 비교표의 처리량과 tail latency에 반영될 가능성이 높다.

개선 방향은 health 변경 시점과 요청 처리 시점을 분리하는 것이다.

- 요청 시점에는 이미 계산된 healthy target 캐시를 읽기만 하도록 만든다.
- health check 결과로 대상이 바뀌었을 때만 캐시를 다시 계산한다.
- 필요하면 atomic snapshot 혹은 copy-on-write 형태로 healthy target 집합을 교체해 read path의 lock 비용을 줄인다.
- 이렇게 하면 `NextTarget`, `HashTarget`, `LeastConnectionTarget` 모두가 공통으로 이득을 얻고, 특히 `RR`는 거의 상수 시간에 가까운 경로로 단순화할 수 있다.

### 왜 이 두 지점을 1차 병목으로 보는가

현재 결과는 "절대 최대 처리량 부족"만의 문제가 아니라 "부하가 올라갈수록 tail latency와 안정성이 급격히 나빠지는 문제"에 가깝다. 이 패턴은 다음과 같은 조건에서 자주 나온다.

- 요청마다 반복되는 작은 할당이 많다.
- 읽기 위주의 hot path에 불필요한 lock과 슬라이스 생성이 있다.
- 상태 변화가 드문 데이터까지 요청마다 다시 계산한다.

이번 `reverseproxy`의 증상은 여기에 잘 맞는다.

- 낮은 부하에서는 정상 동작한다.
- `vegeta 600+`에서 p95/p99가 급격히 나빠진다.
- `k6` spike에서 dropped iteration이 생긴다.

반대로 업스트림 자체가 먼저 병목이었다면 모든 프록시가 비슷한 형태로 무너져야 한다. 하지만 같은 업스트림 조건에서 `Caddy`, `Nginx`, `HAProxy`는 훨씬 안정적이다. 따라서 현재 1차 병목은 업스트림보다는 `reverseproxy`의 request hot path 비용일 가능성이 높다.

### 기대 효과와 검증 방식

이 두 지점을 줄였을 때 기대하는 변화는 아래와 같다.

- 요청당 allocation 감소
- GC 압박 완화
- read path lock 경쟁 감소
- `vegeta 600 RPS`에서 `p95/p99` 안정화
- `k6 peak 900`에서 dropped iteration 제거 또는 큰 폭 감소
- 같은 latency guardrail 안에서 더 높은 실제 throughput 확보

검증은 반드시 같은 시나리오로 다시 측정해야 한다.

- 1차 판정 구간: `vegeta 600 RPS`, `k6 peak 900`
- 2차 확인 구간: `vegeta 900 RPS`, `wrk 200 connections`
- 보조 지표: `docker stats`의 CPU, 메모리 변화

특히 성공 판정은 단순 RPS가 아니라 아래 조건을 함께 봐야 한다.

- `error rate`가 의미 있게 증가하지 않을 것
- `p95/p99`가 현재 기준보다 악화되지 않을 것
- `k6`에서 dropped iteration이 유지되거나 감소할 것
- 같은 guardrail에서 `HAProxy` 대비 목표치 `80%`에 가까워질 것

### 구현 원칙

이 문서 기준의 구현 원칙은 다음과 같다.

- 요청당 재계산되는 메타데이터를 초기화 단계나 상태 변경 시점으로 이동한다.
- read-heavy 경로는 lock과 allocation을 최소화한다.
- `RR` 경로를 우선 최적화하되, 다른 balancer 정책도 같은 기반 최적화의 이익을 받도록 만든다.
- 최적화는 반드시 재측정 가능한 형태로 넣고, 변경 전후 수치를 같은 플레이북으로 비교한다.

이 원칙을 따르면, 1차 목표인 `latency guardrail을 지킨 상태에서 HAProxy 대비 80%` 달성 가능성을 가장 직접적으로 높일 수 있다.

## 구현 반영 내용

아래 내용은 실제 코드에 반영한 1차 최적화다. 목적은 `round_robin` 기준의 선택 경로와 프록시 전달 경로에서 요청당 고정 비용을 줄이는 것이다.

### 변경 1: 요청마다 프록시 객체를 만들지 않도록 변경

대상 파일:

- `internal/proxy/reverse_proxy.go`

바뀐 로직:

- `Handler`에 `proxies sync.Map`을 추가했다.
- `serveProxyToTarget()`는 더 이상 요청마다 `httputil.NewSingleHostReverseProxy()`를 만들지 않는다.
- 대신 `proxyForTarget()`이 target별 캐시를 조회하고, 없을 때만 `newReverseProxy()`로 프록시 인스턴스를 만든다.
- `cachedProxy()`와 `storeProxy()`가 `sync.Map` 기반 재사용 경로를 담당한다.
- `newErrorHandler()`를 분리해 proxy 생성 시점에만 에러 핸들러를 붙인다.

코드 단위 흐름 변화:

1. 이전
   `serveProxyToTarget()` -> `upstreamURL(raw string)` -> `httputil.NewSingleHostReverseProxy()` -> 요청 처리
2. 이후
   `serveProxyToTarget()` -> `proxyForTarget(target)` -> 캐시 hit 시 즉시 `ServeHTTP()` -> miss 시 1회 생성 후 캐시에 저장

왜 효과가 있는가:

- `httputil.NewSingleHostReverseProxy()` 호출과 관련 closure 준비가 요청마다 반복되지 않는다.
- target별 프록시 구성이 한 번만 만들어지므로 allocation burst가 줄어든다.
- spike 구간에서 동일 target으로 몰리는 요청들이 같은 프록시 구성을 공유하므로 GC 압박을 낮출 수 있다.

### 변경 2: upstream URL 파싱을 초기화 시점으로 이동

대상 파일:

- `internal/upstream/upstream.go`
- `internal/upstream/registry.go`
- `internal/proxy/reverse_proxy.go`

바뀐 로직:

- `upstream.Target`에 `URL *url.URL` 필드를 추가했다.
- `copyPool()`에서 `prepareTargets()`와 `parseTargetURLs()`를 호출해 registry 생성 시점에 target URL을 미리 파싱한다.
- `serveProxyToTarget()`는 이제 raw 문자열 대신 미리 준비된 `target.URL`을 사용한다.
- `upstreamURL()`은 요청 시점의 파서가 아니라, 준비된 URL이 존재하는지만 확인하는 가벼운 검증 함수로 바뀌었다.

코드 단위 흐름 변화:

1. 이전
   요청 수신 -> `target.Raw` 조합 -> `url.Parse("http://" + raw)` -> reverse proxy 생성
2. 이후
   registry 생성 -> `parseTargetURLs()`로 `Target.URL` 준비 -> 요청 수신 -> 준비된 `Target.URL` 사용

왜 효과가 있는가:

- URL 파싱이 요청 수만큼 반복되지 않고 pool 준비 시점에 한 번만 수행된다.
- 잘못된 upstream target은 런타임 부하 중이 아니라 registry 초기화 시점에 더 빨리 드러난다.
- hot path에서 문자열 조합과 URL 파싱 비용이 빠진다.

### 변경 3: healthy target 인덱스를 요청마다 다시 만들지 않도록 변경

대상 파일:

- `internal/upstream/upstream.go`
- `internal/upstream/balancer.go`
- `internal/upstream/registry.go`

바뀐 로직:

- `Pool`에 `healthy atomic.Value` 캐시를 추가했다.
- `healthyTargetIndexes()`는 먼저 `cachedHealthyIndexes()`를 보고, 캐시가 있으면 lock 없이 그대로 사용한다.
- `SetTargetHealthy()`, `SetTargetUnhealthy()`, `setTargetState()`는 상태를 바꾼 직후 `storeHealthyIndexesLocked()`를 호출해 healthy 인덱스 snapshot을 갱신한다.
- `copyPool()`에서도 `ensureTargetStates()`와 `storeHealthyIndexesLocked()`를 호출해 초기 healthy snapshot을 미리 만든다.

코드 단위 흐름 변화:

1. 이전
   요청 수신 -> `NextTarget()`/`HashTarget()`/`LeastConnectionTarget()` -> `healthyTargetIndexes()` -> `RLock` -> 전체 target 스캔 -> 새 슬라이스 생성
2. 이후
   요청 수신 -> `NextTarget()`/`HashTarget()`/`LeastConnectionTarget()` -> `healthyTargetIndexes()` -> cached snapshot 읽기
   health 상태 변경 시에만 `storeHealthyIndexesLocked()`로 snapshot 재계산

왜 효과가 있는가:

- `RR` 선택 경로가 매 요청마다 `RLock + slice allocation`을 하지 않아도 된다.
- health 변화 빈도보다 훨씬 많은 요청 수에 대해 같은 계산을 반복하지 않게 된다.
- `HashTarget()`과 `LeastConnectionTarget()`도 같은 캐시를 공유하므로 공통 기반 비용이 줄어든다.

### 변경 4: 초기 상태를 hot path 친화적으로 정규화

대상 파일:

- `internal/upstream/registry.go`

바뀐 로직:

- `prepareTargets()`로 target 슬라이스를 pool 복사본 기준으로 분리했다.
- `ensureTargetStates()`로 target 수에 맞는 health 상태 배열을 정규화했다.
- `ensureActiveCounters()`와 함께 초기 선택 상태를 모두 registry 생성 시점에 맞춘다.

왜 효과가 있는가:

- 요청 처리 중에 "상태가 비어 있으면 어떻게 해석할지" 같은 보정 로직을 최소화할 수 있다.
- read path는 이미 정규화된 pool을 읽기만 하면 되므로 분기 수가 줄어든다.

### 현재 단계의 기대 효과와 한계

기대 효과:

- `round_robin` 기준 hot path의 요청당 allocation 감소
- `vegeta 600+ RPS` 구간의 `p95/p99` 완화
- `k6 peak 900`에서 dropped iteration 감소 가능성

현재 단계에서 아직 하지 않은 것:

- transport 레벨 connection pool 튜닝
- route resolution 캐시
- sticky cookie 경로의 health 조회 최적화
- 벤치마크 재측정 후 수치 기반 2차 병목 분석

즉 이번 변경은 "가장 명확한 고정 비용 2개"를 먼저 줄인 1차 작업이다. 이후에는 같은 플레이북으로 재측정해서, 남는 병목이 선택 경로인지 transport 경로인지, 혹은 route resolution인지 다시 좁혀야 한다.

## 2차 구현 계획

1차 변경 후 재측정 결과를 기준으로 보면, 다음 병목은 `balancer`보다는 `transport`일 가능성이 높다. 이유는 아래와 같다.

- `k6 peak 900`은 이미 `HAProxy` 수준까지 따라왔다.
- 반면 `vegeta 600 RPS`에서는 throughput은 거의 같아도 `p95/p99`가 크게 뒤처진다.
- `wrk 200 connections`에서는 여전히 `HAProxy` 대비 처리량이 매우 낮다.
- `vegeta 900 RPS`에서는 응답이 느려지는 수준을 넘어, 부하 생성기 쪽에서 thread exhaustion이 날 정도로 연결 폭증 신호가 보였다.

이 패턴은 선택 로직 자체보다, upstream 연결 재사용과 dial 제어가 충분하지 않을 때 자주 나타난다. 즉 "요청을 어느 target으로 보낼지"는 1차에서 가벼워졌지만, "그 target으로 어떤 연결 정책으로 보낼지"가 아직 조정되지 않은 상태라고 볼 수 있다.

### 왜 다음 병목을 `transport`로 보는가

현재 `internal/proxy/reverse_proxy.go`는 `Handler.transport`에 `http.DefaultTransport`를 그대로 넣고, 생성한 `ReverseProxy`에 그대로 주입한다.

이 구조의 한계는 아래와 같다.

- reverseproxy 전용 connection pool 정책이 없다.
- benchmark 시나리오처럼 upstream host 수와 동시 요청 패턴이 분명한 환경에서도 idle connection 크기를 명시적으로 맞추지 못한다.
- `MaxIdleConnsPerHost`, `MaxConnsPerHost` 같은 제어 없이 기본 동작에 맡기면, steady 구간은 버티더라도 높은 고정 RPS에서 dial churn이 커질 수 있다.
- 결국 요청당 새 연결이 너무 자주 생기거나, 반대로 connection reuse가 늦어지면서 queueing과 tail latency가 늘 수 있다.

특히 이번 결과에서 중요한 점은 `k6`는 개선됐는데 `vegeta`가 여전히 약하다는 것이다.

- `k6`는 단계형 부하라 연결이 서서히 올라가고, 재사용된 keep-alive 연결이 효과를 내기 쉽다.
- `vegeta`는 고정 RPS를 빠르게 밀어 넣기 때문에, 연결 재사용 정책이 약하면 socket 생성과 dial 경합이 더 직접적으로 드러난다.
- 따라서 2차는 request routing보다 transport reuse를 먼저 다루는 것이 맞다.

### 2차에서 바꿀 로직

대상 파일:

- `internal/proxy/reverse_proxy.go`
- `internal/proxy/transport.go` 신규
- `internal/proxy/reverse_proxy_test.go`
- `docs/architecture/benchmark-playbook.ko.md`

예정 심볼:

- `proxy.Handler`
- `proxy.NewHandler`
- `proxy.newTransport`
- `proxy.cloneDefaultTransport`
- `proxy.applyTransportDefaults`
- `proxy.transportConfig`
- `proxy.Handler.newReverseProxy`

핵심 방향은 `http.DefaultTransport` 직접 사용을 중단하고, reverseproxy 전용 transport를 명시적으로 만드는 것이다.

### 코드 단위 설계

#### 변경 후보 1: `proxy.NewHandler`에서 전용 transport 생성

현재는 `NewHandler()`가 `transport: http.DefaultTransport`를 그대로 넣는다.

2차에서는 아래 흐름으로 바꾼다.

1. `NewHandler()`
   `-> newTransport()`
   `-> tuned *http.Transport`
   `-> Handler.transport`

이렇게 하는 이유는 `http.DefaultTransport`를 전역처럼 공유하기보다, reverseproxy 인스턴스 목적에 맞는 pool 정책을 명시적으로 주기 위해서다.

#### 변경 후보 2: `transport.go`로 transport 생성 책임 분리

`internal/proxy/transport.go`에 transport 생성 책임을 분리한다.

예정 역할:

- `cloneDefaultTransport()`
  기본 transport 동작을 최대한 유지하되, 안전하게 `*http.Transport` 복사본을 얻는다.
- `applyTransportDefaults()`
  benchmark와 운영형 reverse proxy에 맞는 기본값을 주입한다.
- `newTransport()`
  위 두 단계를 묶어 최종 transport를 반환한다.
- `transportConfig`
  숫자 상수를 한곳에 모아 테스트와 조정이 가능하게 한다.

이렇게 분리하는 이유는 두 가지다.

- transport 파라미터는 앞으로 반복 튜닝 대상이 될 가능성이 높다.
- `reverse_proxy.go` 안에 상수와 세부 조정을 섞어 넣으면 이후 실험 때 diff와 책임이 비대해진다.

### 조정하려는 `http.Transport` 파라미터와 이유

#### `MaxIdleConns`

역할:

- 프로세스 전체가 유지할 수 있는 idle keep-alive connection 총량을 정한다.

왜 조정하는가:

- 현재 benchmark는 upstream이 `3`개뿐이고, steady 및 spike 시나리오가 반복된다.
- idle connection 총량이 작으면 이미 열어 둔 연결을 충분히 보관하지 못하고 다시 dial하게 된다.
- 이는 `vegeta 600+`에서 p95/p99 악화로 이어질 수 있다.

예상 효과:

- steady 구간 이후에도 warm connection을 더 많이 유지할 수 있다.
- 새 TCP 연결 생성 빈도가 줄어 tail latency가 완화될 가능성이 높다.

#### `MaxIdleConnsPerHost`

역할:

- host별로 유지할 idle connection 수를 정한다.

왜 조정하는가:

- 이번 시나리오는 `backend-a`, `backend-b`, `backend-c` 세 host로 RR 분산된다.
- host별 idle connection 한도가 낮으면 각 backend가 충분한 keep-alive 풀을 유지하지 못한다.
- 그 결과 RR로 target은 잘 고르더라도, 실제 전송은 자꾸 새 연결을 열게 된다.

예상 효과:

- 각 backend에 대해 warm connection을 미리 유지할 수 있다.
- RR 시나리오에서 target 선택 최적화가 실제 네트워크 reuse 이득으로 이어진다.

#### `MaxConnsPerHost`

역할:

- host별 동시 connection 상한을 정한다.

왜 조정하는가:

- 상한이 전혀 없으면 burst 시점에 dial이 과도하게 퍼질 수 있다.
- 특히 `vegeta 900`에서 본 현상처럼, 처리량보다 먼저 연결 생성 경쟁이 커지면 부하 생성기와 프록시 모두 불안정해질 수 있다.

주의점:

- 너무 낮게 잡으면 오히려 throughput ceiling이 생긴다.
- 따라서 이 값은 "연결 폭증을 막되, 정상 처리량을 자르지 않는 수준"으로 잡아야 한다.

예상 효과:

- pathological burst에서 connection storm를 줄인다.
- p99와 max latency의 이상치를 줄일 가능성이 있다.

#### `IdleConnTimeout`

역할:

- idle keep-alive connection을 얼마나 오래 유지할지 정한다.

왜 조정하는가:

- 현재 benchmark는 steady 뒤에 spike가 온다.
- idle timeout이 너무 짧으면 spike 직전에 연결이 사라져, 다시 warm-up 비용을 지불하게 된다.
- 반대로 너무 길면 유휴 연결을 과도하게 붙들 수 있다.

예상 효과:

- steady와 spike 사이의 재사용 효율을 높인다.
- `k6`뿐 아니라 고정 RPS 구간에서도 warm connection 유지에 유리하다.

#### `ResponseHeaderTimeout`

역할:

- upstream이 헤더를 돌려주기까지 기다리는 상한 시간을 정한다.

왜 조정하는가:

- 이번 병목의 본질은 timeout이 아니라 reuse지만, 고부하에서 연결이 꼬일 때 오래 매달리는 요청이 tail latency를 더 키울 수 있다.
- 명시적 upper bound를 두면 pathological hang이 오래 이어지는 것을 줄일 수 있다.

주의점:

- upstream이 원래 느린 서비스라면 너무 공격적으로 낮추면 오탐 timeout이 생긴다.
- 이번 benchmark upstream은 `5ms` 수준이므로 비교적 공격적인 값도 검토 가능하다.

#### `ExpectContinueTimeout`

역할:

- `100-continue` 관련 대기 시간이다.

왜 포함하는가:

- 현재 시나리오에서는 큰 영향이 없을 가능성이 높다.
- 다만 transport 기본값을 명시적으로 관리하는 구조를 만들 때 함께 정리해 두면 future drift를 줄일 수 있다.

### 기대 효과

2차에서 기대하는 직접 효과는 아래와 같다.

- upstream keep-alive 재사용률 증가
- burst 시 dial 폭증 억제
- `vegeta 600 RPS`의 `p95/p99` 완화
- `vegeta 900 RPS`의 비정상 실패 제거 또는 완화
- `wrk 200 connections`의 처리량 상승

특히 1차는 hot path allocation을 줄여 `k6`를 개선했고, 2차는 transport reuse를 개선해 `vegeta`와 `wrk`를 끌어올리는 역할을 기대한다.

### 리스크와 주의점

- `MaxConnsPerHost`를 너무 낮게 잡으면 throughput이 오히려 감소할 수 있다.
- idle pool을 너무 크게 잡으면 메모리와 file descriptor 사용량이 늘 수 있다.
- benchmark에만 맞춘 값이 운영 일반화에 맞지 않을 수 있다.

그래서 2차는 "정답 상수 확정"보다 먼저 "transport 파라미터를 명시적으로 제어할 수 있는 구조"를 만드는 데 의미가 있다.

### 2차 성공 판정 기준

성공 판정은 아래 순서로 본다.

1. `vegeta 600 RPS`
   throughput 유지, `p95/p99` 개선 여부
2. `vegeta 900 RPS`
   비정상 종료 제거 여부, 성공률 유지 여부
3. `wrk 200 connections`
   RPS 상승 여부
4. `k6 peak 900`
   1차에서 얻은 안정성이 깨지지 않는지 확인

최종적으로는 `latency guardrail`을 지킨 상태에서 `HAProxy 대비 80%`에 가까워졌는지를 본다. 따라서 2차는 단순 최고 RPS보다, `vegeta 600`과 `vegeta 900`의 안정성 회복을 더 중요한 판정 기준으로 둔다.

### 2차 구현 반영 내용

아래 내용은 실제 코드에 반영한 2차 최적화다. 목적은 upstream connection reuse를 늘리고, 고정 RPS 구간에서 dial churn을 줄이는 것이다.

#### 변경 1: `http.DefaultTransport` 직접 사용 제거

대상 파일:

- `internal/proxy/reverse_proxy.go`
- `internal/proxy/transport.go`

바뀐 로직:

- `Handler.transport` 타입을 일반 `http.RoundTripper`가 아니라 `*http.Transport`로 구체화했다.
- `NewHandler()`는 더 이상 `http.DefaultTransport`를 직접 넣지 않고 `newTransport()`를 통해 reverseproxy 전용 transport를 생성한다.

코드 단위 흐름 변화:

1. 이전
   `NewHandler()` -> `transport: http.DefaultTransport`
2. 이후
   `NewHandler()` -> `newTransport()` -> tuned `*http.Transport`

왜 효과가 있는가:

- reverseproxy가 upstream 연결 풀 정책을 명시적으로 소유하게 된다.
- 이후 벤치마크 기반으로 transport 상수를 조정할 수 있는 구조가 생긴다.

#### 변경 2: transport 생성 책임을 `transport.go`로 분리

대상 파일:

- `internal/proxy/transport.go`

추가한 로직:

- `transportConfig`
- `defaultTransportConfig()`
- `cloneDefaultTransport()`
- `applyTransportDefaults()`
- `newTransport()`

각 함수의 역할:

- `cloneDefaultTransport()`
  Go 기본 transport 동작을 유지하기 위해 `http.DefaultTransport.(*http.Transport).Clone()`으로 복사본을 만든다.
- `defaultTransportConfig()`
  이번 벤치마크 기준으로 사용할 기본 상수를 한 곳에 모은다.
- `applyTransportDefaults()`
  복사된 transport에 connection pool, timeout, dialer 값을 주입한다.
- `newTransport()`
  위 단계를 묶어 최종 transport를 반환한다.

왜 이렇게 나눴는가:

- `reverse_proxy.go`에서 프록시 흐름과 transport 상수 조정 책임을 분리하기 위해서다.
- 이후 3차, 4차 튜닝에서 transport 값만 바꿀 때 diff 범위를 작게 유지할 수 있다.

#### 변경 3: keep-alive와 host별 연결 풀 크기 명시

대상 파일:

- `internal/proxy/transport.go`

적용한 기본값:

- `MaxIdleConns = 512`
- `MaxIdleConnsPerHost = 128`
- `MaxConnsPerHost = 256`
- `IdleConnTimeout = 90s`
- `ResponseHeaderTimeout = 2s`
- `ExpectContinueTimeout = 1s`
- `DialContext = net.Dialer{Timeout: 3s, KeepAlive: 30s}`

이 값들을 넣은 이유:

- `3`개의 backend에 대해 RR 분산되는 benchmark에서 host별 idle connection을 충분히 유지하기 위해서다.
- `MaxConnsPerHost`로 비정상적인 connection storm를 제어하되, 정상 처리량을 과하게 자르지 않는 선을 먼저 잡기 위해서다.
- `ResponseHeaderTimeout`은 pathological hang을 오래 끌지 않도록 상한을 둔 것이다.
- `DialContext` timeout과 keep-alive를 명시해 dial 실패와 재사용 정책을 기본값 의존에서 분리했다.

#### 변경 4: transport 설정 검증 테스트 추가

대상 파일:

- `internal/proxy/reverse_proxy_test.go`

추가한 테스트:

- `TestNewHandler_UsesTunedTransport`
- `TestNewTransport_AppliesConfiguredLimits`
- `requireTransportDefaults`

검증 내용:

- `NewHandler()`가 `http.DefaultTransport`를 그대로 재사용하지 않는지
- transport limit과 timeout 값이 기대한 기본값으로 들어가는지
- `DialContext`가 nil이 아닌지

이 테스트를 추가한 이유는 이번 2차 작업의 핵심이 라우팅 의미 변경이 아니라 transport policy 명시화이기 때문이다. 즉 동작 회귀뿐 아니라 "정말로 전용 transport를 쓰고 있는지" 자체를 테스트로 고정해야 한다.
