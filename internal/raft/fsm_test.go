package raftstore

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/raft"

	"loadbalancer/internal/spec"
	control "loadbalancer/internal/state"
)

func TestFSMApplyReplaceConfig(t *testing.T) {
	fsm := NewFSM()
	resp := applyCommand(t, fsm, Command{Type: CommandReplaceConfig, Config: validFSMConfig("r-api", "pool-api")})
	if resp.Error != "" {
		t.Fatalf("ReplaceConfig response error = %q", resp.Error)
	}

	state := fsm.DesiredState()
	if got, want := len(state.ProxyConfig.Routes), 1; got != want {
		t.Fatalf("len(state.ProxyConfig.Routes) = %d, want %d", got, want)
	}
	if _, ok := state.ProxyConfig.UpstreamPools["pool-api"]; !ok {
		t.Fatal("state.ProxyConfig.UpstreamPools[pool-api] missing")
	}
}

func TestFSMApplyInvalidCommandLeavesStateUnchanged(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, Command{Type: CommandReplaceConfig, Config: validFSMConfig("r-api", "pool-api")})

	resp := applyCommand(t, fsm, Command{
		Type: CommandReplaceConfig,
		Config: spec.Config{
			Routes: []spec.RouteConfig{{
				ID:           "r-bad",
				Enabled:      true,
				Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
				UpstreamPool: "missing",
			}},
		},
	})
	if resp.Error == "" {
		t.Fatal("response error is empty, want validation error")
	}
	if got, want := len(resp.ValidationErrors), 1; got != want {
		t.Fatalf("len(resp.ValidationErrors) = %d, want %d", got, want)
	}
	if got, want := fsm.DesiredState().ProxyConfig.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("route ID = %q, want %q", got, want)
	}
}

func TestFSMReplaceConfigReplacesWholeConfig(t *testing.T) {
	fsm := NewFSM()
	applyCommand(t, fsm, Command{Type: CommandReplaceConfig, Config: validFSMConfig("r-api", "pool-api")})

	resp := applyCommand(t, fsm, Command{
		Type:   CommandReplaceConfig,
		Config: validFSMConfig("r-new", "pool-new"),
	})
	if resp.Error != "" {
		t.Fatalf("ReplaceConfig response error = %q", resp.Error)
	}

	state := fsm.DesiredState()
	if got, want := state.ProxyConfig.Routes[0].ID, "r-new"; got != want {
		t.Fatalf("route ID = %q, want %q", got, want)
	}
	if _, ok := state.ProxyConfig.UpstreamPools["pool-api"]; ok {
		t.Fatal("old upstream pool is still present after whole config replace")
	}
}

func TestFSMInitialStateHasEmptyProxyConfig(t *testing.T) {
	fsm := NewFSM()

	state := fsm.DesiredState()
	if state.ProxyConfig.Routes == nil {
		t.Fatal("state.ProxyConfig.Routes = nil, want empty slice")
	}
	if state.ProxyConfig.UpstreamPools == nil {
		t.Fatal("state.ProxyConfig.UpstreamPools = nil, want empty map")
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
	applyCommand(t, fsm, Command{Type: CommandReplaceConfig, Config: validFSMConfig("r-api", "pool-api")})
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
	if got, want := restored.DesiredState().ProxyConfig.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("restored route ID = %q, want %q", got, want)
	}
	if _, ok := restored.DesiredState().ProxyConfig.UpstreamPools["pool-api"]; !ok {
		t.Fatal("restored upstream pool pool-api missing")
	}
	if got, want := restored.DesiredState().VIP.Address, "10.10.0.100/24"; got != want {
		t.Fatalf("restored VIP.Address = %q, want %q", got, want)
	}
	if got, want := restored.DesiredState().RaftTiming.HeartbeatTimeout, "3s"; got != want {
		t.Fatalf("restored RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}

func TestFSMSnapshotRestoreNormalizesVIPDefaults(t *testing.T) {
	body := `{"ProxyConfig":{},"VIP":{"address":"10.10.0.100/24"}}`
	restored := NewFSM()

	if err := restored.Restore(io.NopCloser(strings.NewReader(body))); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if restored.DesiredState().ProxyConfig.Routes == nil {
		t.Fatal("Routes = nil, want empty slice")
	}
	if restored.DesiredState().ProxyConfig.UpstreamPools == nil {
		t.Fatal("UpstreamPools = nil, want empty map")
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
