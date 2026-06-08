# 6주차 연구노트

## 진행 목표

5주차 성능 평가에서는 자체 구현 `proxy`가 기본 요청 전달과 로드밸런싱 기능은 수행하지만, 고부하 구간에서 HAProxy와 차이가 크다는 점을 확인하였다. 특히 `vegeta 600 RPS` 이후 p95/p99 latency가 빠르게 증가했고, `k6 peak 900`에서는 spike 구간에서 dropped iteration이 발생하였다.

이번 주차에는 1차 측정 결과를 바탕으로 병목 후보를 확인하고, 요청 처리 경로의 고정 비용을 줄이는 방향으로 리팩토링을 진행하였다. 목표는 단순히 최고 RPS를 올리는 것이 아니라, 고정 RPS와 spike 상황에서 tail latency와 실패 가능성을 낮추는 것이었다.

## 진행 내용

먼저 1차 측정 결과에서 개선 우선순위를 정하였다. `wrk 200 connections`에서 `proxy`는 `3235.09 RPS`를 기록했고, HAProxy는 `12081.42 RPS`를 기록하였다. 처리량 차이도 컸지만, 운영 안정성 관점에서는 `vegeta`와 `k6` 결과가 더 중요했다. `proxy`는 `vegeta 600 RPS`에서 `p95 86.15ms`, `p99 236.32ms`를 기록했고, `900 RPS`에서는 `p95 551.17ms`, `p99 760.85ms`까지 증가하였다. 또한 `k6 peak 900`에서는 p95 `23.65ms`와 dropped iteration `34`건이 발생하였다. 이 결과를 기준으로 `vegeta 600 RPS`, `k6 peak 900`, `wrk 200 connections`를 우선 확인 구간으로 결정하였다.

첫 번째 병목 후보는 요청마다 reverse proxy 객체와 upstream URL을 준비하는 비용이었다. 기존 흐름에서는 요청이 들어올 때마다 target 주소를 해석하고 `httputil.NewSingleHostReverseProxy()`를 통해 proxy 객체를 준비하는 비용이 반복될 수 있었다. 이 비용은 낮은 부하에서는 크게 보이지 않지만, 요청 수가 늘어나면 URL 파싱, 객체 생성, closure 준비, GC 압박이 tail latency 증가로 이어질 수 있다. 이를 줄이기 위해 `proxy.Handler`에 `proxies sync.Map`을 추가하고, `proxyForTarget()`에서 target별 `httputil.ReverseProxy`를 캐시하도록 구성하였다. 요청 처리 함수인 `serveProxyToTarget()`은 매번 새 proxy를 만들지 않고, 이미 준비된 proxy가 있으면 곧바로 `ServeHTTP()`를 호출하도록 변경하였다.

두 번째 개선은 upstream URL 파싱 위치를 요청 처리 시점에서 초기화 시점으로 옮기는 것이었다. `upstream.Target`에 `URL *url.URL` 필드를 추가하고, upstream registry를 만들 때 target URL을 미리 파싱하도록 구성하였다. 이후 proxy 처리 경로에서는 `target.Raw` 문자열을 다시 조합하거나 파싱하지 않고, 이미 준비된 `target.URL`을 사용하였다. 이 변경은 요청마다 반복되는 문자열 처리와 URL 파싱 비용을 줄이고, 잘못된 target 주소를 부하 중이 아니라 초기 준비 과정에서 더 빨리 확인할 수 있게 한다.

세 번째 개선은 healthy target 목록을 요청마다 다시 계산하지 않도록 하는 것이었다. 기존 구조에서는 `NextTarget()`, `HashTarget()`, `LeastConnectionTarget()`이 healthy target 목록을 얻기 위해 target 상태를 스캔하고 새 목록을 만들 수 있었다. 그러나 health 상태는 요청 수만큼 자주 바뀌지 않는다. 따라서 `upstream.Pool`에 `healthy atomic.Value` 캐시를 두고, health 상태가 바뀔 때만 `storeHealthyIndexesLocked()`로 healthy index snapshot을 갱신하도록 하였다. `healthyTargetIndexes()`는 캐시가 있으면 lock 없이 해당 snapshot을 읽고, 캐시가 없을 때만 상태를 스캔한다. 이 구조는 Round Robin처럼 단순해야 하는 선택 경로에서 불필요한 lock과 allocation을 줄인다.

네 번째 개선은 reverse proxy 전용 `http.Transport`를 명시적으로 구성하는 것이었다. `NewHandler()`에서 `http.DefaultTransport`를 직접 사용하는 대신 `newTransport()`를 호출하도록 변경하고, `internal/proxy/transport.go`에 transport 생성 책임을 분리하였다. `cloneDefaultTransport()`는 Go 기본 transport를 복사하고, `applyTransportDefaults()`는 `MaxIdleConns`, `MaxIdleConnsPerHost`, `MaxConnsPerHost`, `IdleConnTimeout`, `ResponseHeaderTimeout`을 설정한다. 기본값은 `MaxIdleConns=512`, `MaxIdleConnsPerHost=128`, `MaxConnsPerHost=256`, `IdleConnTimeout=90s`, `ResponseHeaderTimeout=2s`로 두었다. 이 설정은 backend 3개에 대해 keep-alive connection을 더 안정적으로 재사용하고, spike 상황에서 새 연결 생성이 과도하게 늘어나는 것을 줄이기 위한 것이다.

