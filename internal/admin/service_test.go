package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/raft"
	appruntime "reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/state"
)

type testRuntime struct {
	appCfg boot.AppConfig
}

func (r *testRuntime) Snapshot() appruntime.Snapshot {
	return appruntime.Snapshot{AppConfig: r.appCfg}
}

type stubStateStore struct {
	listCalled      bool
	replaceCalled   bool
	namespaces      []state.NamespaceSummary
	namespaceConfig state.NamespaceConfig
}

func (s *stubStateStore) ListNamespaces(context.Context) ([]state.NamespaceSummary, error) {
	s.listCalled = true
	return s.namespaces, nil
}

func (s *stubStateStore) GetNamespaceConfig(context.Context, string) (state.NamespaceConfig, error) {
	return s.namespaceConfig, nil
}
func (s *stubStateStore) ReplaceNamespaceConfig(context.Context, string, spec.Config) (state.NamespaceConfig, error) {
	s.replaceCalled = true
	return s.namespaceConfig, nil
}
func (s *stubStateStore) CreateNamespace(context.Context, string) (state.NamespaceSummary, error) {
	return state.NamespaceSummary{}, nil
}
func (s *stubStateStore) DeleteNamespace(context.Context, string) error { return nil }
func (s *stubStateStore) CreateRoute(context.Context, string, spec.RouteConfig) (spec.RouteConfig, error) {
	return spec.RouteConfig{}, nil
}
func (s *stubStateStore) UpdateRoute(context.Context, string, string, spec.RouteConfig) (spec.RouteConfig, error) {
	return spec.RouteConfig{}, nil
}
func (s *stubStateStore) DeleteRoute(context.Context, string, string) error { return nil }
func (s *stubStateStore) CreateUpstreamPool(context.Context, string, string, spec.UpstreamPool) (spec.UpstreamPool, error) {
	return spec.UpstreamPool{}, nil
}
func (s *stubStateStore) UpdateUpstreamPool(context.Context, string, string, spec.UpstreamPool) (spec.UpstreamPool, error) {
	return spec.UpstreamPool{}, nil
}
func (s *stubStateStore) DeleteUpstreamPool(context.Context, string, string) error { return nil }

func TestNewWithConfigState_UsesStateStore(t *testing.T) {
	store := &stubStateStore{
		namespaces: []state.NamespaceSummary{{
			Namespace:         "default",
			Path:              "raft://namespaces/default",
			Exists:            true,
			RouteCount:        1,
			UpstreamPoolCount: 1,
		}},
	}
	service := NewWithConfigState(store)

	items, err := service.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].Namespace, "default"; got != want {
		t.Fatalf("items[0].Namespace = %q, want %q", got, want)
	}
	if !store.listCalled {
		t.Fatal("store.ListNamespaces was not called")
	}
}

func TestNewWithConfigStateWritesThroughStore(t *testing.T) {
	store := &stubStateStore{namespaceConfig: state.NamespaceConfig{Namespace: DefaultNamespace}}
	service := NewWithConfigState(store)
	if _, err := service.ReplaceNamespaceConfig(context.Background(), DefaultNamespace, spec.Config{}); err != nil {
		t.Fatalf("ReplaceNamespaceConfig() error = %v", err)
	}
	if !store.replaceCalled {
		t.Fatal("store.ReplaceNamespaceConfig was not called")
	}
}

