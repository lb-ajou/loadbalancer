package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reverseproxy-poc/internal/admin"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/route"
	"reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/state"
	"reverseproxy-poc/internal/upstream"
)

type stubService struct {
	listNamespacesFn         func() ([]admin.NamespaceView, error)
	createNamespaceFn        func(namespace string) (admin.NamespaceView, error)
	deleteNamespaceFn        func(namespace string) error
	getNamespaceConfigFn     func(namespace string) (admin.NamespaceConfigView, error)
	replaceNamespaceConfigFn func(namespace string, cfg spec.Config) (admin.NamespaceConfigView, error)
	getNamespaceRoutesFn     func(namespace string) ([]spec.RouteConfig, error)
	createRouteFn            func(namespace string, route spec.RouteConfig) (spec.RouteConfig, error)
	updateRouteFn            func(namespace, id string, route spec.RouteConfig) (spec.RouteConfig, error)
	deleteRouteFn            func(namespace, id string) error
	getNamespaceUpstreamsFn  func(namespace string) (map[string]spec.UpstreamPool, error)
	createUpstreamPoolFn     func(namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error)
	updateUpstreamPoolFn     func(namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error)
	deleteUpstreamPoolFn     func(namespace, id string) error
}

type stubJoiner struct {
	joinFn func(ctx context.Context, nodeID, raftAddress string) error
}

type stubClusterProvider struct {
	view ClusterView
}

type stubVIPProvider struct {
	view VIPStatusView
}

type stubLifecycle struct {
	status      NodeClusterStatusView
	bootstrapFn func(context.Context, ClusterBootstrapRequest) error
	joinFn      func(context.Context, NodeJoinClusterRequest) error
}

func (j stubJoiner) JoinRaft(ctx context.Context, nodeID, raftAddress string) error {
	if j.joinFn != nil {
		return j.joinFn(ctx, nodeID, raftAddress)
	}
	return nil
}

func (p stubClusterProvider) ClusterStatus(context.Context) ClusterView {
	return p.view
}

func (p stubVIPProvider) VIPStatus() VIPStatusView {
	return p.view
}

func (s stubLifecycle) BootstrapCluster(ctx context.Context, request ClusterBootstrapRequest) error {
	if s.bootstrapFn != nil {
		return s.bootstrapFn(ctx, request)
	}
	return nil
}

func (s stubLifecycle) JoinCluster(ctx context.Context, request NodeJoinClusterRequest) error {
	if s.joinFn != nil {
		return s.joinFn(ctx, request)
	}
	return nil
}

func (s stubLifecycle) ClusterLifecycleStatus(context.Context) NodeClusterStatusView {
	return s.status
}

func (s stubService) ListNamespaces(_ context.Context) ([]admin.NamespaceView, error) {
	if s.listNamespacesFn != nil {
		return s.listNamespacesFn()
	}
	return nil, nil
}

func (s stubService) CreateNamespace(_ context.Context, namespace string) (admin.NamespaceView, error) {
	if s.createNamespaceFn != nil {
		return s.createNamespaceFn(namespace)
	}
	return admin.NamespaceView{}, nil
}

func (s stubService) DeleteNamespace(_ context.Context, namespace string) error {
	if s.deleteNamespaceFn != nil {
		return s.deleteNamespaceFn(namespace)
	}
	return nil
}

func (s stubService) GetNamespaceConfig(_ context.Context, namespace string) (admin.NamespaceConfigView, error) {
	if s.getNamespaceConfigFn != nil {
		return s.getNamespaceConfigFn(namespace)
	}
	return admin.NamespaceConfigView{}, nil
}

func (s stubService) ReplaceNamespaceConfig(_ context.Context, namespace string, cfg spec.Config) (admin.NamespaceConfigView, error) {
	if s.replaceNamespaceConfigFn != nil {
		return s.replaceNamespaceConfigFn(namespace, cfg)
	}
	return admin.NamespaceConfigView{}, nil
}

func (s stubService) GetNamespaceRoutes(_ context.Context, namespace string) ([]spec.RouteConfig, error) {
	if s.getNamespaceRoutesFn != nil {
		return s.getNamespaceRoutesFn(namespace)
	}
	return nil, nil
}

