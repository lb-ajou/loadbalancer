# 15주차 연구노트

## 진행 목표

14주차에는 로드밸런서 노드를 최초  실행한 뒤 API나 CLI를 통해 cluster를 bootstrap하거나 join할 수 있도록 lifecycle 흐름을 정리하였다. 이번 주차에는 이 흐름을 통합 테스트 관점에서 다시 확인하고, 실제 운영자가 Raft 기반 로드밸런서 cluster 상태를 확인하고 설정을 변경할 수 있는 관리 화면을 개발하였다.

이번 주차에는 통합 테스트를 진행하고 로드밸런서의 전체 라이프사이클을 다듬는 것에 집중하였다. 따라서 단순히 개별 API나 화면을 추가하는 것이 아니라, 3노드 Raft cluster, backend server, VIP failover, 대조군 환경을 함께 놓고 정상 요청 처리와 장애 상황을 확인하는 것을 목표로 하였다. 여기에 더해 통합 테스트에서 반복적으로 확인해야 하는 cluster 상태, leader 여부, VIP ownership, route/upstream runtime 상태를 웹 대시보드에서 볼 수 있도록 프론트엔드 작업을 함께 진행하였다.

## 진행 내용

먼저 기존 Raft HA 테스트 환경을 기준으로 통합 테스트 항목을 정리하였다. `raft-ha-cluster` 시나리오는 3개의 reverse proxy node와 3개의 backend server를 구성하고, bootstrap, join, 설정 복제, leader 장애, 재합류, persistence를 확인한다. 초기에는 clean node인 `proxy-1`을 bootstrap하여 single-node Raft cluster를 만들고, 이후 `proxy-2`, `proxy-3`을 join시킨다. 설정은 dashboard/Admin API를 통해 route와 upstream pool을 저장하고, 각 node의 proxy port에서 같은 host 기반 routing이 동작하는지 확인한다.

VIP failover는 별도의 `raft-ha-vip` 시나리오로 확인하였다. 이 환경에서는 세 proxy가 같은 Docker bridge network에 있고, Raft leader만 VIP를 보유한다. observer container는 개별 node IP가 아니라 VIP 주소로 요청을 보내므로, leader 장애 후 새 leader가 VIP를 획득하고 HTTP 요청이 다시 성공하는지 확인할 수 있다. 이 시나리오는 Keepalived 대조군에서 보던 VIP owner 전환과 같은 관점으로 프로젝트 구현을 검증하기 위한 것이다.

웹 대시보드를 통한 통합 시나리오 테스트의 비교 기준도 다시 정리하였다. 정상 상태에서는 route matching, upstream 선택, health check, runtime target 상태가 일관되게 동작해야 한다. 장애 상태에서는 leader 중단 후 새 leader 선출, follower write 거절, failover 후 설정 write 가능 여부, 죽었던 node의 재기동 후 catch-up, VIP owner 단일성, backend 장애 시 unhealthy target 제외 여부를 확인한다. 대조군 관점에서는 Nginx와 Spring Boot backend로 구성한 일반 L7 reverse proxy 경로, Keepalived 기반 VIP failover, CephFS 기반 상태 공유 환경을 함께 두고 프로젝트 구현의 역할을 구분하였다. CephFS는 상태 파일 전파 비교에 가깝고, Keepalived는 VIP failover 비교에 가깝기 때문에 프로젝트의 Raft 기반 구현과 동일한 지표로만 단순 비교하지 않도록 하였다.

대시보드 프론트엔드 개발에서는 Raft 기반 로드밸런서 통합 관리 대시보드를 개발하였다. 앱 진입 시 `GET /api/node/cluster-status`를 호출하여 node가 아직 cluster에 속하지 않았으면 `/setup`으로 이동하고, 이미 cluster 상태이면 `/overview`로 이동한다. `/setup` 화면에서는 node ID, Raft advertise/bind address, VIP interface, Raft timing, VIP address, GARP 설정을 입력해 bootstrap을 수행할 수 있다. 기존 cluster에 join할 때는 peer dashboard URL 목록을 입력하고 `POST /api/node/join-cluster`를 호출한다.

