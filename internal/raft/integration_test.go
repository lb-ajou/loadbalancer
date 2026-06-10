package raftstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/raft"

	"loadbalancer/internal/spec"
	control "loadbalancer/internal/state"
)

func TestIntegrationThreeNodeClusterReplicatesConfig(t *testing.T) {
	cluster := newInmemCluster(t, 3)
	defer cluster.Close()

	leader := cluster.LeaderStore(t)
	if _, err := leader.ReplaceConfig(context.Background(), validIntegrationConfig()); err != nil {
		t.Fatalf("ReplaceConfig() error = %v", err)
	}

	eventually(t, 3*time.Second, func() bool {
		for _, store := range cluster.stores {
			cfg, err := store.GetConfig(context.Background())
			if err != nil {
				return false
			}
			if len(cfg.Routes) != 1 || cfg.Routes[0].ID != "r-api" {
				return false
			}
			if _, ok := cfg.UpstreamPools["pool-api"]; !ok {
				return false
			}
		}
		return true
	})
}

func TestIntegrationFollowerRejectsWriteWithLeader(t *testing.T) {
	cluster := newInmemCluster(t, 3)
	defer cluster.Close()

	follower := cluster.FollowerStore(t)
	_, err := follower.ReplaceConfig(context.Background(), validIntegrationConfig())
	if err == nil {
		t.Fatal("ReplaceConfig() error = nil, want not leader")
	}
	if !control.IsNotLeader(err) {
		t.Fatalf("ReplaceConfig() error = %v, want not leader", err)
	}
}

func validIntegrationConfig() spec.Config {
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

type inmemCluster struct {
	nodes      []*raft.Raft
	fsms       []*FSM
	stores     []*Store
	transports []*raft.InmemTransport
}

func newInmemCluster(t *testing.T, size int) *inmemCluster {
	t.Helper()

	cluster := &inmemCluster{}
	configuration := raft.Configuration{
		Servers: make([]raft.Server, 0, size),
	}
	logStores := make([]*raft.InmemStore, 0, size)
	stableStores := make([]*raft.InmemStore, 0, size)
	snapshotStores := make([]*raft.DiscardSnapshotStore, 0, size)

	for i := 0; i < size; i++ {
		serverID := raft.ServerID(fmt.Sprintf("node-%d", i))
		address, transport := raft.NewInmemTransport(raft.ServerAddress(serverID))

		cluster.fsms = append(cluster.fsms, NewFSM())
		cluster.transports = append(cluster.transports, transport)
		logStores = append(logStores, raft.NewInmemStore())
		stableStores = append(stableStores, raft.NewInmemStore())
		snapshotStores = append(snapshotStores, raft.NewDiscardSnapshotStore())
		configuration.Servers = append(configuration.Servers, raft.Server{
			Suffrage: raft.Voter,
			ID:       serverID,
			Address:  address,
		})
	}

	for _, transport := range cluster.transports {
		for _, peer := range cluster.transports {
			if transport.LocalAddr() == peer.LocalAddr() {
				continue
			}
			transport.Connect(peer.LocalAddr(), peer)
		}
	}

	for i := 0; i < size; i++ {
		config := raft.DefaultConfig()
		config.LocalID = configuration.Servers[i].ID
		config.HeartbeatTimeout = 50 * time.Millisecond
		config.ElectionTimeout = 50 * time.Millisecond
		config.LeaderLeaseTimeout = 50 * time.Millisecond
		config.CommitTimeout = 5 * time.Millisecond

		if err := raft.BootstrapCluster(config, logStores[i], stableStores[i], snapshotStores[i], cluster.transports[i], configuration); err != nil {
			t.Fatalf("BootstrapCluster() error = %v", err)
		}

		node, err := raft.NewRaft(config, cluster.fsms[i], logStores[i], stableStores[i], snapshotStores[i], cluster.transports[i])
		if err != nil {
			t.Fatalf("NewRaft() error = %v", err)
		}
		cluster.nodes = append(cluster.nodes, node)
		cluster.stores = append(cluster.stores, NewStore(node, cluster.fsms[i]))
	}

	cluster.LeaderStore(t)

	return cluster
}

func (c *inmemCluster) LeaderStore(t *testing.T) *Store {
	t.Helper()

	var store *Store
	eventually(t, 3*time.Second, func() bool {
		for i, node := range c.nodes {
			if node.State() == raft.Leader {
				store = c.stores[i]
				return true
			}
		}
		return false
	})
	return store
}

func (c *inmemCluster) FollowerStore(t *testing.T) *Store {
	t.Helper()

	var store *Store
	eventually(t, 3*time.Second, func() bool {
		for i, node := range c.nodes {
			if node.State() == raft.Follower {
				store = c.stores[i]
				return true
			}
		}
		return false
	})
	return store
}

func (c *inmemCluster) Close() {
	for _, node := range c.nodes {
		_ = node.Shutdown().Error()
	}
	for _, transport := range c.transports {
		_ = transport.Close()
	}
}

func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fn() {
		return
	}
	t.Fatalf("condition not met within %s", timeout)
}