func (s stubService) CreateRoute(_ context.Context, namespace string, route spec.RouteConfig) (spec.RouteConfig, error) {
	if s.createRouteFn != nil {
		return s.createRouteFn(namespace, route)
	}
	return spec.RouteConfig{}, nil
}

func (s stubService) UpdateRoute(_ context.Context, namespace, id string, route spec.RouteConfig) (spec.RouteConfig, error) {
	if s.updateRouteFn != nil {
		return s.updateRouteFn(namespace, id, route)
	}
	return spec.RouteConfig{}, nil
}

func (s stubService) DeleteRoute(_ context.Context, namespace, id string) error {
	if s.deleteRouteFn != nil {
		return s.deleteRouteFn(namespace, id)
	}
	return nil
}

func (s stubService) GetNamespaceUpstreamPools(_ context.Context, namespace string) (map[string]spec.UpstreamPool, error) {
	if s.getNamespaceUpstreamsFn != nil {
		return s.getNamespaceUpstreamsFn(namespace)
	}
	return nil, nil
}

func (s stubService) CreateUpstreamPool(_ context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error) {
	if s.createUpstreamPoolFn != nil {
		return s.createUpstreamPoolFn(namespace, id, pool)
	}
	return spec.UpstreamPool{}, nil
}

func (s stubService) UpdateUpstreamPool(_ context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error) {
	if s.updateUpstreamPoolFn != nil {
		return s.updateUpstreamPoolFn(namespace, id, pool)
	}
	return spec.UpstreamPool{}, nil
}

func (s stubService) DeleteUpstreamPool(_ context.Context, namespace, id string) error {
	if s.deleteUpstreamPoolFn != nil {
		return s.deleteUpstreamPoolFn(namespace, id)
	}
	return nil
}

func performDashboardRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("json decode error = %v", err)
	}
}

func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if got := rec.Result().StatusCode; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func defaultNamespaceConfig() admin.NamespaceConfigView {
	return admin.NamespaceConfigView{
		Namespace: admin.DefaultNamespace,
		Exists:    true,
		Routes: []spec.RouteConfig{{
			ID: "r-api", Enabled: true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "pool-api",
		}},
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
		},
	}
}

func stickyRouteSnapshot() runtime.Snapshot {
	return runtime.Snapshot{
		RouteTable: []route.Route{{
			GlobalID: "default:r-api", LocalID: "r-api", Source: "default",
			Enabled: true, Hosts: []string{"api.example.com"},
			Path:      route.PathMatcher{Kind: route.PathKindPrefix, Value: "/"},
			Algorithm: "sticky_cookie", UpstreamPool: "default:pool-api",
		}},
	}
}

func runtimeEndpointSnapshot(t *testing.T) runtime.Snapshot {
	t.Helper()
	registry, err := upstream.NewRegistry([]upstream.Pool{{
		GlobalID:    "default:pool-api",
		LocalID:     "pool-api",
		Source:      "default",
		Targets:     []upstream.Target{{Raw: "10.0.0.11:8080"}},
		HealthCheck: &upstream.HealthCheck{Path: "/health", Interval: "30s", Timeout: "3s", ExpectStatus: 200},
	}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	pool, ok := registry.Get("default:pool-api")
	if !ok {
		t.Fatal("registry.Get(default:pool-api) returned no pool")
	}
	pool.SetTargetUnhealthy(0, time.Unix(1700000100, 0).UTC(), "unexpected status: 503")
	return runtime.Snapshot{
		RaftIdentity: config.RaftIdentity{NodeID: "node-1"},
		ProxyConfigs: []spec.LoadedConfig{{
			Source: "default",
			Path:   "raft://namespaces/default",
			Config: spec.Config{
				Name: "default",
				Routes: []spec.RouteConfig{{
					ID:           "r-api",
					Enabled:      true,
					Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
					UpstreamPool: "pool-api",
				}},
				UpstreamPools: map[string]spec.UpstreamPool{
					"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
				},
			},
		}},
		RouteTable: []route.Route{{
			GlobalID: "default:r-api", LocalID: "r-api", Source: "default",
			Enabled: true, Hosts: []string{"api.example.com"},
			Path:         route.PathMatcher{Kind: route.PathKindPrefix, Value: "/api/"},
			Algorithm:    "round_robin",
			UpstreamPool: "default:pool-api",
		}},
		Upstreams: registry,
		AppliedAt: time.Unix(1700000000, 0).UTC(),
	}
}

func namespacedConfigHandler(t *testing.T) http.Handler {
	return NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{
		getNamespaceConfigFn: func(namespace string) (admin.NamespaceConfigView, error) {
			requireDefaultNamespace(t, namespace)
			return defaultNamespaceConfig(), nil
		},
	})
}

func createNamespaceHandler(t *testing.T) http.Handler {
	return NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{
		createNamespaceFn: func(namespace string) (admin.NamespaceView, error) {
			requireNamespace(t, namespace, "admin")
			return admin.NamespaceView{Namespace: namespace, Path: "raft://namespaces/admin", Exists: true}, nil
		},
	})
}