리팩토링 후에는 같은 성격의 시나리오로 `proxy`와 HAProxy를 다시 비교하였다. `vegeta 600 RPS`에서는 `proxy`가 throughput `599.87`, `p95 8.25ms`, `p99 13.84ms`를 기록하였고, HAProxy는 throughput `599.91`, `p95 11.10ms`, `p99 31.17ms`를 기록하였다. `vegeta 900 RPS`에서는 `proxy`가 throughput `899.85`, `p95 8.74ms`, `p99 28.28ms`를 기록하여 고정 RPS 구간의 tail latency가 크게 안정되었다. `k6 peak 900`에서는 `proxy`가 req rate `483.70/s`, p95 `8.18ms`, dropped `0`을 기록하였다. `wrk 200 connections`에서는 `proxy`가 `7830.25 RPS`, HAProxy가 `9210.34 RPS`를 기록하여 처리량 차이가 줄어들었다.

| 항목 | 1차 측정 | 리팩토링 후 측정 |
| --- | ---: | ---: |
| `vegeta 600 RPS` p95 | `86.15ms` | `8.25ms` |
| `vegeta 900 RPS` p95 | `551.17ms` | `8.74ms` |
| `k6 peak 900` p95 | `23.65ms` | `8.18ms` |
| `k6 peak 900` dropped | `34` | `0` |
| `wrk 200 conn` RPS | `3235.09` | `7830.25` |

## 확인 및 결과

이번 주차 작업을 통해 1차 성능 평가에서 나타난 문제를 단순한 처리량 부족으로만 볼 수 없다는 점을 확인하였다. `proxy`는 낮은 부하에서는 동작했지만, 고정 RPS와 spike 상황에서 tail latency가 빠르게 증가하였다. 이 현상은 요청마다 반복되는 proxy 준비 비용, healthy target 재계산, upstream connection 재사용 부족이 함께 영향을 준 것으로 판단하였다.

리팩토링 결과는 병목 후보를 줄인 방향이 유효했음을 보여준다. `vegeta 600 RPS`와 `900 RPS`에서 p95가 크게 낮아졌고, `k6 peak 900`의 dropped iteration도 사라졌다. `wrk 200 connections` 처리량도 `3235.09 RPS`에서 `7830.25 RPS`로 증가하였다. 다만 HAProxy는 여전히 최대 처리량과 평균 latency 측면에서 강한 비교군이므로, 자체 구현이 모든 조건에서 우위에 있다고 보기는 어렵다. 이번 주차의 의미는 최고 성능을 완성했다는 것보다, 병목을 지표와 코드 구조에 연결하고 재측정으로 개선 효과를 확인했다는 데 있다.

## 다음 주차 계획

7주차에는 리팩토링 이후에도 남아 있는 기능적 한계를 요구사항과 비교하여 보완할 계획이다. 성능 개선 과정에서 요청 처리 경로와 target 선택 구조가 안정화되었으므로, 다음 주차에는 health 상태 반영, 운영 편의 기능, 대시보드 관측 항목처럼 실제 사용 흐름에서 부족한 부분을 확인한다.

또한 성능 수치만으로는 로드밸런서의 완성도를 판단하기 어렵기 때문에, 장애 target 제외, 설정 변경 처리, route 충돌 방지 같은 운영 기능도 함께 확인한다. 이 작업은 이후 클러스터링 PoC로 넘어가기 전에 단일 노드 로드밸런싱 기능의 완성도를 높이는 단계로 이어진다.

## 관련 문서

- [벤치마크 플레이북](../architecture/benchmark-playbook.ko.md)
- [1차 wrk 결과](https://github.com/lb-ajou/reverseproxy-poc/blob/main/plan/benchmarks/final/first-test-wrk-rps.csv)
- [1차 vegeta 결과](https://github.com/lb-ajou/reverseproxy-poc/blob/main/plan/benchmarks/final/first-test-vegeta-p95-ms.csv)
- [1차 k6 결과](https://github.com/lb-ajou/reverseproxy-poc/blob/main/plan/benchmarks/final/first-test-k6-p95-ms.csv)
- [리팩토링 후 vegeta 결과](https://github.com/lb-ajou/reverseproxy-poc/blob/main/plan/benchmarks/tuning2-20260419/vegeta-proxy-20260419-115824/vegeta-r900.txt)
- [리팩토링 후 k6 결과](https://github.com/lb-ajou/reverseproxy-poc/blob/main/plan/benchmarks/tuning2-20260419/k6-proxy-20260419-115959/k6-summary.txt)
- [리팩토링 후 wrk 결과](https://github.com/lb-ajou/reverseproxy-poc/blob/main/plan/benchmarks/tuning2-20260419/wrk-proxy-20260419-120124/wrk-c200.txt)
- [Reverse Proxy 구현](../../internal/proxy/reverse_proxy.go)
- [Transport 설정](../../internal/proxy/transport.go)
- [Upstream 상태 구조](../../internal/upstream/upstream.go)
- [Balancer 구현](../../internal/upstream/balancer.go)
