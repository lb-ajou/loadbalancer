# 2주차 연구노트

## 진행 목표

2주차의 목표는 로드밸런싱 PoC를 진행하기 전에 기존 오픈소스 로드밸런서의 구조를 조사하는 것이었다. 조사 대상은 Nginx와 HAProxy로 정하였다. 두 도구는 모두 reverse proxy와 로드밸런싱 기능을 제공하지만, 요청을 처리하는 구조와 설정을 구성하는 방식에는 차이가 있기 때문에 자체 구현 방향을 잡기 전에 먼저 비교할 필요가 있었다.

이번 주차에서는 벤치마크 도구나 성능 측정 방식은 다루지 않고, Nginx와 HAProxy의 내부 구조 및 설정 방식에 집중하였다. 기능 요구사항과 유저플로우는 1주차에서 이미 정리했으므로 반복하지 않았다. 대신 두 시스템이 클라이언트 요청을 어떤 구조로 받아들이고, 어떤 설정 단위를 통해 backend 서버로 전달하는지를 확인하는 데 중점을 두었다.

## 진행 내용

먼저 Nginx의 기본 구조를 조사하였다. Nginx 공식 문서에 따르면 Nginx는 하나의 master process와 여러 worker process로 구성된다. master process는 설정을 읽고 평가하며 worker process를 관리하고, 실제 요청 처리는 worker process가 담당한다. worker process는 이벤트 기반 모델과 운영체제별 메커니즘을 사용해 요청을 처리한다. 이 구조는 하나의 프로세스가 모든 요청을 직접 처리하는 방식이 아니라, master가 전체 실행을 관리하고 worker들이 실제 연결과 요청 처리를 맡는 방식으로 이해할 수 있다.

Nginx 설정 파일 구조도 함께 확인하였다. Nginx 설정은 directive 단위로 작성되며, `events`, `http`, `server`, `location` 같은 context 안에 관련 설정을 배치한다. HTTP 요청 처리에서는 `server` 블록이 가상 서버 단위로 동작하고, `location` 블록이 요청 URI에 따라 어떤 처리를 할지 결정한다. 공식 문서에서는 여러 `location`이 있을 때 prefix와 regular expression 기준으로 요청을 매칭하는 흐름을 설명하고 있다. 이 구조는 요청 조건을 먼저 해석한 뒤 해당 위치에서 proxy 동작을 지정하는 방식이다.

Nginx에서 reverse proxy는 주로 `proxy_pass` directive를 통해 설정된다. 특정 `location`에 들어온 요청을 지정된 proxied server로 전달하고, 응답을 다시 클라이언트에 돌려주는 구조다. 또한 `proxy_pass`는 단일 서버뿐 아니라 upstream 서버 그룹을 대상으로도 사용할 수 있다. HTTP load balancing 문서에서는 `upstream` 블록 안에 여러 backend 서버를 정의하고, `location`에서 `proxy_pass http://backend`처럼 해당 upstream 그룹을 참조하는 방식을 제시한다. 로드밸런싱 방식은 기본적으로 Round Robin을 사용할 수 있고, `least_conn` 같은 방식을 지정해 active connection 수가 적은 서버로 요청을 보낼 수도 있다.

다음으로 HAProxy의 구조와 설정 방식을 조사하였다. HAProxy 공식 configuration manual에서는 설정이 `global`, `defaults`, `frontend`, `backend`, `listen` 같은 section으로 구성된다고 설명한다. 이 중 `frontend`는 클라이언트 연결을 받아들이는 listening socket 집합을 나타내고, `backend`는 프록시가 요청을 전달할 서버 집합을 나타낸다. `listen`은 frontend와 backend를 하나로 합친 complete proxy 형태로 설명된다. 이 구조는 Nginx처럼 `server`와 `location` context를 따라 요청을 처리하는 방식과 다르게, 요청 수신 지점과 backend 서버 그룹을 명시적으로 분리해서 표현한다.

HAProxy의 frontend 설정은 클라이언트가 접속할 IP 주소와 포트를 정의하는 역할을 한다. 공식 튜토리얼의 예시에서는 `frontend` 안에서 `mode http`, `bind :80`, `default_backend web_servers`를 지정한다. 여기서 `bind`는 들어오는 연결을 받을 주소와 포트를 의미하고, `default_backend`는 조건에 맞는 별도 backend 선택 규칙이 없을 때 사용할 backend 서버 그룹을 의미한다. 또한 `use_backend`와 조건식을 사용하면 Host header 같은 요청 조건에 따라 서로 다른 backend로 트래픽을 보낼 수 있다.

HAProxy의 backend 설정은 실제 요청을 처리할 서버 pool을 정의한다. 공식 튜토리얼에서는 `backend web_servers` 안에 `balance roundrobin`과 여러 `server` 항목을 배치하는 예시를 제공한다. 각 `server` 항목에는 서버 이름, IP 주소, 포트가 들어가며, `check` 옵션을 붙이면 health check를 통해 응답하지 않는 서버를 로드밸런싱 대상에서 제외할 수 있다. 또한 `balance leastconn`을 사용하면 연결 수가 가장 적은 서버로 트래픽을 보낼 수 있으며, 공식 문서에서는 hash, source, random 등 다른 알고리즘도 제공된다고 설명한다.

