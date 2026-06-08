# 4주차 연구노트

## 진행 목표

3주차에 만든 reverse proxy 요청 처리 흐름 위에 로드밸런싱 알고리즘을 적용할 수 있는 구조를 만들고자 하였다. 3주차에는 요청을 받아 backend 서버로 전달하는 기본 흐름을 구성했으므로, 이번 주차에는 여러 backend 중 어떤 서버를 선택할지 결정하는 방식을 구체화하였다.

이번 주차에서는 Round Robin, Hash, Least Connections 알고리즘의 기본 개념을 학습하고, target 선택 로직을 하나의 고정된 방식이 아니라 설정에 따라 바꿀 수 있는 구조로 설계하고자 하였다. 또한 관리자 대시보드에서는 route, backend, health 상태뿐 아니라 적용된 알고리즘도 함께 표시하는 구조를 구성하고자 하였다.

## 진행 내용

먼저 Round Robin 알고리즘을 기본 선택 방식으로 사용하였다. Round Robin은 backend 서버 목록을 순서대로 순회하면서 요청을 분산하는 방식이다. 구현에서는 `upstream.Pool`이 `Targets` 목록과 다음 선택 위치를 나타내는 counter를 가지고, `NextTarget()`이 요청마다 counter를 증가시킨 뒤 target을 반환하도록 구성하였다. target 선택 시에는 먼저 healthy 상태인 target index 목록을 만들고, 증가한 counter 값을 healthy target 개수로 나눈 나머지를 사용해 실제 target index를 결정하였다. 이 구조에서는 장애 상태인 target이 순회 대상에서 제외되고, 정상 target들 사이에서만 순환 선택이 이루어진다.

다음으로 Hash 계열 알고리즘을 확인하였다. Hash 방식은 요청에서 특정 값을 뽑아 해시를 계산하고, 그 결과를 backend 서버 목록에 매핑하는 방식이다. 구현에서는 `proxy.Handler`가 요청 정보를 읽어 hash key를 만들고, `upstream.Pool`의 hash 기반 target 선택 함수가 해당 key를 healthy target index로 변환하도록 구성하였다. hash key는 protocol, client 주소, client port, destination host, destination port처럼 요청에서 얻을 수 있는 값들을 조합하였다. `upstream.Pool`에서는 이 key를 해시 값으로 바꾸고, healthy target 개수로 나눈 나머지를 사용해 target을 선택하였다. 같은 key와 같은 healthy target 집합이 유지되면 같은 backend가 선택되므로, 같은 요청 흐름을 같은 서버로 보낼 수 있다.

Least Connections 알고리즘도 함께 구현하였다. Least Connections는 현재 처리 중인 요청 수가 가장 적은 backend를 선택하는 방식이다. 구현에서는 `upstream.Pool`에 target별 active counter를 두고, `LeastConnectionTarget()`이 healthy target 중 active 값이 가장 작은 target을 찾도록 구성하였다. 이때 healthy target들을 순회하면서 가장 낮은 active 값을 확인하고, 같은 값을 가진 target이 여러 개이면 해당 후보들 안에서 Round Robin 방식으로 하나를 선택하였다. 선택된 target은 proxy 요청을 시작할 때 active counter를 증가시키고, `httputil.ReverseProxy`를 통한 proxy 처리가 끝난 뒤 release 함수를 통해 감소시키는 방식으로 수명주기를 맞춘다. 이 방식은 응답 시간이 긴 요청이 특정 backend에 몰렸을 때, 다음 요청을 상대적으로 덜 점유된 backend로 보내는 데 사용된다.

이번 주차의 핵심은 알고리즘 자체보다 알고리즘을 적용하는 위치를 분리하는 것이었다. 요청의 host나 path를 보고 어떤 backend pool로 보낼지 결정하는 작업과, 그 pool 안에서 실제 target을 고르는 작업을 서로 다른 책임으로 나누었다. route 해석은 요청 조건을 기준으로 backend 그룹을 찾는 역할로 두고, `proxy.Handler`의 target 선택 흐름은 해당 그룹 안에서 어떤 서버를 사용할지 결정하는 역할로 두었다. 이 둘을 분리하여 route 규칙을 바꾸지 않고 알고리즘만 교체하거나, 알고리즘은 유지한 채 요청 조건만 수정하는 구조를 만들었다.

