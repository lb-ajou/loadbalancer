package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunClusterStatusPrintsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/node/cluster-status" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"unconfigured"}`)
	}))
	defer server.Close()

	var stdout strings.Builder
	err := Run(context.Background(), Options{
		Args:   []string{"cluster", "status", "--dashboard", server.URL},
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != `{"state":"unconfigured"}` {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunClusterBootstrapPostsRequest(t *testing.T) {
	var got clusterBootstrapRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/cluster/bootstrap" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	args := []string{
		"cluster", "bootstrap",
		"--dashboard", server.URL,
		"--node-id", "node-1",
		"--raft-bind", "0.0.0.0:7001",
		"--raft-advertise", "10.0.0.11:7001",
		"--vip-interface", "eth0",
		"--vip-address", "10.0.0.100/24",
		"--garp-count", "3",
		"--garp-interval", "250ms",
		"--raft-heartbeat-timeout", "3s",
		"--raft-election-timeout", "5s",
	}
	err := Run(context.Background(), Options{Args: args, Stdout: io.Discard})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertBootstrapRequest(t, got)
}

func TestRunClusterJoinPostsPeers(t *testing.T) {
	var got clusterJoinRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/node/join-cluster" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	args := []string{
		"cluster", "join",
		"--dashboard", server.URL,
		"--node-id", "node-2",
		"--raft-advertise", "10.0.0.12:7002",
		"--vip-interface", "eth0",
		"--peer", "http://10.0.0.11:9090",
		"--peer", "http://10.0.0.13:9090",
	}
	err := Run(context.Background(), Options{Args: args, Stdout: io.Discard})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertJoinRequest(t, got)
}

func TestRunClusterJoinRequiresPeer(t *testing.T) {
	err := Run(context.Background(), Options{
		Args: []string{
			"cluster", "join",
			"--node-id", "node-2",
			"--raft-advertise", "10.0.0.12:7002",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "at least one --peer is required") {
		t.Fatalf("Run() error = %v", err)
	}
}

func assertBootstrapRequest(t *testing.T, got clusterBootstrapRequest) {
	t.Helper()
	if got.NodeID != "node-1" || got.RaftAdvertiseAddr != "10.0.0.11:7001" {
		t.Fatalf("bootstrap request = %+v", got)
	}
	if got.RaftBindAddr != "0.0.0.0:7001" || got.VIPInterface != "eth0" {
		t.Fatalf("bootstrap local fields = %+v", got)
	}
	if got.VIP == nil || got.VIP.Address != "10.0.0.100/24" {
		t.Fatalf("bootstrap VIP = %+v", got.VIP)
	}
	if got.VIP.GARPCount != 3 || got.VIP.GARPInterval != "250ms" {
		t.Fatalf("bootstrap VIP options = %+v", got.VIP)
	}
	if got.RaftTiming == nil || got.RaftTiming.HeartbeatTimeout != "3s" {
		t.Fatalf("bootstrap raft timing = %+v", got.RaftTiming)
	}
}

func assertJoinRequest(t *testing.T, got clusterJoinRequest) {
	t.Helper()
	if got.NodeID != "node-2" || got.RaftAdvertiseAddr != "10.0.0.12:7002" {
		t.Fatalf("join request = %+v", got)
	}
	if got.VIPInterface != "eth0" {
		t.Fatalf("join VIP interface = %q", got.VIPInterface)
	}
	if len(got.Peers) != 2 || got.Peers[0] != "http://10.0.0.11:9090" {
		t.Fatalf("join peers = %v", got.Peers)
	}
}
