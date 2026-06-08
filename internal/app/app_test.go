package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/dashboard"
	"reverseproxy-poc/internal/upstream"
)

func TestAppStartAndStopHealthChecker(t *testing.T) {
	app := &App{
		healthChecker: upstream.NewChecker(nil),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.startHealthChecker(ctx)

	if app.runCtx == nil {
		t.Fatal("runCtx is nil")
	}
	if app.healthCtx == nil {
		t.Fatal("healthCtx is nil")
	}
	if app.healthCancel == nil {
		t.Fatal("healthCancel is nil")
	}

	app.stopHealthChecker()

	if app.runCtx != nil {
		t.Fatal("runCtx is not nil after stop")
	}
	if app.healthCtx != nil {
		t.Fatal("healthCtx is not nil after stop")
	}
	if app.healthCancel != nil {
		t.Fatal("healthCancel is not nil after stop")
	}
}

func TestAppSwapHealthChecker_ReplacesCheckerAndStartsNewContext(t *testing.T) {
	registry, err := upstream.NewRegistry([]upstream.Pool{
		{GlobalID: "default:pool-api"},
	})
	if err != nil {
		t.Fatalf("upstream.NewRegistry() error = %v", err)
	}

	app := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.startHealthChecker(ctx)
	firstHealthCtx := app.healthCtx

	app.swapHealthChecker(registry)

	if app.healthChecker == nil {
		t.Fatal("healthChecker is nil")
	}
	if app.healthCtx == nil {
		t.Fatal("healthCtx is nil")
	}
	if app.healthCtx == firstHealthCtx {
		t.Fatal("healthCtx was not replaced")
	}
	if app.healthCancel == nil {
		t.Fatal("healthCancel is nil")
	}

	app.stopHealthChecker()
}

func TestAppHealthCheckerCancelCancelsContext(t *testing.T) {
	app := &App{
		healthChecker: upstream.NewChecker(nil),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.startHealthChecker(ctx)
	healthCtx := app.healthCtx

	app.stopHealthChecker()

	select {
	case <-healthCtx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("health context was not canceled")
	}
}

func TestNew_WiresDashboardServerHandler(t *testing.T) {
	dir := t.TempDir()
	cfg := boot.AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
		RaftDataDir:         filepath.Join(dir, "raft"),
	}

	app, err := New(cfg, filepath.Join(dir, "app.json"), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	if app.dashboardHandler == nil {
		t.Fatal("dashboardHandler is nil")
	}
	if app.dashboardServer == nil {
		t.Fatal("dashboardServer is nil")
	}
	if app.dashboardServer.Handler == nil {
		t.Fatal("dashboardServer.Handler is nil")
	}
	if app.dashboardServer.Handler != app.dashboardHandler {
		t.Fatal("dashboardServer.Handler was not wired to dashboardHandler")
	}
}

func TestNew_StartsUnconfiguredWithoutRaftState(t *testing.T) {
	dir := t.TempDir()
	cfg := boot.AppConfig{
		RaftDataDir: filepath.Join(dir, "raft"),
	}
	app, err := New(cfg, filepath.Join(dir, "app.json"), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()
	if app.raftNode != nil {
		t.Fatal("raftNode is not nil, want unconfigured node")
	}
	if got := app.Snapshot().RaftIdentity.NodeID; got != "" {
		t.Fatalf("RaftNodeID = %q, want empty", got)
	}
}

func TestBootstrapClusterUsesRaftNodeConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := boot.AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
		RaftDataDir:         filepath.Join(dir, "raft"),
	}

	app, err := New(cfg, filepath.Join(dir, "app.json"), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = app.BootstrapCluster(context.Background(), dashboard.ClusterBootstrapRequest{
		NodeID:            "node-1",
		RaftBindAddr:      "not-a-valid-address",
		RaftAdvertiseAddr: "127.0.0.1:7001",
	})
	if err == nil {
		t.Fatal("BootstrapCluster() error = nil, want raft bind error")
	}
}

func TestRaftTimingFromConfig(t *testing.T) {
	cfg := config.RaftConfig{
		Timing: config.RaftTiming{
			HeartbeatTimeout:   "3s",
			ElectionTimeout:    "5s",
			LeaderLeaseTimeout: "2s",
			CommitTimeout:      "250ms",
		},
	}

	timing, err := raftTimingFromConfig(cfg)
	if err != nil {
		t.Fatalf("raftTimingFromConfig() error = %v", err)
	}
	if timing.HeartbeatTimeout != 3*time.Second ||
		timing.ElectionTimeout != 5*time.Second ||
		timing.LeaderLeaseTimeout != 2*time.Second ||
		timing.CommitTimeout != 250*time.Millisecond {
		t.Fatalf("raft timing = %+v", timing)
	}
}

func TestPostRaftJoinPostsExpectedJSON(t *testing.T) {
	var gotPath string
	var gotBody raftJoinRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode join body error = %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := postRaftJoin(context.Background(), server.Client(), []string{server.URL}, "node-2", "127.0.0.1:7002"); err != nil {
		t.Fatalf("postRaftJoin() error = %v", err)
	}
	if gotPath != "/api/cluster/join" {
		t.Fatalf("path = %q, want /api/cluster/join", gotPath)
	}
	if gotBody.NodeID != "node-2" || gotBody.RaftAddress != "127.0.0.1:7002" {
		t.Fatalf("join body = %+v, want node-2/127.0.0.1:7002", gotBody)
	}
}

func TestPostRaftJoinTriesJoinAddrsInOrder(t *testing.T) {
	firstCalled := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalled = true
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"not_raft_leader","message":"not leader"}`))
	}))
	defer first.Close()

	secondCalled := false
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer second.Close()

	addrs := []string{first.URL, second.URL}
	err := postRaftJoin(context.Background(), first.Client(), addrs, "node-2", "127.0.0.1:7002")
	if err != nil {
		t.Fatalf("postRaftJoin() error = %v", err)
	}
	if !firstCalled || !secondCalled {
		t.Fatalf("called first=%v second=%v, want both true", firstCalled, secondCalled)
	}
}

func TestFetchClusterRaftTimingFromPeer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/cluster" {
			t.Fatalf("request = %s %s, want GET /api/cluster", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"raft_timing":{"heartbeat_timeout":"3s","election_timeout":"5s"}}`))
	}))
	defer server.Close()

	timing, err := fetchClusterRaftTiming(context.Background(), server.Client(), []string{server.URL})
	if err != nil {
		t.Fatalf("fetchClusterRaftTiming() error = %v", err)
	}
	if timing == nil || timing.HeartbeatTimeout != "3s" {
		t.Fatalf("timing = %+v, want heartbeat 3s", timing)
	}
}