func validationErrorHandler() http.Handler {
	return NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{
		replaceNamespaceConfigFn: func(string, spec.Config) (admin.NamespaceConfigView, error) {
			return admin.NamespaceConfigView{}, duplicateRouteAPIError()
		},
	})
}

func requireDefaultNamespace(t *testing.T, namespace string) {
	requireNamespace(t, namespace, admin.DefaultNamespace)
}

func requireNamespace(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("namespace = %q, want %q", got, want)
	}
}

func requireRouteID(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("route.ID = %q, want %q", got, want)
	}
}

func requireNamespaceConfigCounts(t *testing.T, body admin.NamespaceConfigView) {
	t.Helper()
	requireNamespace(t, body.Namespace, admin.DefaultNamespace)
	requireCount(t, "Routes", len(body.Routes), 1)
	requireCount(t, "UpstreamPools", len(body.UpstreamPools), 1)
}

func requireCount(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("len(%s) = %d, want %d", name, got, want)
	}
}

func duplicateRouteAPIError() *admin.APIError {
	return &admin.APIError{
		StatusCode: http.StatusUnprocessableEntity,
		Message:    "validation failed",
		ValidationErrors: []spec.ValidationError{
			{Field: "routes[0].id", Message: "duplicate route id"},
		},
	}
}

func TestNamespacedConfigEndpoint_ReturnsEditableConfig(t *testing.T) {
	rec := performDashboardRequest(namespacedConfigHandler(t), http.MethodGet, "/api/namespaces/default/config", "")
	requireStatus(t, rec, http.StatusOK)
	var body admin.NamespaceConfigView
	decodeJSON(t, rec, &body)
	requireNamespaceConfigCounts(t, body)
}

func TestReplaceNamespacedConfigEndpoint_ReplacesEditableConfig(t *testing.T) {
	var gotConfig spec.Config
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{
		replaceNamespaceConfigFn: func(namespace string, cfg spec.Config) (admin.NamespaceConfigView, error) {
			requireDefaultNamespace(t, namespace)
			gotConfig = cfg
			return admin.NamespaceConfigView{
				Namespace:     namespace,
				Exists:        true,
				Routes:        cfg.Routes,
				UpstreamPools: cfg.UpstreamPools,
			}, nil
		},
	})
	body := `{
		"routes":[{"id":"r-api","enabled":true,"match":{"hosts":["api.example.com"]},"upstream_pool":"pool-api"}],
		"upstream_pools":{"pool-api":{"upstreams":["10.0.0.11:8080"]}}
	}`

	rec := performDashboardRequest(handler, http.MethodPut, "/api/namespaces/default/config", body)
	requireStatus(t, rec, http.StatusOK)
	if got, want := len(gotConfig.Routes), 1; got != want {
		t.Fatalf("len(gotConfig.Routes) = %d, want %d", got, want)
	}
	var response admin.NamespaceConfigView
	decodeJSON(t, rec, &response)
	requireNamespace(t, response.Namespace, admin.DefaultNamespace)
	requireCount(t, "UpstreamPools", len(response.UpstreamPools), 1)
}

func TestRuntimeConfigEndpoint_ExposesRouteAlgorithm(t *testing.T) {
	handler := NewHandler(runtime.NewState(stickyRouteSnapshot()), stubService{})
	rec := performDashboardRequest(handler, http.MethodGet, "/api/runtime", "")
	requireStatus(t, rec, http.StatusOK)
	var body RuntimeView
	decodeJSON(t, rec, &body)
	if len(body.Routes) != 1 {
		t.Fatalf("len(Routes) = %d, want 1", len(body.Routes))
	}
	if got, want := body.Routes[0].Algorithm, "sticky_cookie"; got != want {
		t.Fatalf("Algorithm = %q, want %q", got, want)
	}
}

