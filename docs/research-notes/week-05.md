# 5주차 연구노트

## 진행 목표

4주차까지 구현한 reverse proxy와 로드밸런싱 알고리즘이 실제 부하 상황에서 어느 정도의 처리량과 안정성을 보이는지 확인하고자 하였다. 이번 주차에는 자체 구현 `proxy`를 기존 오픈소스 프록시와 같은 조건에서 비교할 수 있도록 벤치마크 환경을 구성하였다.

비교 대상은 Nginx와 HAProxy로 잡았다. 두 프록시는 로드밸런싱과 reverse proxy 영역에서 많이 사용되고, 2주차 조사에서도 내부 구조와 설정 방식을 확인한 대상이기 때문이다. 이번 주차의 성능 평가는 자체 구현 `proxy`가 기존 오픈소스 로드밸런서와 비교했을 때 어느 수준의 처리량과 안정성을 보이는지 확인하는 데 초점을 두었다.

## 진행 내용

먼저 비교 환경을 Docker Compose 기반으로 구성하였다. 자체 구현 `proxy`, Nginx, HAProxy가 모두 동일한 backend pool을 바라보도록 하였고, backend는 `benchmark-a`, `benchmark-b`, `benchmark-c` 세 개로 구성하였다. 테스트 대상 엔드포인트는 `GET /api/info`로 맞추고, Host header는 `benchmark.localtest.me`를 사용하였다. 포트는 `proxy=18080`, `nginx=18082`, `haproxy=18083`으로 나누어 같은 시나리오를 대상만 바꿔 실행할 수 있게 하였다.

비교 조건에서는 로드밸런싱 방식도 통일하였다. 각 프록시는 동일한 backend 3개에 대해 Round Robin 기준으로 요청을 분산하도록 설정하였다. 이렇게 구성한 이유는 이번 주차의 목적이 알고리즘별 우열을 따지는 것이 아니라, 동일한 분산 조건에서 프록시 자체의 요청 처리 성능과 안정성을 비교하는 것이기 때문이다. 또한 backend 응답 조건을 동일하게 맞추어, 측정 결과가 backend 차이보다 프록시 처리 경로의 차이를 반영하도록 하였다.

벤치마크 도구는 역할을 나누어 선정하였다. `wrk`는 connection 수를 높여 가며 프록시의 포화 지점과 최대 처리량 후보를 확인하는 데 사용하였다. 이번 1차 측정에서는 `15s`, `4 threads`, `50/100/200 connections` 조건을 사용하였다. `vegeta`는 일정한 RPS를 유지하면서 latency와 실패율을 확인하는 데 사용하였다. 조건은 `300/600/900 RPS`, 각 `30s`로 구성하였다. `k6`는 `15초 ramp-up`, `30초 steady`, `15초 spike`, `peak 900` 구조로 설정하여 실제 운영에서 부하가 증가하고 튀는 상황을 확인하였다. 마지막으로 `docker stats`는 이후 자원 사용량을 함께 보기 위한 보조 측정 항목으로 두었다.

측정 결과는 처리량만으로 판단하지 않고 latency, 실패 여부, spike 상황에서의 dropped iteration을 함께 확인하였다. `wrk`는 최대 처리량 후보를 확인하는 데 적합하지만, 정상 응답 비율이나 tail latency를 충분히 설명하지 못할 수 있다. 따라서 `vegeta`와 `k6` 결과를 함께 보면서 고정 부하와 운영형 부하에서의 안정성을 확인하였다.

`wrk` 결과에서는 HAProxy가 가장 높은 처리량을 보였다. HAProxy는 `50 connections`에서 `5246.96 RPS`, `100 connections`에서 `6717.92 RPS`, `200 connections`에서 `12081.42 RPS`를 기록하였다. 자체 구현 `proxy`는 같은 조건에서 `1622.79 RPS`, `2671.52 RPS`, `3235.09 RPS`를 기록하였다. connection 수가 늘어날수록 처리량은 증가했지만, HAProxy와의 차이는 컸고 평균 latency도 `31.79ms`, `41.02ms`, `69.31ms`로 함께 증가하였다. Nginx는 `50 connections`에서는 `3199.82 RPS`로 `proxy`보다 높았지만, `100 connections` 이후 처리량이 거의 늘지 않았고 `200 connections`에서는 `3035.18 RPS`를 기록하였다.