운영 요약 화면인 `/overview`에서는 `GET /api/status`와 `GET /api/cluster`를 사용하여 route 수, upstream pool 수, target health, Raft leader, quorum 상태, write availability를 표시한다. leader가 없거나 unhealthy target이 있거나 현재 node에서 설정 write가 불가능하면 alert로 보여준다. 이 화면은 통합 테스트 중 현재 cluster가 어떤 상태인지 빠르게 확인하는 용도로 사용할 수 있다.

노드 진단 화면인 `/node`도 보강하였다. 이 화면은 local node ID, Raft address, proxy/dashboard listener, projection 상태, VIP address와 interface, Raft state, leader address, quorum 상태를 보여준다. 또한 Raft timing 값과 last log index, commit index, applied index를 표시하여 설정 write 이후 적용 지연이 발생했을 때 commit gap이나 apply gap을 확인할 수 있게 하였다. 이는 단순히 UI를 보기 좋게 만드는 작업이 아니라, 통합 테스트에서 장애 원인을 추적하기 위한 관찰 지점을 마련한 것이다.

웹 대시보드를 구현하는 과정에서 dashboard API 계약도 리팩토링하였다. 이전에는 namespace 단위 config API와 route/upstream 개별 CRUD 흐름이 남아 있었지만, 현재 구조에서는 cluster 전체가 하나의 proxy config를 공유한다. 따라서 프론트엔드와 백엔드 모두 `GET /api/config`, `PUT /api/config` 기반의 전체 설정 교체 모델로 정리하였다. route와 upstream pool을 개별 endpoint로 수정하는 대신, 클라이언트가 현재 config를 읽어 로컬 상태에서 편집한 뒤 전체 config를 저장하는 방식이다. HA mode에서는 이 저장 요청이 하나의 Raft `replace_config` command로 합의된다.

API 응답에서 운영상 의미가 중복되거나 오래된 필드도 제거하였다. `GET /api/runtime`은 현재 node에 적용된 route와 upstream target detail을 보여주는 endpoint로 좁히고, node identity나 config count 같은 metadata는 제거하였다. 운영 요약과 count는 `GET /api/status`에서 확인하고, Raft leader와 member 정보는 `GET /api/cluster`에서 확인하도록 역할을 분리하였다. 또한 `/api/status`의 legacy `config_store` 필드를 제거하여 UI가 더 이상 과거 file/raft 저장소 구분에 의존하지 않도록 하였다.

Cluster lifecycle 상태 API도 단순화하였다. 기존에는 `can_bootstrap`, `can_join`, `has_raft_state`, `raft_running` 같은 파생 필드를 함께 내려주었지만, 프론트엔드는 이제 `state` 값 하나를 기준으로 분기한다. `unconfigured`이면 bootstrap/join form을 활성화하고, `existing_state`이면 기존 Raft state 복구 대상임을 표시하며, `clustered`이면 일반 운영 화면으로 이동한다. Raft data dir 검사에 실패한 경우에는 `check_error` 상태와 `last_error`를 표시하고 setup 동작을 막는다.

또한 Route와 upstream 관리 화면도 새 API 계약에 맞게 변경하였다. namespace 개념을 제거하면서 화면은 단일 config를 기준으로 route table과 upstream pool table을 관리한다. route나 pool을 생성, 수정, 삭제할 때는 클라이언트 상태에서 config 전체를 갱신하고, 저장 시 `PUT /api/config`로 전송한다. 저장 요청이 follower node에서 실행되어 `not_raft_leader` 오류가 발생하면 leader address를 표시하고 관련 status/cluster query를 재조회하도록 하였다. clean node에서 설정 write를 시도해 `cluster_not_configured`가 반환되는 경우에는 setup 화면으로 이어질 수 있도록 query invalidation 기준을 정리하였다.