func TestRuntimeEndpoint_ReturnsConsolidatedRuntimeView(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtimeEndpointSnapshot(t)), stubService{})
	rec := performDashboardRequest(handler, http.MethodGet, "/api/runtime", "")
	requireStatus(t, rec, http.StatusOK)
	var body RuntimeView
	decodeJSON(t, rec, &body)
	if got, want := body.Node.ID, "node-1"; got != want {
		t.Fatalf("Node.ID = %q, want %q", got, want)
	}
	requireCount(t, "Routes", len(body.Routes), 1)
	requireCount(t, "Upstreams", len(body.Upstreams), 1)
	requireCount(t, "Targets", len(body.Upstreams[0].Targets), 1)
	target := body.Upstreams[0].Targets[0]
	if target.Address != "10.0.0.11:8080" || target.Healthy {
		t.Fatalf("target = %+v, want unhealthy 10.0.0.11:8080", target)
	}
	if target.LastCheckedAt == nil || target.LastError == "" {
		t.Fatalf("target = %+v, want health detail", target)
	}
}

func TestRuntimeEndpointReturnsRaftConfigStore(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{})
	rec := performDashboardRequest(handler, http.MethodGet, "/api/runtime", "")
	requireStatus(t, rec, http.StatusOK)
	var body RuntimeView
	decodeJSON(t, rec, &body)
	if got := body.Node.ConfigStore; got != legacyConfigStoreValue {
		t.Fatalf("ConfigStore = %q, want raft", got)
	}
}

func TestStatusEndpointReturnsRaftConfigStore(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{})
	rec := performDashboardRequest(handler, http.MethodGet, "/api/status", "")
	requireStatus(t, rec, http.StatusOK)
	var body StatusView
	decodeJSON(t, rec, &body)
	if got := body.Node.ConfigStore; got != legacyConfigStoreValue {
		t.Fatalf("ConfigStore = %q, want raft", got)
	}
}

func TestStatusEndpoint_ReturnsNodeClusterAndRuntimeSummary(t *testing.T) {
	cluster := ClusterView{
		Enabled: true,
		Leader:  ClusterLeaderView{ID: "node-1", Address: "127.0.0.1:7001"},
		Local:   ClusterLocalView{ID: "node-1", State: "leader"},
	}
	vip := VIPStatusView{Enabled: true, Interface: "eth0", Address: "10.0.0.100/24", Owned: true}
	handler := NewHandlerWithProviders(
		runtime.NewState(runtimeEndpointSnapshot(t)),
		stubService{},
		nil,
		stubClusterProvider{view: cluster},
		stubVIPProvider{view: vip},
		nil,
	)

	rec := performDashboardRequest(handler, http.MethodGet, "/api/status", "")
	requireStatus(t, rec, http.StatusOK)
	var body StatusView
	decodeJSON(t, rec, &body)
	if !body.Raft.Enabled || !body.Raft.IsLeader || body.Raft.LeaderAddress != "127.0.0.1:7001" {
		t.Fatalf("Raft = %+v, want leader status", body.Raft)
	}
	if !body.VIP.Owned {
		t.Fatalf("VIP = %+v, want owned", body.VIP)
	}
	if got, want := body.Runtime.UnhealthyTargetCount, 1; got != want {
		t.Fatalf("UnhealthyTargetCount = %d, want %d", got, want)
	}
}

