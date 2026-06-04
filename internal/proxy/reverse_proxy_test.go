package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/raftstate"
	"reverseproxy-poc/internal/route"
	"reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/upstream"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
)

func newTestRoute(algorithm string) route.Route {
	return route.Route{
		GlobalID: "default:r-api", LocalID: "r-api", Source: "default",
		Enabled: true, Hosts: []string{"api.example.com"},
		Path:      route.PathMatcher{Kind: route.PathKindPrefix, Value: "/api/"},
		Algorithm: algorithm, UpstreamPool: "default:pool-api",
	}
}

func newRegistry(t *testing.T, raws ...string) *upstream.Registry {
	t.Helper()
	targets := make([]upstream.Target, 0, len(raws))
	for _, raw := range raws {
		targets = append(targets, upstream.Target{Raw: raw})
	}
	registry, err := upstream.NewRegistry([]upstream.Pool{{GlobalID: "default:pool-api", Targets: targets}})
	if err != nil {
		t.Fatalf("upstream.NewRegistry() error = %v", err)
	}
	return registry
}

func newProxyHandler(routes []route.Route, registry *upstream.Registry) http.Handler {
	snapshot := runtime.NewSnapshot(boot.AppConfig{}, raftstate.Identity{}, raftstate.Timing{}, vipruntime.Config{}, nil, routes, registry)
	return NewHandler(runtime.NewState(snapshot))
}

func newProxyHandlerWithConfig(appCfg boot.AppConfig, routes []route.Route, registry *upstream.Registry) http.Handler {
	snapshot := runtime.NewSnapshot(appCfg, raftstate.Identity{NodeID: "node-2"}, raftstate.Timing{}, vipruntime.Config{}, nil, routes, registry)
	return NewHandler(runtime.NewState(snapshot))
}

func requireBody(t *testing.T, body io.Reader) string {
	t.Helper()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	return string(data)
}

func newBackendServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body + r.URL.Path))
	}))
}

func newJSONServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func requireStatusCode(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if got := rec.Result().StatusCode; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func markAllTargetsUnhealthy(t *testing.T, registry *upstream.Registry) {
	t.Helper()
	pool, ok := registry.Get("default:pool-api")
	if !ok {
		t.Fatal("registry.Get() returned no pool")
	}
	now := time.Now()
	pool.SetTargetUnhealthy(0, now, "timeout")
	pool.SetTargetUnhealthy(1, now, "timeout")
}

func stickyBodies(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, httptest.NewRequest(http.MethodGet, "http://api.example.com/api/info", nil))
	cookies := firstRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected sticky cookie to be set")
	}
	secondReq := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/info", nil)
	secondReq.AddCookie(cookies[0])
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)
	return requireBody(t, firstRec.Result().Body), requireBody(t, secondRec.Result().Body)
}