프론트엔드 폼 검증도 함께 정리하였다. route와 upstream pool 입력은 `valibot`과 `formisch` 기반으로 마이그레이션하여 UI 입력값과 백엔드 config schema 사이의 차이를 줄였다. 특히 route의 host, path match, upstream pool 참조, health check 입력값은 저장 전 클라이언트에서 먼저 검증하고, 서버의 validation error가 내려오면 field 단위 message를 표시할 수 있게 하였다. 이를 통해 통합 테스트 중 잘못된 설정을 저장한 뒤 원인을 찾기 어려운 상황을 줄이고자 하였다.

## 검토 및 결과

이번 주차 작업을 통해 전체 통합 시나리오 테스트를 진행할 수 있었다. 프로젝트 구현은 단일 node reverse proxy가 아니라, Raft leader election, desired config 복제, runtime projection, VIP ownership, backend health 상태가 함께 동작해야 하는 HA L7 로드밸런서이다. 따라서 테스트도 단순 요청 성공 여부뿐 아니라 설정 write 위치, leader 장애 후 write 가능 여부, VIP owner 전환, node 재시작 후 state 복구, backend 장애 시 runtime target 상태까지 함께 확인해야 한다.

API 리팩토링 결과, 프론트엔드가 따라야 할 계약도 단순해졌다. 편집용 설정은 `/api/config`, 운영 요약은 `/api/status`, runtime detail은 `/api/runtime`, cluster membership은 `/api/cluster`, node lifecycle은 `/api/node/cluster-status`로 분리되었다. 이 구분은 화면 구현뿐 아니라 테스트 중 어떤 endpoint를 확인해야 하는지도 명확하게 만든다.

프론트엔드 대시보드는 통합 테스트의 관찰 도구 역할을 하게 되었다. `/setup`은 14주차에서 만든 lifecycle API를 웹에서 실행하는 진입점이고, `/overview`는 cluster 전체의 운영 요약을 확인하는 화면이며, `/node`는 Raft와 VIP 상태를 더 자세히 추적하는 화면이다. route/upstream 관리 화면은 Raft desired config를 수정하는 UI로 동작한다. 이로써 CLI나 curl만으로 확인하던 bootstrap, join, status, config write 흐름을 웹에서도 같은 API 기준으로 수행할 수 있게 되었다.

한계도 남아 있다. 이번 주차에서는 통합 테스트 항목과 운영 화면을 정리하는 데 집중했기 때문에, 장시간 장애 상황에서 down된 voter를 자동으로 제거하거나 non-voter로 재합류시키는 기능 등 추가적인 기능 범위의 확장은 진행하지 않았다. hard kill 이후 stale VIP가 남는 문제도 startup cleanup과 재시작 절차로 완화할 수는 있지만, 죽은 node가 장시간 방치되는 상황을 즉시 해결하지는 못한다. 또한 전체 로드밸런서를 컨테이너 형태로 배포하는 파이프라인을 구성하고, Nginx, Spring Boot backend, Keepalived, CephFS 대조군을 하나의 자동화된 통합 테스트 파이프라인으로 완전히 묶는 작업은 최종 정리 단계에서 보강할 필요가 있다.

## 다음 주차 계획

16주차에는 최종 배포와 최종 보고서 작성을 진행한다. Docker image와 정적 대시보드 번들 배포 절차를 정리하고, GitHub Action 기반 자동 이미지 빌드 및 배포 파이프라인을 구성한다. 로드밸런서의 Docker Compose 기반 실행 방법과 테스트 방법을 최종 문서에 반영한다.

또한 1주차부터 15주차까지의 흐름을 최종 보고서로 정리한다. 로드밸런싱 PoC, 알고리즘 구현, 성능 비교, Raft clustering, VIP failover, 통합 테스트, 대시보드 개발 내용을 하나의 프로젝트 결과로 연결하고, 남은 한계와 향후 개선 방향을 함께 정리한다.

## 관련 문서

- [Dashboard API](../api/dashboard-api.ko.md)
- [Raft HA 클러스터 테스트 가이드](../architecture/raft-ha-test-guide.ko.md)
- [Raft HA VIP 테스트 가이드](../architecture/raft-ha-vip-test-guide.ko.md)
- [Raft Config State](../architecture/raft-config-state.ko.md)
