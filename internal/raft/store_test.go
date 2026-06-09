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

func TestStoreReplaceConfigReturnsNotLeaderWhenNodeIsFollower(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Follower}, NewFSM())

	_, err := store.ReplaceConfig(context.Background(), spec.Config{})
	if err == nil {
		t.Fatal("ReplaceConfig() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("ReplaceConfig() error = %v, want not leader", err)
	}
}

func TestStoreReturnsNotLeaderBeforeWriteValidationOnFollower(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Follower}, NewFSM())

	_, err := store.ReplaceConfig(context.Background(), invalidStoreConfig())
	if err == nil {
		t.Fatal("ReplaceConfig() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("ReplaceConfig() error = %v, want not leader", err)
	}
}

func TestStoreReplaceConfigAppliesCommandOnLeader(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

	view, err := store.ReplaceConfig(context.Background(), validStoreConfig())
	if err != nil {
		t.Fatalf("ReplaceConfig() error = %v", err)
	}
	if got, want := view.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("view.Routes[0].ID = %q, want %q", got, want)
	}
	if _, ok := fsm.DesiredState().ProxyConfig.UpstreamPools["pool-api"]; !ok {
		t.Fatal("pool-api missing from FSM state")
	}
}

func TestStoreGetConfigReturnsCurrentConfig(t *testing.T) {
	fsm := NewFSM()
	store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)
	if _, err := store.ReplaceConfig(context.Background(), validStoreConfig()); err != nil {
		t.Fatalf("ReplaceConfig() error = %v", err)
	}

	view, err := store.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if got, want := view.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("view.Routes[0].ID = %q, want %q", got, want)
	}
	if view.AppliedAt.IsZero() {
		t.Fatal("view.AppliedAt is zero")
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

func TestStoreMapsApplyLeadershipErrorToNotLeader(t *testing.T) {
	store := NewStore(&fakeRaft{leader: "127.0.0.1:7001", state: raft.Leader, applyErr: raft.ErrNotLeader}, NewFSM())

	_, err := store.ReplaceConfig(context.Background(), validStoreConfig())
	if err == nil {
		t.Fatal("ReplaceConfig() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("ReplaceConfig() error = %v, want not leader", err)
	}
}

func TestStoreReturnsContextErrorBeforeApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	node := &fakeRaft{state: raft.Leader}
	store := NewStore(node, NewFSM())

	_, err := store.ReplaceConfig(ctx, validStoreConfig())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReplaceConfig() error = %v, want context canceled", err)
	}
	if node.applyCount != 0 {
		t.Fatalf("Apply() calls = %d, want 0", node.applyCount)
	}
}

func TestStoreMapsApplyRejectionsToStateErrorSemantics(t *testing.T) {
	t.Run("validation failure", func(t *testing.T) {
		fsm := NewFSM()
		store := NewStore(&fakeRaft{state: raft.Leader, apply: fsm.Apply}, fsm)

		_, err := store.ReplaceConfig(context.Background(), invalidStoreConfig())
		requireStateError(t, err, http.StatusUnprocessableEntity, "validation_failed")
		var validationErrs spec.ValidationErrors
		if !errors.As(err, &validationErrs) {
			t.Fatalf("ReplaceConfig() error = %T %v, want spec.ValidationErrors", err, err)
		}
		if got, want := len(validationErrs), 1; got != want {
			t.Fatalf("len(validationErrs) = %d, want %d", got, want)
		}
	})

	t.Run("explicit apply rejection", func(t *testing.T) {
		store := NewStore(&fakeRaft{
			state: raft.Leader,
			apply: func(*raft.Log) interface{} {
				return ApplyResponse{Error: "rejected", StatusCode: http.StatusBadRequest, Code: "invalid_request"}
			},
		}, NewFSM())

		_, err := store.ReplaceConfig(context.Background(), validStoreConfig())
		requireStateError(t, err, http.StatusBadRequest, "invalid_request")
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

func invalidStoreConfig() spec.Config {
	return spec.Config{
		Routes: []spec.RouteConfig{{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "missing",
		}},
	}
}