func TestCreateUpstreamPoolAndRoute_WritesDefaultNamespace(t *testing.T) {
	service, _ := newTestService(t)

	pool, err := service.CreateUpstreamPool(context.Background(), DefaultNamespace, "pool-api", spec.UpstreamPool{
		Upstreams: []string{"10.0.0.11:8080"},
	})
	if err != nil {
		t.Fatalf("CreateUpstreamPool() error = %v", err)
	}
	if got, want := len(pool.Upstreams), 1; got != want {
		t.Fatalf("len(pool.Upstreams) = %d, want %d", got, want)
	}

	routeCfg, err := service.CreateRoute(context.Background(), DefaultNamespace, spec.RouteConfig{
		ID:      "r-api",
		Enabled: true,
		Match: spec.RouteMatchConfig{
			Hosts: []string{"api.example.com"},
		},
		UpstreamPool: "pool-api",
	})
	if err != nil {
		t.Fatalf("CreateRoute() error = %v", err)
	}
	if got, want := routeCfg.ID, "r-api"; got != want {
		t.Fatalf("routeCfg.ID = %q, want %q", got, want)
	}

	view, err := service.GetNamespaceConfig(context.Background(), DefaultNamespace)
	if err != nil {
		t.Fatalf("GetNamespaceConfig() error = %v", err)
	}
	if got, want := len(view.Routes), 1; got != want {
		t.Fatalf("len(view.Routes) = %d, want %d", got, want)
	}
	if _, ok := view.UpstreamPools["pool-api"]; !ok {
		t.Fatal("view.UpstreamPools[pool-api] missing")
	}
}

func TestToAPIError_PreservesStateErrorMetadata(t *testing.T) {
	err := toAPIError(state.NewNotLeaderError("http://leader:9090"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("toAPIError() error type = %T, want *APIError", err)
	}
	if got, want := apiErr.Code, "not_raft_leader"; got != want {
		t.Fatalf("apiErr.Code = %q, want %q", got, want)
	}
	if got, want := apiErr.LeaderAddress, "http://leader:9090"; got != want {
		t.Fatalf("apiErr.LeaderAddress = %q, want %q", got, want)
	}
}

func TestDeleteUpstreamPool_RejectsReferencedPool(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.ReplaceNamespaceConfig(context.Background(), DefaultNamespace, spec.Config{
		Routes: []spec.RouteConfig{{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "pool-api",
		}},
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceNamespaceConfig() error = %v", err)
	}

	err = service.DeleteUpstreamPool(context.Background(), DefaultNamespace, "pool-api")
	if err == nil {
		t.Fatal("DeleteUpstreamPool() error = nil, want conflict")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("DeleteUpstreamPool() error type = %T, want *APIError", err)
	}
	if got, want := apiErr.StatusCode, http.StatusConflict; got != want {
		t.Fatalf("apiErr.StatusCode = %d, want %d", got, want)
	}
}

func TestListNamespaces_IncludesDefaultWhenMissing(t *testing.T) {
	service, _ := newTestService(t)

	items, err := service.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}

	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].Namespace, DefaultNamespace; got != want {
		t.Fatalf("items[0].Namespace = %q, want %q", got, want)
	}
	if items[0].Exists {
		t.Fatal("items[0].Exists = true, want false")
	}
}

func TestListNamespaces_ReadsStateStoreState(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.CreateNamespace(context.Background(), "admin"); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}

	items, err := service.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}

	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].Namespace, "admin"; got != want {
		t.Fatalf("items[0].Namespace = %q, want %q", got, want)
	}
	if !items[0].Exists {
		t.Fatal("items[0].Exists = false, want true")
	}
}

func TestNewTestServiceLeavesRaftIdentityEmpty(t *testing.T) {
	_, testRuntime := newTestService(t)
	if got := testRuntime.Snapshot().RaftIdentity.NodeID; got != "" {
		t.Fatalf("RaftNodeID = %q, want empty", got)
	}
}

func TestCreateNamespace_WritesEmptyConfig(t *testing.T) {
	service, _ := newTestService(t)

	view, err := service.CreateNamespace(context.Background(), "admin")
	if err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	if got, want := view.Namespace, "admin"; got != want {
		t.Fatalf("view.Namespace = %q, want %q", got, want)
	}

	configView, err := service.GetNamespaceConfig(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetNamespaceConfig() error = %v", err)
	}
	if got, want := len(configView.Routes), 0; got != want {
		t.Fatalf("len(configView.Routes) = %d, want %d", got, want)
	}
	if got, want := len(configView.UpstreamPools), 0; got != want {
		t.Fatalf("len(configView.UpstreamPools) = %d, want %d", got, want)
	}

	items, err := service.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
}

