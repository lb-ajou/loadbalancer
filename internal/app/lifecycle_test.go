package app

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/dashboard"
	"reverseproxy-poc/internal/raft"
	"reverseproxy-poc/internal/state"
)

func TestBootstrapClusterStartsRaftOnCleanNode(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)

	err := app.BootstrapCluster(context.Background(), dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftBindAddr:      "127.0.0.1:0",
		RaftAdvertiseAddr: "127.0.0.1:7001",
	})
	if err != nil {
		t.Fatalf("BootstrapCluster() error = %v", err)
	}
	if app.raftNode == nil {
		t.Fatal("raftNode is nil, want started raft node")
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestBootstrapClusterPersistsLocalRaftConfig(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)

	err := app.BootstrapCluster(context.Background(), dashboard.ClusterBootstrapRequest{
		NodeID:            "node-a",
		RaftBindAddr:      "127.0.0.1:0",
		RaftAdvertiseAddr: "127.0.0.1:7009",
	})
	if err != nil {
		t.Fatalf("BootstrapCluster() error = %v", err)
	}
	defer func() { _ = app.Shutdown(context.Background()) }()

	got, ok, err := raftstore.LoadLocalNodeConfig(filepath.Join(dir, "raft"))
	if err != nil {
		t.Fatalf("LoadLocalNodeConfig() error = %v", err)
	}
	if !ok || got.NodeID != "node-a" || got.AdvertiseAddr != "127.0.0.1:7009" {
		t.Fatalf("LocalNodeConfig = %+v ok=%v", got, ok)
	}
}

func TestBootstrapClusterStoresRaftTiming(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)

	err := app.BootstrapCluster(context.Background(), dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftBindAddr:      "127.0.0.1:0",
		RaftAdvertiseAddr: "127.0.0.1:7011",
		RaftTiming: &dashboard.ClusterRaftTimingRequest{
			HeartbeatTimeout:   "100ms",
			ElectionTimeout:    "150ms",
			LeaderLeaseTimeout: "50ms",
			CommitTimeout:      "10ms",
		},
	})
	if err != nil {
		t.Fatalf("BootstrapCluster() error = %v", err)
	}
	defer func() { _ = app.Shutdown(context.Background()) }()

	if got, want := app.Snapshot().RaftTiming.HeartbeatTimeout, "100ms"; got != want {
		t.Fatalf("RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}

func TestClusterLifecycleStatusCleanNodeAllowsBootstrapAndJoin(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)

	view := app.ClusterLifecycleStatus(context.Background())
	if view.State != "unconfigured" || !view.CanBootstrap || !view.CanJoin {
		t.Fatalf("ClusterLifecycleStatus() = %+v, want unconfigured with actions", view)
	}
	if view.RaftRunning || view.HasRaftState {
		t.Fatalf("ClusterLifecycleStatus() = %+v, want no raft state", view)
	}
}

func TestBootstrapConfigDefaultsRaftBindAddrFromAdvertisePort(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)

	_, raftCfg, _, err := app.bootstrapConfig(dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftAdvertiseAddr: "10.0.0.11:7001",
	})
	if err != nil {
		t.Fatalf("bootstrapConfig() error = %v", err)
	}
	if got, want := raftCfg.Identity.BindAddr, "0.0.0.0:7001"; got != want {
		t.Fatalf("RaftBindAddr = %q, want %q", got, want)
	}
}

func TestBootstrapConfigDefaultsVIPPolicyFromClusterState(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)

	_, _, vip, err := app.bootstrapConfig(dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftAdvertiseAddr: "10.0.0.11:7001",
		VIPInterface:      "eth0",
		VIP:               &dashboard.ClusterVIPRequest{Address: "10.10.0.100/24"},
	})
	if err != nil {
		t.Fatalf("bootstrapConfig() error = %v", err)
	}
	if vip.GARPCount != state.DefaultVIPGARPCount ||
		vip.GARPInterval != state.DefaultVIPGARPInterval ||
		vip.AcquireDelay != state.DefaultVIPAcquireDelay {
		t.Fatalf("VIP = %+v, want cluster defaults", vip)
	}
}

func TestRestoreRaftConfigPrefersLocalMetadata(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "raft")
	err := raftstore.SaveLocalNodeConfig(dataDir, raftstore.LocalNodeConfig{
		NodeID:        "node-restored",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:7010",
	})
	if err != nil {
		t.Fatalf("SaveLocalNodeConfig() error = %v", err)
	}

	app := &App{}
	cfg, err := app.restoreRaftConfig(boot.AppConfig{RaftDataDir: dataDir})
	if err != nil {
		t.Fatalf("restoreRaftConfig() error = %v", err)
	}
	if cfg.Identity.NodeID != "node-restored" || cfg.Identity.AdvertiseAddr != "127.0.0.1:7010" {
		t.Fatalf("restored config = %+v", cfg)
	}
}

func TestClusterLifecycleStatusClusteredNodeDisablesActions(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)
	err := app.BootstrapCluster(context.Background(), dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftBindAddr:      "127.0.0.1:0",
		RaftAdvertiseAddr: "127.0.0.1:7001",
	})
	if err != nil {
		t.Fatalf("BootstrapCluster() error = %v", err)
	}
	defer func() { _ = app.Shutdown(context.Background()) }()

	view := app.ClusterLifecycleStatus(context.Background())
	if view.State != "clustered" || !view.RaftRunning {
		t.Fatalf("ClusterLifecycleStatus() = %+v, want clustered running", view)
	}
	if view.CanBootstrap || view.CanJoin {
		t.Fatalf("ClusterLifecycleStatus() = %+v, want actions disabled", view)
	}
}

func TestBootstrapClusterRejectsAlreadyConfiguredNode(t *testing.T) {
	dir := t.TempDir()
	app := newTestLifecycleApp(t, dir)
	request := dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftBindAddr:      "127.0.0.1:0",
		RaftAdvertiseAddr: "127.0.0.1:7001",
	}
	if err := app.BootstrapCluster(context.Background(), request); err != nil {
		t.Fatalf("BootstrapCluster() setup error = %v", err)
	}
	err := app.BootstrapCluster(context.Background(), request)
	if err == nil {
		t.Fatal("BootstrapCluster() error = nil, want already configured")
	}
	if got, want := stateErrorCode(err), "cluster_already_configured"; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	_ = app.Shutdown(context.Background())
}

func newTestLifecycleApp(t *testing.T, dir string) *App {
	t.Helper()
	app, err := New(boot.AppConfig{
		RaftDataDir: filepath.Join(dir, "raft"),
	}, filepath.Join(dir, "app.json"), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return app
}

func stateErrorCode(err error) string {
	if stateErr, ok := err.(*state.StateError); ok {
		return stateErr.Code
	}
	return ""
}
