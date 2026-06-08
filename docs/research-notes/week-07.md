# 7주차 연구노트

## 진행 목표

6주차에는 성능 평가 결과를 바탕으로 reverse proxy의 요청 처리 경로와 transport 설정을 개선하였다. 이번 주차에는 성능 수치보다 로드밸런서 기능 요구사항과 비교했을 때 부족했던 운영 기능을 보완하였다.

특히 4주차에 구현한 healthy target 기준 선택 구조가 실제 backend 상태를 참조할 수 있도록 active health check를 구현하였다. 또한 4주차에 만든 대시보드 관측 구조에서 target별 health 상태와 active connection 값을 실제 runtime 값으로 조회할 수 있도록 응답 항목을 구체화하였다.

## 진행 내용

먼저 health check 설정과 health check 결과를 분리하였다. 설정에는 `health_check.path`, `interval`, `timeout`, `expect_status`처럼 검사 방법만 둔다. 실제 검사 결과는 설정 파일이나 desired state에 저장하지 않고, runtime 메모리의 `upstream.TargetState`에 저장하였다. backend의 health 상태는 사용자가 입력한 설정값이 아니라 현재 노드가 관찰한 실행 상태이므로, 설정과 같은 저장소에 두지 않았다. 이 방식에서는 프로세스를 재시작하면 target 상태를 다시 측정하고, 각 노드는 자신의 local health 상태를 따로 가진다.

`internal/upstream`에는 target별 상태를 표현하기 위해 `TargetState`를 사용하였다. 이 구조는 `Healthy`, `LastCheckedAt`, `LastError`를 가지고, health check 결과에 따라 `SetTargetHealthy()`와 `SetTargetUnhealthy()`로 갱신된다. 초기 상태는 모든 target을 healthy로 두었다. 서버가 시작되자마자 첫 health check 결과를 기다리느라 전체 backend를 차단하는 것은 PoC 단계에서 과도하다고 판단했기 때문이다. 이후 health check가 실패하면 해당 target은 unhealthy로 전환된다.

active health check는 `upstream.Checker`가 실행한다. `NewChecker()`는 runtime snapshot의 upstream registry를 받아 checker를 만들고, `Start(ctx)`는 health check 설정이 있는 pool마다 worker를 실행한다. 각 worker는 `health_check.interval`마다 target들을 검사한다. `Pool.CheckTarget()`은 `http://<target><path>`로 GET 요청을 보내고, 응답 status가 `expect_status`와 같으면 `SetTargetHealthy()`를 호출한다. 요청이 실패하거나 예상하지 않은 status가 나오면 `SetTargetUnhealthy()`를 호출한다. `timeout`은 요청 context에 적용하여 backend가 응답하지 않는 경우에도 무기한 대기하지 않도록 하였다.

4주차에 구현한 unhealthy target 제외 정책이 실제 health check 결과를 사용하도록 보강하였다. `Pool.NextTarget()`, `HashTarget()`, `LeastConnectionTarget()`은 이미 healthy target 목록을 기준으로 target을 선택하도록 작성되어 있었다. 이번 주차에는 `Pool.CheckTarget()`의 결과가 `SetTargetHealthy()`와 `SetTargetUnhealthy()`를 통해 그 healthy target 목록에 반영되도록 하였다. 모든 target이 unhealthy이면 target 선택은 실패하고, proxy는 `matched upstream pool has no healthy targets`에 해당하는 502 응답을 반환한다. 이 정책은 장애 target으로 요청을 계속 보내지 않고, 요청 가능한 backend가 없다는 상태를 명확하게 드러낸다.

6주차에서 만든 healthy target cache는 health check 결과를 반영하는 저장 위치로 사용하였다. `Pool`은 healthy target index snapshot을 `healthy atomic.Value`에 저장하고, health 상태가 바뀔 때 `storeHealthyIndexesLocked()`로 snapshot을 다시 만든다. 요청 처리 시점에는 `healthyTargetIndexes()`가 저장된 snapshot을 읽는다. 이 구조는 4주차의 알고리즘 선택 경로를 유지하면서, 7주차의 health check 결과를 선택 기준에 반영한다.

