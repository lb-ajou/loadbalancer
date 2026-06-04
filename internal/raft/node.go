package raftstore

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

type NodeOptions struct {
	NodeID             string
	BindAddr           string
	AdvertiseAddr      string
	DataDir            string
	Bootstrap          bool
	FSM                *FSM
	HeartbeatTimeout   time.Duration
	ElectionTimeout    time.Duration
	LeaderLeaseTimeout time.Duration
	CommitTimeout      time.Duration
}

type Node struct {
	Raft             *raft.Raft
	HasExistingState bool
	logStore         *raftboltdb.BoltStore
	stableStore      *raftboltdb.BoltStore
	transport        *raft.NetworkTransport
}

func (n *Node) Shutdown() error {
	if n == nil {
		return nil
	}

	var errs []error
	if n.Raft != nil {
		if err := n.Raft.Shutdown().Error(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.transport != nil {
		if err := n.transport.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.logStore != nil {
		if err := n.logStore.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.stableStore != nil {
		if err := n.stableStore.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func NewNode(opts NodeOptions) (*Node, error) {
	if opts.FSM == nil {
		return nil, fmt.Errorf("raft FSM is required")
	}
	if opts.NodeID == "" {
		return nil, fmt.Errorf("raft node ID is required")
	}
	if opts.BindAddr == "" {
		return nil, fmt.Errorf("raft bind address is required")
	}
	if opts.AdvertiseAddr == "" {
		return nil, fmt.Errorf("raft advertise address is required")
	}
	if opts.DataDir == "" {
		return nil, fmt.Errorf("raft data dir is required")
	}
	if err := os.MkdirAll(opts.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create raft data dir: %w", err)
	}

	advertiseAddr, err := net.ResolveTCPAddr("tcp", opts.AdvertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve raft advertise address: %w", err)
	}

	transport, err := raft.NewTCPTransport(opts.BindAddr, advertiseAddr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("create raft transport: %w", err)
	}
	node := &Node{transport: transport}

	logStore, err := raftboltdb.NewBoltStore(filepath.Join(opts.DataDir, "raft-log.bolt"))
	if err != nil {
		_ = node.Shutdown()
		return nil, fmt.Errorf("create raft log store: %w", err)
	}
	node.logStore = logStore

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(opts.DataDir, "raft-stable.bolt"))
	if err != nil {
		_ = node.Shutdown()
		return nil, fmt.Errorf("create raft stable store: %w", err)
	}
	node.stableStore = stableStore

	snapshotStore, err := raft.NewFileSnapshotStore(filepath.Join(opts.DataDir, "snapshots"), 2, os.Stderr)
	if err != nil {
		_ = node.Shutdown()
		return nil, fmt.Errorf("create raft snapshot store: %w", err)
	}

	raftConfig := newRaftConfig(opts)

	raftNode, err := raft.NewRaft(raftConfig, opts.FSM, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		_ = node.Shutdown()
		return nil, fmt.Errorf("create raft node: %w", err)
	}
	node.Raft = raftNode

	hasState, err := raft.HasExistingState(logStore, stableStore, snapshotStore)
	if err != nil {
		_ = node.Shutdown()
		return nil, fmt.Errorf("inspect raft state: %w", err)
	}
	node.HasExistingState = hasState
	if opts.Bootstrap && !hasState {
		future := node.Raft.BootstrapCluster(raft.Configuration{
			Servers: []raft.Server{{
				ID:      raft.ServerID(opts.NodeID),
				Address: raft.ServerAddress(opts.AdvertiseAddr),
			}},
		})
		if err := future.Error(); err != nil {
			_ = node.Shutdown()
			return nil, fmt.Errorf("bootstrap raft cluster: %w", err)
		}
	}

	return node, nil
}

func HasExistingState(dataDir string) (bool, error) {
	if dataDir == "" {
		return false, fmt.Errorf("raft data dir is required")
	}
	if _, err := os.Stat(dataDir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect raft data dir: %w", err)
	}
	logStore, stableStore, snapshotStore, err := openStateStores(dataDir)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = logStore.Close()
		_ = stableStore.Close()
	}()
	return raft.HasExistingState(logStore, stableStore, snapshotStore)
}

func openStateStores(dataDir string) (*raftboltdb.BoltStore, *raftboltdb.BoltStore, raft.SnapshotStore, error) {
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-log.bolt"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open raft log store: %w", err)
	}
	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-stable.bolt"))
	if err != nil {
		_ = logStore.Close()
		return nil, nil, nil, fmt.Errorf("open raft stable store: %w", err)
	}
	snapshotStore, err := raft.NewFileSnapshotStore(filepath.Join(dataDir, "snapshots"), 2, os.Stderr)
	if err != nil {
		_ = logStore.Close()
		_ = stableStore.Close()
		return nil, nil, nil, fmt.Errorf("open raft snapshot store: %w", err)
	}
	return logStore, stableStore, snapshotStore, nil
}

func newRaftConfig(opts NodeOptions) *raft.Config {
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(opts.NodeID)
	applyRaftTiming(raftConfig, opts)
	return raftConfig
}

func applyRaftTiming(raftConfig *raft.Config, opts NodeOptions) {
	if opts.HeartbeatTimeout > 0 {
		raftConfig.HeartbeatTimeout = opts.HeartbeatTimeout
	}
	if opts.ElectionTimeout > 0 {
		raftConfig.ElectionTimeout = opts.ElectionTimeout
	}
	if opts.LeaderLeaseTimeout > 0 {
		raftConfig.LeaderLeaseTimeout = opts.LeaderLeaseTimeout
	}
	if opts.CommitTimeout > 0 {
		raftConfig.CommitTimeout = opts.CommitTimeout
	}
}
