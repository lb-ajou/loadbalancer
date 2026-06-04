package raftstore

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
)

func TestStoreReturnsNotLeaderWhenNodeIsFollower(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Follower}, NewFSM())

	_, err := store.CreateNamespace(context.Background(), "admin")
	if err == nil {
		t.Fatal("CreateNamespace() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("CreateNamespace() error = %v, want not leader", err)
	}
}

func TestStoreReplaceNamespaceConfigReturnsNotLeaderWhenNodeIsFollower(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Follower}, NewFSM())

	_, err := store.ReplaceNamespaceConfig(context.Background(), "admin", spec.Config{})
	if err == nil {
		t.Fatal("ReplaceNamespaceConfig() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("ReplaceNamespaceConfig() error = %v, want not leader", err)
	}
}

func TestStoreReturnsNotLeaderBeforeWriteValidationOnFollower(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Follower}, NewFSM())

	_, err := store.CreateNamespace(context.Background(), "bad:name")
	if err == nil {
		t.Fatal("CreateNamespace() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("CreateNamespace() error = %v, want not leader", err)
	}
}

func TestStoreAppliesCommandOnLeader(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

	_, err := store.CreateUpstreamPool(context.Background(), "default", "pool-api", spec.UpstreamPool{
		Upstreams: []string{"10.0.0.11:8080"},
	})
	if err != nil {
		t.Fatalf("CreateUpstreamPool() error = %v", err)
	}
	state := fsm.DesiredState()
	if _, ok := state.Namespaces["default"].UpstreamPools["pool-api"]; !ok {
		t.Fatal("pool-api missing from FSM state")
	}
}

func TestStoreSetAndClearClusterVIPOnLeader(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

	if err := store.SetClusterVIP(context.Background(), validClusterVIP()); err != nil {
		t.Fatalf("SetClusterVIP() error = %v", err)
	}
	if got, want := fsm.DesiredState().VIP.Address, "10.10.0.100/24"; got != want {
		t.Fatalf("VIP.Address = %q, want %q", got, want)
	}
	if err := store.ClearClusterVIP(context.Background()); err != nil {
		t.Fatalf("ClearClusterVIP() error = %v", err)
	}
	if fsm.DesiredState().VIP != nil {
		t.Fatalf("VIP = %+v, want nil", fsm.DesiredState().VIP)
	}
}

func TestStoreSetClusterVIPReturnsNotLeaderWhenFollower(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Follower}, NewFSM())

	err := store.SetClusterVIP(context.Background(), validClusterVIP())
	if !control.IsNotLeader(err) {
		t.Fatalf("SetClusterVIP() error = %v, want not leader", err)
	}
}

func TestStoreSetClusterRaftTimingOnLeader(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

	if err := store.SetClusterRaftTiming(context.Background(), validClusterRaftTiming()); err != nil {
		t.Fatalf("SetClusterRaftTiming() error = %v", err)
	}
	if got, want := fsm.DesiredState().RaftTiming.HeartbeatTimeout, "3s"; got != want {
		t.Fatalf("RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}

func TestStoreListNamespacesReturnsRaftMetadataPath(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)
	if _, err := store.CreateNamespace(context.Background(), "admin"); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}

	items, err := store.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	paths := namespacePaths(items)
	requireNamespacePath(t, paths, "admin", "raft://namespaces/admin")
	requireNamespacePath(t, paths, "default", "raft://namespaces/default")
}

func namespacePaths(items []control.NamespaceSummary) map[string]string {
	paths := map[string]string{}
	for _, item := range items {
		paths[item.Namespace] = item.Path
	}
	return paths
}

func requireNamespacePath(t *testing.T, paths map[string]string, namespace, want string) {
	t.Helper()
	if got := paths[namespace]; got != want {
		t.Fatalf("%s path = %q, want %q", namespace, got, want)
	}
}

func TestStoreReplaceNamespaceConfigAppliesCommandOnLeader(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

	view, err := store.ReplaceNamespaceConfig(context.Background(), "default", validStoreConfig())
	if err != nil {
		t.Fatalf("ReplaceNamespaceConfig() error = %v", err)
	}
	if !view.Exists {
		t.Fatal("view.Exists = false, want true")
	}
	if got, want := view.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("view.Routes[0].ID = %q, want %q", got, want)
	}
	if _, ok := fsm.DesiredState().Namespaces["default"].UpstreamPools["pool-api"]; !ok {
		t.Fatal("pool-api missing from FSM state")
	}
}

func TestStoreMapsApplyLeadershipErrorToNotLeader(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Leader, applyErr: raft.ErrNotLeader}, NewFSM())

	_, err := store.CreateNamespace(context.Background(), "admin")
	if err == nil {
		t.Fatal("CreateNamespace() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("CreateNamespace() error = %v, want not leader", err)
	}
}

func TestStoreReturnsContextErrorBeforeApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	node := &fakeRaft{state: raft.Leader}
	store := NewStore(node, NewFSM())

	_, err := store.CreateNamespace(ctx, "admin")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateNamespace() error = %v, want context canceled", err)
	}
	if node.applyCount != 0 {
		t.Fatalf("Apply() calls = %d, want 0", node.applyCount)
	}
}

func TestStoreRejectsInvalidNamespaceWithBadRequest(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

	_, err := store.CreateNamespace(context.Background(), "bad/name")
	requireStateError(t, err, http.StatusBadRequest, "invalid_namespace")
}

func TestStoreRejectsInvalidNamespaceBeforeApply(t *testing.T) {
	node := &fakeRaft{state: raft.Leader}
	store := NewStore(node, NewFSM())

	_, err := store.CreateNamespace(context.Background(), "bad:name")
	requireStateError(t, err, http.StatusBadRequest, "invalid_namespace")
	if node.applyCount != 0 {
		t.Fatalf("Apply() calls = %d, want 0", node.applyCount)
	}
}

func TestStoreRejectsInvalidNamespaceReads(t *testing.T) {
	store := NewStore(&fakeRaft{state: raft.Leader}, NewFSM())

	_, err := store.GetNamespaceConfig(context.Background(), "bad:name")
	requireStateError(t, err, http.StatusBadRequest, "invalid_namespace")
}

func TestStoreMapsApplyRejectionsToStateErrorSemantics(t *testing.T) {
	t.Run("duplicate namespace", func(t *testing.T) {
		fsm := NewFSM()
		store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

		if _, err := store.CreateNamespace(context.Background(), "admin"); err != nil {
			t.Fatalf("CreateNamespace() setup error = %v", err)
		}

		_, err := store.CreateNamespace(context.Background(), "admin")
		requireStateError(t, err, http.StatusConflict, "resource_conflict")
	})

	t.Run("missing route delete", func(t *testing.T) {
		fsm := NewFSM()
		store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

		if _, err := store.CreateNamespace(context.Background(), "admin"); err != nil {
			t.Fatalf("CreateNamespace() setup error = %v", err)
		}

		err := store.DeleteRoute(context.Background(), "admin", "missing")
		requireStateError(t, err, http.StatusNotFound, "resource_not_found")
	})

	t.Run("route id mismatch", func(t *testing.T) {
		fsm := NewFSM()
		store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

		_, err := store.UpdateRoute(context.Background(), "admin", "r-api", spec.RouteConfig{ID: "other"})
		requireStateError(t, err, http.StatusBadRequest, "invalid_request")
	})

	t.Run("validation failure", func(t *testing.T) {
		fsm := NewFSM()
		store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

		_, err := store.CreateRoute(context.Background(), "admin", spec.RouteConfig{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "missing",
		})
		requireStateError(t, err, http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("replace validation failure", func(t *testing.T) {
		fsm := NewFSM()
		store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

		_, err := store.ReplaceNamespaceConfig(context.Background(), "admin", spec.Config{
			Routes: []spec.RouteConfig{{
				ID:           "r-api",
				Enabled:      true,
				Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
				UpstreamPool: "missing",
			}},
		})
		requireStateError(t, err, http.StatusUnprocessableEntity, "validation_failed")
	})
}

func requireStateError(t *testing.T, err error, statusCode int, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want status %d code %q", statusCode, code)
	}
	var stateErr *control.StateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("error = %T %v, want *control.StateError", err, err)
	}
	if stateErr.StatusCode != statusCode || stateErr.Code != code {
		t.Fatalf("StateError = status %d code %q, want status %d code %q", stateErr.StatusCode, stateErr.Code, statusCode, code)
	}
}

type fakeRaft struct {
	leader     string
	state      raft.RaftState
	apply      func(*raft.Log) interface{}
	applyErr   error
	applyCount int
}

func (r *fakeRaft) State() raft.RaftState      { return r.state }
func (r *fakeRaft) Leader() raft.ServerAddress { return raft.ServerAddress(r.leader) }
func (r *fakeRaft) Apply(data []byte, timeout time.Duration) raft.ApplyFuture {
	r.applyCount++
	if r.applyErr != nil {
		return &fakeApplyFuture{err: r.applyErr}
	}
	if r.apply == nil {
		return &fakeApplyFuture{}
	}
	return &fakeApplyFuture{response: r.apply(&raft.Log{Index: 1, Data: data})}
}

type fakeApplyFuture struct {
	err      error
	response interface{}
}

func (f *fakeApplyFuture) Error() error          { return f.err }
func (f *fakeApplyFuture) Response() interface{} { return f.response }
func (f *fakeApplyFuture) Index() uint64         { return 1 }
func (f *fakeApplyFuture) Start() time.Time      { return time.Now() }

func validStoreConfig() spec.Config {
	return spec.Config{
		Routes: []spec.RouteConfig{{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "pool-api",
		}},
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
		},
	}
}