4주차에 구현한 Least Connections의 active counter는 runtime 응답에서 조회할 수 있도록 하였다. `Pool`은 target별 active counter를 가지고, `LeastConnectionTarget()`이 선택한 target의 counter를 증가시킨다. proxy 처리가 끝나면 release 함수가 counter를 감소시킨다. 대시보드 runtime 응답은 이 값을 `active_connections`로 반환한다. 이 값으로 현재 요청을 처리 중인 backend와 Least Connections 선택 결과를 확인할 수 있다.

관리자 대시보드/API에서는 runtime 상태 조회 응답이 실제 health check 결과를 표시하도록 하였다. `GET /api/status`는 전체 route 수, upstream pool 수, target 수, healthy target 수, unhealthy target 수를 요약해서 제공한다. `GET /api/runtime`은 route table과 upstream pool 목록을 반환하고, 각 target에 대해 `address`, `healthy`, `last_checked_at`, `last_error`, `active_connections`를 함께 제공한다. 이 응답은 설정값뿐 아니라 현재 프로세스가 관찰한 backend 상태를 보여준다.

마지막으로 테스트 범위를 보강하였다. upstream 단위에서는 초기 target이 선택 가능한지, unhealthy target이 선택에서 제외되는지, 모든 target이 unhealthy일 때 선택이 실패하는지 확인하였다. health check 단위에서는 성공 응답일 때 healthy로 전환되고, 실패나 예상하지 않은 status일 때 unhealthy로 전환되는지 확인하였다. app lifecycle에서는 health checker가 서버 실행과 함께 시작되고 종료 시 context cancel로 멈추는지 확인하였다. dashboard API에서는 runtime 응답에 target health와 active connection 정보가 포함되는지 확인하였다.

## 확인 및 결과

이번 주차 작업에서는 4주차에 구현한 health 기반 target 선택 구조에 실제 health check 실행 결과를 공급하였다. 실제 운영에서는 backend가 부분적으로 장애 상태가 될 수 있으므로, target 상태를 주기적으로 갱신하지 않으면 알고리즘이 오래된 상태를 기준으로 동작한다. 이번 구현은 active health check 결과를 `TargetState`에 저장하고, 그 결과를 target 선택 로직이 읽도록 만든다.

또한 health 상태와 설정값의 저장 위치를 분리하였다. route와 upstream pool 정의는 desired state로 관리하고, target별 healthy 여부와 active connection은 runtime local state로 관리한다. 여러 노드가 같은 설정을 공유하더라도 각 노드가 관찰하는 backend health 상태는 다를 수 있으므로, 이 구분은 이후 클러스터링 단계의 상태 설계 기준이 된다.

대시보드/API는 health 상태를 운영자가 직접 확인할 수 있는 조회 경로를 제공한다. 운영자는 `/api/status`로 전체 target 상태 요약을 보고, `/api/runtime`으로 어떤 target이 unhealthy인지, 마지막 검사 시각이 언제인지, 어떤 오류가 있었는지 확인한다. 이 응답을 사용하면 502 응답이 발생했을 때 어떤 backend가 선택 대상에서 제외되었는지 추적할 수 있다.

## 다음 주차 계획

8주차에는 단일 노드 로드밸런싱 PoC에서 클러스터링 PoC로 넘어갈 계획이다. 이번 주차까지 route, upstream, health, runtime 관측 구조를 구현했으므로, 다음 단계에서는 여러 로드밸런서 노드가 같은 설정을 공유하고 장애 상황에서도 일관된 제어를 수행하는 구조를 조사한다.

이를 위해 Raft와 같은 분산 합의 알고리즘을 학습하고, 클러스터링 기능 요구사항과 유저플로우를 작성한다. 특히 어떤 상태를 여러 노드가 공유하고, 어떤 상태를 노드별 local runtime 정보로 유지할지 구분한다.

## 관련 문서

- [Dashboard API](../api/dashboard-api.ko.md)
- [아키텍처 상세 설명](../architecture/architecture.ko.md)
- [Health Check 구현](../../internal/upstream/health.go)
- [Upstream 상태 구조](../../internal/upstream/upstream.go)
- [Upstream Registry와 Checker](../../internal/upstream/registry.go)
- [Balancer 구현](../../internal/upstream/balancer.go)
- [Runtime API 구현](../../internal/dashboard/runtime_api.go)
- [Dashboard View 모델](../../internal/dashboard/view.go)
- [App lifecycle](../../internal/app/server.go)