func TestFetchClusterRaftTimingTriesPeersInOrder(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"raft_timing":{"heartbeat_timeout":"250ms"}}`))
	}))
	defer second.Close()

	timing, err := fetchClusterRaftTiming(context.Background(), first.Client(), []string{first.URL, second.URL})
	if err != nil {
		t.Fatalf("fetchClusterRaftTiming() error = %v", err)
	}
	if timing == nil || timing.HeartbeatTimeout != "250ms" {
		t.Fatalf("timing = %+v, want second peer timing", timing)
	}
}

func TestClusterStatusURLAcceptsEndpointAddress(t *testing.T) {
	got, err := clusterStatusURL("http://leader:9090/api/cluster")
	if err != nil {
		t.Fatalf("clusterStatusURL() error = %v", err)
	}
	if got != "http://leader:9090/api/cluster" {
		t.Fatalf("clusterStatusURL() = %q, want endpoint unchanged", got)
	}
}

func TestNewRaftJoinHTTPClientHasTimeout(t *testing.T) {
	client := newRaftJoinHTTPClient()
	if client == nil {
		t.Fatal("newRaftJoinHTTPClient() returned nil")
	}
	if client.Timeout <= 0 {
		t.Fatalf("client.Timeout = %s, want positive timeout", client.Timeout)
	}
}

func TestRaftJoinURLAcceptsEndpointAddress(t *testing.T) {
	got, err := raftJoinURL("http://leader:9090/api/cluster/join")
	if err != nil {
		t.Fatalf("raftJoinURL() error = %v", err)
	}
	if got != "http://leader:9090/api/cluster/join" {
		t.Fatalf("raftJoinURL() = %q, want endpoint unchanged", got)
	}
}