알고리즘 모듈화 과정에서는 health 상태와의 관계도 함께 고려하였다. 로드밸런싱 알고리즘은 전체 target 목록이 아니라 healthy target 목록을 기준으로 동작하도록 구성하였다. Round Robin은 unhealthy target을 건너뛰도록 하였고, Hash 방식은 healthy target 집합 위에서만 매핑하도록 하였으며, Least Connections도 장애가 있는 target을 후보에서 제외하도록 하였다. 이를 통해 target 선택 로직이 알고리즘 종류와 관계없이 health 상태를 공통으로 반영하도록 구성하였다.

| 알고리즘 | 선택 기준 | 구현 방식 | 특징 |
| --- | --- | --- | --- |
| Round Robin | healthy target을 순서대로 순환 | `NextTarget()`에서 counter를 증가시키고 healthy target 개수로 나눈 나머지를 사용 | 구현이 단순하고 기본 분산 방식으로 사용하기 적합 |
| Hash | 요청에서 만든 hash key | 요청 정보로 만든 key를 해시 값으로 변환한 뒤 healthy target에 매핑 | 같은 key가 유지되면 같은 backend를 선택할 수 있음 |
| Least Connections | active connection 수가 가장 적은 target | target별 active counter를 비교하고, 선택 후 증가와 요청 종료 후 감소를 수행 | 긴 요청이 몰린 backend를 피하는 데 사용 가능 |

관리자 대시보드 2차 구현에서는 현재 route와 backend pool, backend health 상태, 선택 알고리즘을 표시하는 구조를 구성하였다. 이후 테스트 과정에서는 이 화면을 통해 특정 요청이 어떤 route에 매칭되고, 어떤 pool과 알고리즘을 거쳐 backend로 전달되는지 확인한다. 이번 주차에서는 대시보드를 기능 조작 도구라기보다, 로드밸런서 내부 상태를 확인하는 관측 도구로 설계하였다.

## 확인 및 결과

4주차 작업을 통해 로드밸런서의 핵심이 단순한 요청 전달이 아니라 target 선택 정책에 있다는 점을 확인할 수 있었다. Round Robin은 기본 분산 방식으로 적합하고, Hash 방식은 같은 입력을 같은 backend로 보내는 일관성에 장점이 있으며, Least Connections는 현재 요청 점유 상태를 반영할 수 있다는 장점이 있다. 각 알고리즘은 목적이 다르므로 하나의 방식만 고정하지 않고 route나 설정에 따라 선택할 수 있는 구조로 구성하였다.

또한 route 해석과 backend 선택을 분리한 구조가 이후 확장에 유리하다는 점을 확인하였다. 3주차의 reverse proxy 흐름이 요청을 backend로 전달하는 기본 경로였다면, 4주차의 알고리즘 모듈화는 그 경로 안에서 target 선택 정책을 바꿀 수 있게 만드는 작업이다. 이 구조에서는 성능 비교나 기능 보완 과정에서 알고리즘별 동작을 따로 검증할 수 있다.

## 다음 주차 계획

5주차에는 구현한 로드밸런싱 로직을 테스트하고, Nginx 및 HAProxy와의 성능 비교를 준비할 계획이다. 이를 위해 비교 대상과 테스트 환경을 정하고, 동일한 backend 조건에서 자체 구현과 기존 오픈소스 로드밸런서를 비교하는 환경을 구성한다.

또한 벤치마크 도구를 선정하고, 처리량, 지연시간, 실패율 같은 지표를 어떤 방식으로 측정할지 정리한다. 4주차에서 정리한 알고리즘 구조는 이후 테스트에서 요청 분산이 의도대로 동작하는지 확인하는 기준으로 사용한다.

## 관련 문서

- [라우팅 알고리즘 추가 플레이북](../architecture/routing-algorithm-playbook.ko.md)
- [아키텍처 상세 설명](../architecture/architecture.ko.md)
- [Dashboard API](../api/dashboard-api.ko.md)