조사 결과 Nginx와 HAProxy는 모두 reverse proxy와 로드밸런싱 기능을 제공하지만, 설정을 바라보는 방식이 다르다는 점을 확인하였다. Nginx는 `http` 안의 `server`와 `location`을 중심으로 요청 조건과 proxy 동작을 연결하고, backend 서버 목록은 `upstream` 블록으로 정의한다. HAProxy는 `frontend`에서 요청을 받고 `backend`에서 서버 pool과 로드밸런싱 방식을 정의하는 구조가 더 직접적으로 드러난다. 이 차이는 이후 자체 로드밸런서에서 요청 조건을 해석하는 계층과 backend 선택 계층을 어떻게 나눌지 검토하는 데 참고할 수 있다.

## 검토 및 결과

이번 주차 조사를 통해 Nginx와 HAProxy를 단순한 성능 비교 대상이 아니라 구조 비교 대상으로도 볼 필요가 있음을 확인하였다. Nginx는 master/worker 구조와 event 기반 요청 처리 방식을 가지고 있으며, 설정에서는 `server`, `location`, `upstream`, `proxy_pass`가 핵심 역할을 한다. 반면 HAProxy는 `frontend`와 `backend`를 중심으로 요청 수신 지점과 서버 pool을 분리해서 표현하며, `balance`와 `server` 설정을 통해 로드밸런싱 방식을 명확하게 지정한다.

두 시스템의 공통점은 클라이언트 요청을 받아 backend 서버로 전달하고, 여러 backend 중 하나를 선택할 수 있는 구조를 제공한다는 점이다. 차이점은 Nginx가 웹 서버와 reverse proxy 설정 흐름 안에서 load balancing을 구성하는 느낌이 강한 반면, HAProxy는 요청 수신, backend 선택, 서버 pool 관리가 별도 section으로 더 명확하게 나뉜다는 점이다. 이 차이는 이후 자체 구현에서 route 해석과 backend 선택을 분리해야 하는 이유를 설명하는 참고 자료가 된다.

이를 참고했을 때 우리 프로젝트에서는 요청을 받아들이는 부분, 요청 조건을 해석하는 부분, backend 서버를 선택하는 부분을 분리해서 구현하는 방향이 적절하다고 판단하였다. Nginx의 `server`, `location`, `upstream` 구조는 요청 조건과 backend 그룹을 분리해서 관리할 수 있다는 점에서 참고할 수 있고, HAProxy의 `frontend`, `backend` 구조는 요청 수신 지점과 서버 pool을 명확히 나누는 방식이라는 점에서 참고할 수 있다. 따라서 자체 구현에서는 클라이언트 요청을 받은 뒤 host나 path 같은 조건으로 어떤 backend 그룹에 보낼지 결정하고, 그 다음 단계에서 로드밸런싱 알고리즘과 health 상태를 기준으로 실제 backend 서버를 선택하는 흐름을 목표로 잡는 것이 좋다. 이렇게 나누면 route 규칙을 수정하는 작업과 로드밸런싱 알고리즘을 수정하는 작업이 서로 강하게 묶이지 않으므로, 이후 Round Robin, Hash, Least Connections 같은 알고리즘을 추가하거나 backend 상태 확인 기능을 붙일 때 구조를 유지하기 쉽다.

## 다음 주차 계획

3주차에는 Go의 `net/http`와 `net/http/httputil`을 학습하고, 실제 reverse proxy 기능 구현을 시작할 예정이다. 특히 Nginx와 HAProxy 조사에서 확인한 요청 수신, 경로 해석, backend 전달 구조를 참고하여 Go 기반으로 요청을 받아 backend 서버로 전달하는 기본 흐름을 구성한다.

또한 관리자 대시보드 1차 구현과 heartbeat 기능 구현도 함께 진행할 계획이다. 이때 대시보드는 현재 로드밸런서 상태를 확인하는 용도로 두고, heartbeat는 backend 상태를 확인해 이후 로드밸런싱 대상 관리에 활용할 수 있는 기반으로 다룬다.

## 관련 문서

- [NGINX Beginner's Guide](https://nginx.org/en/docs/beginners_guide.html)
- [NGINX Reverse Proxy](https://docs.nginx.com/nginx/admin-guide/web-server/reverse-proxy/)
- [NGINX HTTP Load Balancing](https://docs.nginx.com/nginx/admin-guide/load-balancer/http-load-balancer/)
- [HAProxy Configuration Manual](https://docs.haproxy.org/2.0/configuration.html)
- [HAProxy Frontends](https://www.haproxy.com/documentation/haproxy-configuration-tutorials/proxying-essentials/configuration-basics/frontends/)
- [HAProxy Backends](https://www.haproxy.com/documentation/haproxy-configuration-tutorials/proxying-essentials/configuration-basics/backends/)