func TestDeleteNamespace_RemovesNamespace(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.CreateNamespace(context.Background(), "admin"); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}

	if err := service.DeleteNamespace(context.Background(), "admin"); err != nil {
		t.Fatalf("DeleteNamespace() error = %v", err)
	}

	items, err := service.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
}

func newTestService(t *testing.T) (Service, *testRuntime) {
	t.Helper()

	cfg := boot.Default()
	fsm := raftstore.NewFSMWithConfig(cfg, nil)
	store := raftstore.NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)
	return NewWithConfigState(store), &testRuntime{appCfg: cfg}
}

func TestNamespaceConfigAppliedAtUsesStateStore(t *testing.T) {
	appliedAt := time.Unix(1700000000, 0).UTC()
	service := NewWithConfigState(&stubStateStore{
		namespaceConfig: state.NamespaceConfig{
			Namespace: DefaultNamespace,
			Exists:    true,
			AppliedAt: appliedAt,
		},
	})

	view, err := service.GetNamespaceConfig(context.Background(), DefaultNamespace)
	if err != nil {
		t.Fatalf("GetNamespaceConfig() error = %v", err)
	}
	if got, want := view.AppliedAt, appliedAt; got != want {
		t.Fatalf("AppliedAt = %v, want %v", got, want)
	}
}

func TestReplaceNamespaceConfig_UsesStoreAndReturnsView(t *testing.T) {
	appliedAt := time.Unix(1700000000, 0).UTC()
	store := &stubStateStore{
		namespaceConfig: state.NamespaceConfig{
			Namespace: DefaultNamespace,
			Exists:    true,
			Routes:    []spec.RouteConfig{{ID: "r-api"}},
			AppliedAt: appliedAt,
		},
	}
	service := NewWithConfigState(store)

	view, err := service.ReplaceNamespaceConfig(context.Background(), DefaultNamespace, spec.Config{})
	if err != nil {
		t.Fatalf("ReplaceNamespaceConfig() error = %v", err)
	}
	if !store.replaceCalled {
		t.Fatal("store.ReplaceNamespaceConfig was not called")
	}
	if got, want := view.Namespace, DefaultNamespace; got != want {
		t.Fatalf("view.Namespace = %q, want %q", got, want)
	}
	if got, want := view.AppliedAt, appliedAt; got != want {
		t.Fatalf("AppliedAt = %v, want %v", got, want)
	}
}

func TestReplaceNamespaceConfig_WritesStoreAndReturnsView(t *testing.T) {
	service, _ := newTestService(t)

	view, err := service.ReplaceNamespaceConfig(context.Background(), DefaultNamespace, spec.Config{
		Routes: []spec.RouteConfig{{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "pool-api",
		}},
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceNamespaceConfig() error = %v", err)
	}
	if got, want := len(view.Routes), 1; got != want {
		t.Fatalf("len(view.Routes) = %d, want %d", got, want)
	}

	configView, err := service.GetNamespaceConfig(context.Background(), DefaultNamespace)
	if err != nil {
		t.Fatalf("GetNamespaceConfig() error = %v", err)
	}
	if got, want := len(configView.Routes), 1; got != want {
		t.Fatalf("len(configView.Routes) = %d, want %d", got, want)
	}
}

type fakeRaft struct {
	state raft.RaftState
	apply func(*raft.Log) interface{}
}

func (r *fakeRaft) State() raft.RaftState      { return r.state }
func (r *fakeRaft) Leader() raft.ServerAddress { return "" }
func (r *fakeRaft) Apply(data []byte, _ time.Duration) raft.ApplyFuture {
	if r.apply == nil {
		return &fakeApplyFuture{}
	}
	return &fakeApplyFuture{response: r.apply(&raft.Log{Index: 1, Data: data})}
}

type fakeApplyFuture struct {
	response interface{}
}

func (f *fakeApplyFuture) Error() error          { return nil }
func (f *fakeApplyFuture) Response() interface{} { return f.response }
func (f *fakeApplyFuture) Index() uint64         { return 1 }
func (f *fakeApplyFuture) Start() time.Time      { return time.Now() }