func TestClusterEndpoint_ReturnsProviderView(t *testing.T) {
	cluster := ClusterView{
		Enabled: true,
		Leader:  ClusterLeaderView{ID: "node-1", Address: "127.0.0.1:7001"},
		Local:   ClusterLocalView{ID: "node-2", State: "follower"},
		Members: []ClusterMemberView{{ID: "node-1", Address: "127.0.0.1:7001", Role: "voter", IsLeader: true}},
		RaftTiming: &ClusterRaftTimingView{
			HeartbeatTimeout: "3s",
			ElectionTimeout:  "5s",
		},
	}
	handler := NewHandlerWithProviders(
		runtime.NewState(runtime.Snapshot{}),
		stubService{},
		nil,
		stubClusterProvider{view: cluster},
		nil,
		nil,
	)

	rec := performDashboardRequest(handler, http.MethodGet, "/api/cluster", "")
	requireStatus(t, rec, http.StatusOK)
	var body ClusterView
	decodeJSON(t, rec, &body)
	if body.Leader.ID != "node-1" || len(body.Members) != 1 {
		t.Fatalf("ClusterView = %+v, want leader and member", body)
	}
	if body.RaftTiming == nil || body.RaftTiming.HeartbeatTimeout != "3s" {
		t.Fatalf("ClusterView.RaftTiming = %+v", body.RaftTiming)
	}
}

func TestCreateNamespaceEndpoint_CreatesNamespace(t *testing.T) {
	rec := performDashboardRequest(createNamespaceHandler(t), http.MethodPost, "/api/namespaces", `{"namespace":"admin"}`)
	requireStatus(t, rec, http.StatusCreated)
	var body admin.NamespaceView
	decodeJSON(t, rec, &body)
	requireNamespace(t, body.Namespace, "admin")
}

