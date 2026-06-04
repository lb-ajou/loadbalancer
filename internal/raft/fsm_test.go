package raftstore

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
)

func TestFSMApplyCreatePoolAndRoute(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, Command{
		Type:      CommandCreateUpstreamPool,
		Namespace: control.DefaultNamespace,
		PoolID:    "pool-api",
		Pool:      spec.UpstreamPool{Upstreams: []string{"10.0.0.11:8080"}},
	})
	applyCommand(t, fsm, Command{
		Type:      CommandCreateRoute,
		Namespace: control.DefaultNamespace,
		Route: spec.RouteConfig{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "pool-api",
		},
	})

	state := fsm.DesiredState()
	cfg := state.Namespaces[control.DefaultNamespace]
	if got, want := len(cfg.Routes), 1; got != want {
		t.Fatalf("len(cfg.Routes) = %d, want %d", got, want)
	}
	if _, ok := cfg.UpstreamPools["pool-api"]; !ok {
		t.Fatal("cfg.UpstreamPools[pool-api] missing")
	}
}

func TestFSMApplyInvalidCommandLeavesStateUnchanged(t *testing.T) {
	fsm := NewFSM()
	resp := applyCommand(t, fsm, Command{
		Type:      CommandCreateRoute,
		Namespace: control.DefaultNamespace,
		Route: spec.RouteConfig{
			ID:           "r-api",
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			UpstreamPool: "missing",
		},
	})
	if resp.Error == "" {
		t.Fatal("response error is empty, want validation error")
	}
	if got := len(fsm.DesiredState().Namespaces); got != 0 {
		t.Fatalf("len(fsm.DesiredState().Namespaces) = %d, want 0", got)
	}
}

func TestFSMApplyRejectsInvalidNamespace(t *testing.T) {
	fsm := NewFSM()
	resp := applyCommand(t, fsm, Command{
		Type:      CommandCreateNamespace,
		Namespace: "bad/name",
	})

	requireApplyRejection(t, resp, http.StatusBadRequest, "invalid_namespace")
	if got := len(fsm.DesiredState().Namespaces); got != 0 {
		t.Fatalf("len(fsm.DesiredState().Namespaces) = %d, want 0", got)
	}
}

func TestFSMReplaceNamespaceConfigReplacesOnlyTargetNamespace(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, Command{
		Type:      CommandReplaceNamespaceConfig,
		Namespace: "default",
		Config:    validFSMConfig("r-api", "pool-api"),
	})
	applyCommand(t, fsm, Command{
		Type:      CommandReplaceNamespaceConfig,
		Namespace: "admin",
		Config:    validFSMConfig("r-admin", "pool-admin"),
	})

	resp := applyCommand(t, fsm, Command{
		Type:      CommandReplaceNamespaceConfig,
		Namespace: "default",
		Config:    validFSMConfig("r-new", "pool-new"),
	})
	if resp.Error != "" {
		t.Fatalf("ReplaceNamespaceConfig response error = %q", resp.Error)
	}

	state := fsm.DesiredState()
	if got, want := state.Namespaces["default"].Routes[0].ID, "r-new"; got != want {
		t.Fatalf("default route ID = %q, want %q", got, want)
	}
	if got, want := state.Namespaces["admin"].Routes[0].ID, "r-admin"; got != want {
		t.Fatalf("admin route ID = %q, want %q", got, want)
	}
}

func TestFSMReplaceNamespaceConfigRejectsInvalidWithoutChangingState(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, Command{
		Type:      CommandReplaceNamespaceConfig,
		Namespace: "default",
		Config:    validFSMConfig("r-api", "pool-api"),
	})

	resp := applyCommand(t, fsm, Command{
		Type:      CommandReplaceNamespaceConfig,
		Namespace: "default",
		Config: spec.Config{
			Routes: []spec.RouteConfig{{
				ID:           "r-bad",
				Enabled:      true,
				Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
				UpstreamPool: "missing",
			}},
		},
	})

	requireApplyRejection(t, resp, http.StatusUnprocessableEntity, "validation_failed")
	state := fsm.DesiredState()
	if got, want := state.Namespaces["default"].Routes[0].ID, "r-api"; got != want {
		t.Fatalf("default route ID = %q, want %q", got, want)
	}
}

func TestFSMSetAndClearClusterVIP(t *testing.T) {
	fsm := NewFSM()
	vip := validClusterVIP()

	resp := applyCommand(t, fsm, Command{Type: CommandSetClusterVIP, VIP: &vip})
	if resp.Error != "" {
		t.Fatalf("SetClusterVIP response error = %q", resp.Error)
	}
	if got, want := fsm.DesiredState().VIP.Address, "10.10.0.100/24"; got != want {
		t.Fatalf("VIP.Address = %q, want %q", got, want)
	}

	resp = applyCommand(t, fsm, Command{Type: CommandClearClusterVIP})
	if resp.Error != "" {
		t.Fatalf("ClearClusterVIP response error = %q", resp.Error)
	}
	if fsm.DesiredState().VIP != nil {
		t.Fatalf("VIP = %+v, want nil", fsm.DesiredState().VIP)
	}
}

func TestFSMSetClusterVIPNormalizesDefaults(t *testing.T) {
	fsm := NewFSM()
	vip := control.ClusterVIPConfig{Address: "10.10.0.100/24"}

	resp := applyCommand(t, fsm, Command{Type: CommandSetClusterVIP, VIP: &vip})
	if resp.Error != "" {
		t.Fatalf("SetClusterVIP response error = %q", resp.Error)
	}
	if got, want := fsm.DesiredState().VIP.GARPCount, control.DefaultVIPGARPCount; got != want {
		t.Fatalf("VIP.GARPCount = %d, want %d", got, want)
	}
}