`vegeta` 결과에서는 자체 구현 `proxy`의 고정 RPS 안정성 문제가 더 분명하게 나타났다. `300 RPS`에서는 `p95 6.99ms`, `p99 13.13ms`로 양호했지만, `600 RPS`에서는 `p95 86.15ms`, `p99 236.32ms`로 크게 증가하였다. `900 RPS`에서는 `p95 551.17ms`, `p99 760.85ms`까지 올라가면서 고부하 구간의 tail latency가 뚜렷하게 악화되었다. Nginx는 `600 RPS`에서 `p95 1.051s`, `p99 1.496s`로 크게 흔들렸고, HAProxy도 `900 RPS`에서 throughput이 `830.76 RPS`로 떨어져 목표 요청률을 완전히 유지하지 못하였다. 이 결과를 통해 단순 처리량뿐 아니라 고정 RPS에서의 tail latency도 함께 봐야 한다는 점을 확인하였다.

`k6` 운영형 시나리오에서도 `proxy`의 한계가 확인되었다. 자체 구현 `proxy`는 요청률 `483.23/s`, 평균 latency `9.41ms`, p95 `23.65ms`를 기록했고, spike 구간에서 dropped iteration `34`건이 발생하였다. 반면 Nginx와 HAProxy는 같은 시나리오에서 dropped iteration이 없었다. HAProxy는 p95 `6.64ms`, Nginx는 `7.65ms`를 기록하였다. 이 결과를 통해 `proxy`는 steady 구간에서는 기본 동작이 가능하지만, 부하가 갑자기 증가하는 상황에서는 비교군보다 안정성이 낮다는 점을 확인하였다.

이번 1차 결과는 현재 구현 상태에서 병목 후보를 찾기 위한 성능 기준선으로 사용하였다. 특히 `vegeta 600 RPS` 이후 tail latency가 빠르게 커진 점과 `k6` spike 구간에서 dropped iteration이 발생한 점은 다음 주차 리팩토링의 핵심 근거가 되었다.

| 비교 항목 | 확인한 내용 |
| --- | --- |
| 최대 처리량 | HAProxy가 가장 높고, `proxy`는 `200 connections`에서 `3235.09 RPS`를 기록하였다. |
| 고정 RPS 안정성 | `proxy`는 `600 RPS`부터 tail latency가 크게 증가하였다. |
| 운영형 시나리오 | `proxy`는 `k6` spike 구간에서 dropped iteration `34`건이 발생하였다. |
| 비교군 특징 | HAProxy는 처리량이 가장 높았고, Nginx는 일부 시나리오에서 처리량 확장이 제한적으로 나타났다. |

## 확인 및 결과

이번 주차를 통해 자체 구현 `proxy`가 기본적인 로드밸런싱 기능을 넘어 실제 비교 가능한 성능 측정 대상이 되었음을 확인하였다. 다만 현재 구현 상태에서는 처리량과 안정성 모두에서 비교군과 차이가 컸다. `wrk`에서는 HAProxy보다 낮은 처리량을 보였고, `vegeta`에서는 `600 RPS` 이상부터 tail latency가 크게 증가하였다.

동시에 1차 벤치마크가 다음 작업의 방향을 정하는 데 충분한 근거가 된다는 점도 확인하였다. 자체 구현 `proxy`는 `300 RPS`나 낮은 connection 구간에서는 기본 동작을 수행했지만, 부하가 커지면 latency가 빠르게 악화되었다. 따라서 다음 주차에는 단순 기능 추가보다 요청 처리 경로, upstream 선택 비용, connection 재사용 같은 병목 후보를 확인하는 작업이 필요하다.

## 다음 주차 계획

6주차에는 이번 벤치마크 결과를 바탕으로 자체 구현 `proxy`의 병목 가능성을 확인하고 리팩토링을 진행할 계획이다. 특히 `vegeta 600 RPS` 이후 급격히 증가한 tail latency와 `k6 peak 900`에서 발생한 dropped iteration을 중심으로 원인을 확인한다.

또한 동일한 벤치마크 시나리오를 다시 사용하여 리팩토링 결과를 확인할 수 있도록 한다. 이때 단순히 최고 RPS가 올랐는지만 보지 않고, p95/p99 latency, 반복 간 변동성, CPU와 메모리 사용량까지 함께 확인한다.

## 관련 문서

- [벤치마크 플레이북](../architecture/benchmark-playbook.ko.md)
- [RPS 시나리오와 서비스 규모 해석](../architecture/rps-scenarios.ko.md)
- [benchmark-check compose 설명](../../composes/benchmark-check/README.md)
- [1차 wrk 결과](https://github.com/lb-ajou/loadbalancer/blob/main/docs/benchmark/final/first-test-wrk-rps.csv)
- [1차 vegeta 결과](https://github.com/lb-ajou/loadbalancer/blob/main/docs/benchmark/final/first-test-vegeta-p95-ms.csv)
- [1차 k6 결과](https://github.com/lb-ajou/loadbalancer/blob/main/docs/benchmark/final/first-test-k6-p95-ms.csv)
