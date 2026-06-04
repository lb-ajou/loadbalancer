package raftstore

import (
	"testing"
	"time"

	"github.com/hashicorp/raft"
)

func TestNewRaftConfigKeepsDefaultsWhenTimingUnset(t *testing.T) {
	got := newRaftConfig(NodeOptions{NodeID: "node-1"})
	want := raft.DefaultConfig()

	if got.HeartbeatTimeout != want.HeartbeatTimeout ||
		got.ElectionTimeout != want.ElectionTimeout ||
		got.LeaderLeaseTimeout != want.LeaderLeaseTimeout ||
		got.CommitTimeout != want.CommitTimeout {
		t.Fatalf("default raft timing changed: got %+v want %+v", got, want)
	}
}

func TestNewRaftConfigAppliesTiming(t *testing.T) {
	cfg := newRaftConfig(NodeOptions{
		NodeID:             "node-1",
		HeartbeatTimeout:   3 * time.Second,
		ElectionTimeout:    5 * time.Second,
		LeaderLeaseTimeout: 2 * time.Second,
		CommitTimeout:      250 * time.Millisecond,
	})

	if cfg.LocalID != raft.ServerID("node-1") {
		t.Fatalf("LocalID = %q, want node-1", cfg.LocalID)
	}
	if cfg.HeartbeatTimeout != 3*time.Second ||
		cfg.ElectionTimeout != 5*time.Second ||
		cfg.LeaderLeaseTimeout != 2*time.Second ||
		cfg.CommitTimeout != 250*time.Millisecond {
		t.Fatalf("raft timing not applied: %+v", cfg)
	}
}