func TestFSMSetClusterVIPRejectsInvalidConfig(t *testing.T) {
	fsm := NewFSM()
	vip := validClusterVIP()
	vip.Address = "bad"

	resp := applyCommand(t, fsm, Command{Type: CommandSetClusterVIP, VIP: &vip})
	requireApplyRejection(t, resp, http.StatusBadRequest, "invalid_vip")
	if fsm.DesiredState().VIP != nil {
		t.Fatalf("VIP = %+v, want nil", fsm.DesiredState().VIP)
	}
}

func TestFSMSetClusterRaftTiming(t *testing.T) {
	fsm := NewFSM()
	timing := validClusterRaftTiming()

	resp := applyCommand(t, fsm, Command{Type: CommandSetClusterRaftTiming, RaftTiming: &timing})
	if resp.Error != "" {
		t.Fatalf("SetClusterRaftTiming response error = %q", resp.Error)
	}
	if got, want := fsm.DesiredState().RaftTiming.HeartbeatTimeout, "3s"; got != want {
		t.Fatalf("RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}

func TestFSMSetClusterRaftTimingRejectsInvalidConfig(t *testing.T) {
	fsm := NewFSM()
	timing := validClusterRaftTiming()
	timing.ElectionTimeout = "1s"

	resp := applyCommand(t, fsm, Command{Type: CommandSetClusterRaftTiming, RaftTiming: &timing})
	requireApplyRejection(t, resp, http.StatusBadRequest, "invalid_raft_timing")
	if fsm.DesiredState().RaftTiming != nil {
		t.Fatalf("RaftTiming = %+v, want nil", fsm.DesiredState().RaftTiming)
	}
}

func TestFSMSnapshotRestoreRoundTrip(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, Command{
		Type:      CommandCreateNamespace,
		Namespace: "admin",
	})
	vip := validClusterVIP()
	applyCommand(t, fsm, Command{Type: CommandSetClusterVIP, VIP: &vip})
	timing := validClusterRaftTiming()
	applyCommand(t, fsm, Command{Type: CommandSetClusterRaftTiming, RaftTiming: &timing})
	snapshot, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	var buf bytes.Buffer
	if err := snapshot.Persist(&memorySink{Buffer: &buf}); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}

	restored := NewFSM()
	if err := restored.Restore(io.NopCloser(bytes.NewReader(buf.Bytes()))); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if _, ok := restored.DesiredState().Namespaces["admin"]; !ok {
		t.Fatal("restored namespace admin missing")
	}
	if got, want := restored.DesiredState().VIP.Address, "10.10.0.100/24"; got != want {
		t.Fatalf("restored VIP.Address = %q, want %q", got, want)
	}
	if got, want := restored.DesiredState().RaftTiming.HeartbeatTimeout, "3s"; got != want {
		t.Fatalf("restored RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}

func TestFSMSnapshotRestoreNormalizesVIPDefaults(t *testing.T) {
	body := `{"namespaces":{},"vip":{"address":"10.10.0.100/24"}}`
	restored := NewFSM()

	if err := restored.Restore(io.NopCloser(strings.NewReader(body))); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if got, want := restored.DesiredState().VIP.GARPCount, control.DefaultVIPGARPCount; got != want {
		t.Fatalf("VIP.GARPCount = %d, want %d", got, want)
	}
}

func requireApplyRejection(t *testing.T, resp ApplyResponse, statusCode int, code string) {
	t.Helper()
	if resp.Error == "" {
		t.Fatal("ApplyResponse.Error is empty, want rejection")
	}
	if resp.StatusCode != statusCode || resp.Code != code {
		t.Fatalf("ApplyResponse = status %d code %q, want status %d code %q", resp.StatusCode, resp.Code, statusCode, code)
	}
}

func applyCommand(t *testing.T, fsm *FSM, cmd Command) ApplyResponse {
	t.Helper()
	data, err := EncodeCommand(cmd)
	if err != nil {
		t.Fatalf("EncodeCommand() error = %v", err)
	}
	resp, ok := fsm.Apply(&raft.Log{Data: data}).(ApplyResponse)
	if !ok {
		t.Fatalf("Apply() response type = %T, want ApplyResponse", resp)
	}
	return resp
}

type memorySink struct {
	*bytes.Buffer
}

func (s *memorySink) ID() string    { return "memory" }
func (s *memorySink) Close() error  { return nil }
func (s *memorySink) Cancel() error { return nil }

func validFSMConfig(routeID, poolID string) spec.Config {
	return spec.Config{
		Routes: []spec.RouteConfig{{
			ID:           routeID,
			Enabled:      true,
			Match:        spec.RouteMatchConfig{Hosts: []string{routeID + ".example.com"}},
			UpstreamPool: poolID,
		}},
		UpstreamPools: map[string]spec.UpstreamPool{
			poolID: {Upstreams: []string{"10.0.0.11:8080"}},
		},
	}
}

func validClusterVIP() control.ClusterVIPConfig {
	return control.ClusterVIPConfig{
		Address:           "10.10.0.100/24",
		GARPCount:         3,
		GARPInterval:      "100ms",
		AcquireDelay:      "300ms",
		ReleaseOnShutdown: true,
	}
}

func validClusterRaftTiming() control.ClusterRaftTimingConfig {
	return control.ClusterRaftTimingConfig{
		HeartbeatTimeout:   "3s",
		ElectionTimeout:    "5s",
		LeaderLeaseTimeout: "2s",
		CommitTimeout:      "250ms",
	}
}