func TestConfigAPI_NotLeaderErrorIncludesLeaderAddress(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{
		createNamespaceFn: func(string) (admin.NamespaceView, error) {
			return admin.NamespaceView{}, &admin.APIError{
				StatusCode: http.StatusConflict,
				Message:    "configuration writes must be sent to the raft leader",
				Err:        state.NewNotLeaderError("127.0.0.1:9090"),
			}
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/namespaces", strings.NewReader(`{"namespace":"admin"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_raft_leader"`) {
		t.Fatalf("response body = %s, want not_raft_leader code", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"leader_address":"127.0.0.1:9090"`) {
		t.Fatalf("response body = %s, want leader_address", rec.Body.String())
	}
}

func TestClusterJoinEndpoint_CallsJoiner(t *testing.T) {
	var gotNodeID, gotRaftAddress string
	handler := NewHandlerWithRaft(runtime.NewState(runtime.Snapshot{}), stubService{}, stubJoiner{
		joinFn: func(_ context.Context, nodeID, raftAddress string) error {
			gotNodeID = nodeID
			gotRaftAddress = raftAddress
			return nil
		},
	})

	rec := performDashboardRequest(handler, http.MethodPost, "/api/cluster/join", `{"node_id":"node-2","raft_address":"127.0.0.1:7002"}`)
	requireStatus(t, rec, http.StatusNoContent)
	if gotNodeID != "node-2" || gotRaftAddress != "127.0.0.1:7002" {
		t.Fatalf("JoinRaft called with nodeID=%q raftAddress=%q, want node-2/127.0.0.1:7002", gotNodeID, gotRaftAddress)
	}
}

func TestClusterJoinEndpoint_RejectsInvalidRequestBeforeJoiner(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid node id", body: `{"node_id":"bad:node","raft_address":"127.0.0.1:7002"}`},
		{name: "invalid raft address", body: `{"node_id":"node-2","raft_address":"not-a-host-port"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joinCalls := 0
			handler := NewHandlerWithRaft(runtime.NewState(runtime.Snapshot{}), stubService{}, stubJoiner{
				joinFn: func(context.Context, string, string) error {
					joinCalls++
					return nil
				},
			})

			rec := performDashboardRequest(handler, http.MethodPost, "/api/cluster/join", tt.body)
			requireStatus(t, rec, http.StatusBadRequest)
			if joinCalls != 0 {
				t.Fatalf("JoinRaft calls = %d, want 0", joinCalls)
			}
		})
	}
}

func TestClusterJoinEndpoint_MapsNotLeaderError(t *testing.T) {
	handler := NewHandlerWithRaft(runtime.NewState(runtime.Snapshot{}), stubService{}, stubJoiner{
		joinFn: func(context.Context, string, string) error {
			return state.NewNotLeaderError("127.0.0.1:9090")
		},
	})

	rec := performDashboardRequest(handler, http.MethodPost, "/api/cluster/join", `{"node_id":"node-2","raft_address":"127.0.0.1:7002"}`)
	requireStatus(t, rec, http.StatusConflict)
	var body errorResponse
	decodeJSON(t, rec, &body)
	if body.Code != "not_raft_leader" || body.LeaderAddress != "127.0.0.1:9090" {
		t.Fatalf("error body = %+v, want not_raft_leader with leader address", body)
	}
}

func TestClusterBootstrapEndpoint_CallsLifecycle(t *testing.T) {
	var got ClusterBootstrapRequest
	handler := NewHandlerWithProviders(
		runtime.NewState(runtime.Snapshot{}),
		stubService{},
		nil,
		nil,
		nil,
		stubLifecycle{bootstrapFn: func(_ context.Context, request ClusterBootstrapRequest) error {
			got = request
			return nil
		}},
	)

	body := `{"node_id":"node-1","raft_advertise_addr":"127.0.0.1:7001","raft_timing":{"heartbeat_timeout":"3s","election_timeout":"5s"},"vip_interface":"eth0","vip":{"address":"10.0.0.100/24"}}`
	rec := performDashboardRequest(handler, http.MethodPost, "/api/cluster/bootstrap", body)
	requireStatus(t, rec, http.StatusNoContent)
	if got.NodeID != "node-1" || got.VIP == nil || got.VIP.Address != "10.0.0.100/24" {
		t.Fatalf("bootstrap request = %+v", got)
	}
	if got.RaftTiming == nil || got.RaftTiming.HeartbeatTimeout != "3s" {
		t.Fatalf("bootstrap raft timing = %+v", got.RaftTiming)
	}
}

func TestClusterBootstrapEndpoint_RejectsInvalidRaftTiming(t *testing.T) {
	handler := NewHandlerWithProviders(
		runtime.NewState(runtime.Snapshot{}),
		stubService{},
		nil,
		nil,
		nil,
		stubLifecycle{},
	)

	body := `{"node_id":"node-1","raft_advertise_addr":"127.0.0.1:7001","raft_timing":{"heartbeat_timeout":"5s","election_timeout":"3s"}}`
	rec := performDashboardRequest(handler, http.MethodPost, "/api/cluster/bootstrap", body)
	requireStatus(t, rec, http.StatusBadRequest)
	var errBody errorResponse
	decodeJSON(t, rec, &errBody)
	if errBody.Code != "invalid_raft_timing" {
		t.Fatalf("error body = %+v, want invalid_raft_timing", errBody)
	}
}

func TestClusterBootstrapEndpoint_RejectsInvalidVIP(t *testing.T) {
	handler := NewHandlerWithProviders(
		runtime.NewState(runtime.Snapshot{}),
		stubService{},
		nil,
		nil,
		nil,
		stubLifecycle{},
	)

	body := `{"node_id":"node-1","raft_advertise_addr":"127.0.0.1:7001","vip_interface":"eth0","vip":{"address":"bad"}}`
	rec := performDashboardRequest(handler, http.MethodPost, "/api/cluster/bootstrap", body)
	requireStatus(t, rec, http.StatusBadRequest)
	var errBody errorResponse
	decodeJSON(t, rec, &errBody)
	if errBody.Code != "invalid_vip" {
		t.Fatalf("error body = %+v, want invalid_vip", errBody)
	}
}

func TestNodeClusterStatusEndpoint_ReturnsLifecycleStatus(t *testing.T) {
	handler := NewHandlerWithProviders(
		runtime.NewState(runtime.Snapshot{}),
		stubService{},
		nil,
		nil,
		nil,
		stubLifecycle{status: NodeClusterStatusView{
			State:        "unconfigured",
			CanBootstrap: true,
			CanJoin:      true,
		}},
	)

	rec := performDashboardRequest(handler, http.MethodGet, "/api/node/cluster-status", "")
	requireStatus(t, rec, http.StatusOK)
	var body NodeClusterStatusView
	decodeJSON(t, rec, &body)
	if body.State != "unconfigured" || !body.CanBootstrap || !body.CanJoin {
		t.Fatalf("status body = %+v, want unconfigured actions", body)
	}
}

func TestNodeJoinClusterEndpoint_CallsLifecycle(t *testing.T) {
	var got NodeJoinClusterRequest
	handler := NewHandlerWithProviders(
		runtime.NewState(runtime.Snapshot{}),
		stubService{},
		nil,
		nil,
		nil,
		stubLifecycle{joinFn: func(_ context.Context, request NodeJoinClusterRequest) error {
			got = request
			return nil
		}},
	)

	body := `{"node_id":"node-2","raft_advertise_addr":"127.0.0.1:7002","vip_interface":"eth0","peers":["http://node-1:9090"]}`
	rec := performDashboardRequest(handler, http.MethodPost, "/api/node/join-cluster", body)
	requireStatus(t, rec, http.StatusNoContent)
	if got.NodeID != "node-2" || len(got.Peers) != 1 || got.Peers[0] != "http://node-1:9090" {
		t.Fatalf("join request = %+v", got)
	}
}

func TestDeleteNamespaceEndpoint_DeletesNamespace(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{
		deleteNamespaceFn: func(namespace string) error {
			if namespace != "admin" {
				t.Fatalf("namespace = %q, want %q", namespace, "admin")
			}
			return nil
		},
	})

	rec := performDashboardRequest(handler, http.MethodDelete, "/api/namespaces/admin", "")
	requireStatus(t, rec, http.StatusNoContent)
}

func TestValidationError_ReturnsStructuredErrorBody(t *testing.T) {
	requestBody := `{
		"routes":[{"id":"r-api","enabled":true,"match":{"hosts":["api.example.com"]},"upstream_pool":"pool-api"}],
		"upstream_pools":{"pool-api":{"upstreams":["10.0.0.11:8080"]}}
	}`
	rec := performDashboardRequest(validationErrorHandler(), http.MethodPut, "/api/namespaces/default/config", requestBody)
	requireStatus(t, rec, http.StatusUnprocessableEntity)
	var body admin.APIError
	decodeJSON(t, rec, &body)
	if got, want := body.Message, "validation failed"; got != want {
		t.Fatalf("Message = %q, want %q", got, want)
	}
	if got, want := len(body.ValidationErrors), 1; got != want {
		t.Fatalf("len(ValidationErrors) = %d, want %d", got, want)
	}
}

func TestRemovedAPIEndpoints_ReturnNotFound(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{})
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/config"},
		{http.MethodGet, "/api/runtime/config"},
		{http.MethodGet, "/api/app-config"},
		{http.MethodGet, "/api/proxy-configs"},
		{http.MethodGet, "/api/runtime/routes"},
		{http.MethodGet, "/api/upstreams"},
		{http.MethodPost, "/api/raft/join"},
		{http.MethodGet, "/api/namespaces/default/routes"},
		{http.MethodPost, "/api/namespaces/default/routes"},
		{http.MethodPut, "/api/namespaces/default/routes/r-api"},
		{http.MethodDelete, "/api/namespaces/default/routes/r-api"},
		{http.MethodGet, "/api/namespaces/default/upstream-pools"},
		{http.MethodPost, "/api/namespaces/default/upstream-pools"},
		{http.MethodPut, "/api/namespaces/default/upstream-pools/pool-api"},
		{http.MethodDelete, "/api/namespaces/default/upstream-pools/pool-api"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := performDashboardRequest(handler, tt.method, tt.path, `{}`)
			requireStatus(t, rec, http.StatusNotFound)
		})
	}
}

func TestSPAPath_ReturnsDashboardHTML(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{})
	rec := performDashboardRequest(handler, http.MethodGet, "/routes", "")
	requireStatus(t, rec, http.StatusOK)
	if got := rec.Result().Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<!doctype html>") {
		t.Fatalf("body did not contain HTML document")
	}
}

func TestClusterLifecyclePage_ReturnsLifecycleHTML(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{})
	rec := performDashboardRequest(handler, http.MethodGet, "/cluster-lifecycle", "")
	requireStatus(t, rec, http.StatusOK)
	if got := rec.Result().Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Cluster Lifecycle") {
		t.Fatalf("body did not contain lifecycle page title")
	}
	if !strings.Contains(body, "/api/node/cluster-status") {
		t.Fatalf("body did not contain cluster status API call")
	}
}

func TestUnknownAPIPath_ReturnsNotFound(t *testing.T) {
	handler := NewHandler(runtime.NewState(runtime.Snapshot{}), stubService{})
	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := rec.Result().StatusCode, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}