func TestHandlerServeHTTP_ProxiesMatchedRequest(t *testing.T) {
	backend := newBackendServer("backend:")
	defer backend.Close()
	handler := newProxyHandler([]route.Route{newTestRoute("round_robin")}, newRegistry(t, backend.Listener.Addr().String()))
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	requireStatusCode(t, rec, http.StatusOK)
	if got, want := requireBody(t, res.Body), "backend:/api/users"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestHandlerServeHTTP_ForwardsLBNodeHeader(t *testing.T) {
	var gotLBNode string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLBNode = r.Header.Get("X-AjouLB-LB-Node")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	handler := newProxyHandlerWithConfig(boot.AppConfig{}, []route.Route{newTestRoute("round_robin")}, newRegistry(t, backend.Listener.Addr().String()))
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/users", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	requireStatusCode(t, rec, http.StatusOK)
	if gotLBNode != "node-2" {
		t.Fatalf("upstream X-AjouLB-LB-Node = %q, want node-2", gotLBNode)
	}
}

func TestHandlerServeHTTP_ReturnsNotFoundWhenNoRouteMatches(t *testing.T) {
	snapshot := runtime.NewSnapshot(boot.AppConfig{}, raftstate.Identity{}, raftstate.Timing{}, vipruntime.Config{}, nil, nil, nil)
	handler := NewHandler(runtime.NewState(snapshot))

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/users", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := rec.Result().StatusCode, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestNewHandler_UsesTunedTransport(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}))
	proxyHandler, ok := handler.(*Handler)
	if !ok {
		t.Fatalf("handler type = %T, want *Handler", handler)
	}
	if proxyHandler.transport == http.DefaultTransport {
		t.Fatal("transport reused http.DefaultTransport")
	}
	requireTransportDefaults(t, proxyHandler.transport)
}

func TestNewTransport_AppliesConfiguredLimits(t *testing.T) {
	transport := newTransport()
	requireTransportDefaults(t, transport)
}

func TestHandlerServeHTTP_ReturnsBadGatewayWhenNoHealthyTargets(t *testing.T) {
	registry := newRegistry(t, "10.0.0.11:8080", "10.0.0.12:8080")
	markAllTargetsUnhealthy(t, registry)
	handler := newProxyHandler([]route.Route{newTestRoute("round_robin")}, registry)
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	requireStatusCode(t, rec, http.StatusBadGateway)
}

func TestHandlerServeHTTP_StickyCookieSetsCookieAndReusesTarget(t *testing.T) {
	serverA := newJSONServer(`{"server":"a"}`)
	defer serverA.Close()
	serverB := newJSONServer(`{"server":"b"}`)
	defer serverB.Close()
	registry := newRegistry(t, serverA.Listener.Addr().String(), serverB.Listener.Addr().String())
	handler := newProxyHandler([]route.Route{newTestRoute("sticky_cookie")}, registry)
	firstBody, secondBody := stickyBodies(t, handler)
	if firstBody != secondBody {
		t.Fatalf("sticky response mismatch: %q != %q", firstBody, secondBody)
	}
}

func newFiveTupleHandler(t *testing.T) http.Handler {
	t.Helper()
	serverA := newJSONServer(`{"server":"a"}`)
	t.Cleanup(serverA.Close)
	serverB := newJSONServer(`{"server":"b"}`)
	t.Cleanup(serverB.Close)
	registry := newRegistry(t, serverA.Listener.Addr().String(), serverB.Listener.Addr().String())
	return newProxyHandler([]route.Route{newTestRoute("5_tuple_hash")}, registry)
}

func newFiveTupleRequest(remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/info", nil)
	req.RemoteAddr = remoteAddr
	return req
}

func responseBody(t *testing.T, handler http.Handler, req *http.Request) string {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return requireBody(t, rec.Result().Body)
}

func TestHandlerServeHTTP_FiveTupleHashReusesTargetFromForwardedHeaders(t *testing.T) {
	handler := newFiveTupleHandler(t)
	firstReq := newFiveTupleRequest("10.0.0.20:34567")
	firstReq.Header.Set("X-Forwarded-For", "203.0.113.10")
	secondReq := newFiveTupleRequest("10.0.0.30:45678")
	secondReq.Header.Set("X-Forwarded-For", "203.0.113.10")
	requireSameBody(t, handler, firstReq, secondReq, "5_tuple_hash response")
}

func TestHandlerServeHTTP_FiveTupleHashFallsBackToRemoteAddr(t *testing.T) {
	handler := newFiveTupleHandler(t)
	req := newFiveTupleRequest("203.0.113.10:34567")
	requireSameBody(t, handler, req.Clone(req.Context()), req, "5_tuple_hash remote addr")
}

func TestHandlerServeHTTP_FiveTupleHashSkipsUnhealthyTargets(t *testing.T) {
	registry := fiveTupleRegistry(t)
	markTargetUnhealthy(t, registry, 0, "down")
	handler := newProxyHandler([]route.Route{newTestRoute("5_tuple_hash")}, registry)
	req := newFiveTupleRequest("")
	req.Header.Set("Forwarded", "for=203.0.113.10")
	requireBodyEquals(t, handler, req, `{"server":"b"}`)
}

func TestHandlerServeHTTP_LeastConnectionAvoidsBusyTarget(t *testing.T) {
	handler, started, release := newLeastConnectionBusyHandler(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go serveBlockedRequest(handler, newLeastConnectionRequest(), &wg)
	<-started
	requireBodyEquals(t, handler, newLeastConnectionRequest(), `{"server":"fast"}`)
	close(release)
	wg.Wait()
}

func TestHandlerServeHTTP_LeastConnectionReleasesAfterProxyReturns(t *testing.T) {
	server := newJSONServer(`{"server":"only"}`)
	defer server.Close()
	registry := newRegistry(t, server.Listener.Addr().String())
	handler := newProxyHandler([]route.Route{newTestRoute("least_connection")}, registry)
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/info", nil)
	requireBodyEquals(t, handler, req, `{"server":"only"}`)
	pool, ok := registry.Get("default:pool-api")
	if !ok {
		t.Fatal("registry.Get() returned no pool")
	}
	if got, want := pool.ActiveConnections(0), uint64(0); got != want {
		t.Fatalf("ActiveConnections() = %d, want %d", got, want)
	}
}

func TestFindHealthyTarget_ReturnsFalseForUnhealthyTarget(t *testing.T) {
	pool := &upstream.Pool{
		Targets: []upstream.Target{
			{Raw: "127.0.0.1:18081"},
			{Raw: "127.0.0.1:18082"},
		},
	}
	pool.SetTargetUnhealthy(1, time.Now(), "failed")

	if _, ok := findHealthyTarget(pool, "127.0.0.1:18082"); ok {
		t.Fatal("findHealthyTarget() returned unhealthy target")
	}
}

func TestTrustedClientAddress_PrefersForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/info", nil)
	req.RemoteAddr = "10.0.0.20:34567"
	req.Header.Set("Forwarded", "for=198.51.100.2:1234")

	host, port := trustedClientAddress(req)

	if host != "198.51.100.2" || port != "" {
		t.Fatalf("trustedClientAddress() = %q, %q", host, port)
	}
}

func requireSameBody(t *testing.T, handler http.Handler, firstReq, secondReq *http.Request, label string) {
	t.Helper()
	firstBody := responseBody(t, handler, firstReq)
	secondBody := responseBody(t, handler, secondReq)
	if firstBody != secondBody {
		t.Fatalf("%s mismatch: %q != %q", label, firstBody, secondBody)
	}
}

func requireBodyEquals(t *testing.T, handler http.Handler, req *http.Request, want string) {
	t.Helper()
	if got := responseBody(t, handler, req); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func requireTransportDefaults(t *testing.T, transport *http.Transport) {
	t.Helper()
	cfg := defaultTransportConfig()
	requireTransportConnectionLimits(t, transport, cfg)
	requireTransportTimeouts(t, transport, cfg)
}

func requireTransportConnectionLimits(t *testing.T, transport *http.Transport, cfg transportConfig) {
	t.Helper()
	if transport.MaxIdleConns != cfg.maxIdleConns {
		t.Fatalf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, cfg.maxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != cfg.maxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, cfg.maxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != cfg.maxConnsPerHost {
		t.Fatalf("MaxConnsPerHost = %d, want %d", transport.MaxConnsPerHost, cfg.maxConnsPerHost)
	}
}

func requireTransportTimeouts(t *testing.T, transport *http.Transport, cfg transportConfig) {
	t.Helper()
	if transport.IdleConnTimeout != cfg.idleConnTimeout {
		t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, cfg.idleConnTimeout)
	}
	if transport.ResponseHeaderTimeout != cfg.responseHeaderWait {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, cfg.responseHeaderWait)
	}
	if transport.DialContext == nil {
		t.Fatal("DialContext is nil")
	}
}

func fiveTupleRegistry(t *testing.T) *upstream.Registry {
	t.Helper()
	serverA := newJSONServer(`{"server":"a"}`)
	t.Cleanup(serverA.Close)
	serverB := newJSONServer(`{"server":"b"}`)
	t.Cleanup(serverB.Close)
	return newRegistry(t, serverA.Listener.Addr().String(), serverB.Listener.Addr().String())
}

func markTargetUnhealthy(t *testing.T, registry *upstream.Registry, index int, reason string) {
	t.Helper()
	pool, ok := registry.Get("default:pool-api")
	if !ok {
		t.Fatal("registry.Get() returned no pool")
	}
	pool.SetTargetUnhealthy(index, time.Now(), reason)
}

func blockingServer(name string) (*httptest.Server, chan struct{}, chan struct{}) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"server":"` + name + `"}`))
	})
	return httptest.NewServer(handler), started, release
}

func newLeastConnectionBusyHandler(t *testing.T) (http.Handler, chan struct{}, chan struct{}) {
	t.Helper()
	blocked, started, release := blockingServer("slow")
	t.Cleanup(blocked.Close)
	fast := newJSONServer(`{"server":"fast"}`)
	t.Cleanup(fast.Close)
	registry := newRegistry(t, blocked.Listener.Addr().String(), fast.Listener.Addr().String())
	return newProxyHandler([]route.Route{newTestRoute("least_connection")}, registry), started, release
}

func newLeastConnectionRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://api.example.com/api/info", nil)
}

func serveBlockedRequest(handler http.Handler, req *http.Request, wg *sync.WaitGroup) {
	defer wg.Done()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}
