package cli

import (
	"errors"
	"flag"
)

type bootstrapInput struct {
	dashboard          string
	nodeID             string
	raftBindAddr       string
	raftAdvertiseAddr  string
	vipInterface       string
	vipAddress         string
	garpCount          int
	garpInterval       string
	acquireDelay       string
	releaseOnShutdown  bool
	heartbeatTimeout   string
	electionTimeout    string
	leaderLeaseTimeout string
	commitTimeout      string
}

type joinInput struct {
	dashboard         string
	nodeID            string
	raftBindAddr      string
	raftAdvertiseAddr string
	vipInterface      string
	peers             stringList
}

func bootstrapFlags(fs *flag.FlagSet) *bootstrapInput {
	input := &bootstrapInput{}
	fs.StringVar(&input.dashboard, "dashboard", "http://localhost:9090", "dashboard API base URL")
	fs.StringVar(&input.nodeID, "node-id", "", "raft node id")
	fs.StringVar(&input.raftBindAddr, "raft-bind", "", "raft bind address")
	fs.StringVar(&input.raftAdvertiseAddr, "raft-advertise", "", "raft advertise address")
	fs.StringVar(&input.vipInterface, "vip-interface", "", "local VIP network interface")
	fs.StringVar(&input.vipAddress, "vip-address", "", "cluster VIP CIDR")
	fs.IntVar(&input.garpCount, "garp-count", 0, "GARP repeat count")
	fs.StringVar(&input.garpInterval, "garp-interval", "", "GARP interval duration")
	fs.StringVar(&input.acquireDelay, "acquire-delay", "", "VIP acquire delay duration")
	fs.BoolVar(&input.releaseOnShutdown, "release-on-shutdown", false, "release VIP on shutdown")
	fs.StringVar(&input.heartbeatTimeout, "raft-heartbeat-timeout", "", "raft heartbeat timeout")
	fs.StringVar(&input.electionTimeout, "raft-election-timeout", "", "raft election timeout")
	fs.StringVar(&input.leaderLeaseTimeout, "raft-leader-lease-timeout", "", "raft leader lease timeout")
	fs.StringVar(&input.commitTimeout, "raft-commit-timeout", "", "raft commit timeout")
	return input
}

func joinFlags(fs *flag.FlagSet) *joinInput {
	input := &joinInput{}
	fs.StringVar(&input.dashboard, "dashboard", "http://localhost:9090", "dashboard API base URL")
	fs.StringVar(&input.nodeID, "node-id", "", "raft node id")
	fs.StringVar(&input.raftBindAddr, "raft-bind", "", "raft bind address")
	fs.StringVar(&input.raftAdvertiseAddr, "raft-advertise", "", "raft advertise address")
	fs.StringVar(&input.vipInterface, "vip-interface", "", "local VIP network interface")
	fs.Var(&input.peers, "peer", "cluster peer dashboard URL")
	return input
}

func (input bootstrapInput) request() (clusterBootstrapRequest, error) {
	if input.nodeID == "" {
		return clusterBootstrapRequest{}, errors.New("--node-id is required")
	}
	if input.raftAdvertiseAddr == "" {
		return clusterBootstrapRequest{}, errors.New("--raft-advertise is required")
	}
	if input.vipAddress != "" && input.vipInterface == "" {
		return clusterBootstrapRequest{}, errors.New("--vip-interface is required when --vip-address is set")
	}

	request := clusterBootstrapRequest{
		NodeID:            input.nodeID,
		RaftBindAddr:      input.raftBindAddr,
		RaftAdvertiseAddr: input.raftAdvertiseAddr,
		VIPInterface:      input.vipInterface,
	}
	if input.vipAddress != "" {
		request.VIP = input.vipRequest()
	}
	if timing := input.raftTimingRequest(); timing != nil {
		request.RaftTiming = timing
	}
	return request, nil
}

func (input bootstrapInput) raftTimingRequest() *clusterRaftTimingRequest {
	if input.heartbeatTimeout == "" && input.electionTimeout == "" &&
		input.leaderLeaseTimeout == "" && input.commitTimeout == "" {
		return nil
	}
	return &clusterRaftTimingRequest{
		HeartbeatTimeout:   input.heartbeatTimeout,
		ElectionTimeout:    input.electionTimeout,
		LeaderLeaseTimeout: input.leaderLeaseTimeout,
		CommitTimeout:      input.commitTimeout,
	}
}

func (input bootstrapInput) vipRequest() *clusterVIPRequest {
	return &clusterVIPRequest{
		Address:           input.vipAddress,
		GARPCount:         input.garpCount,
		GARPInterval:      input.garpInterval,
		AcquireDelay:      input.acquireDelay,
		ReleaseOnShutdown: input.releaseOnShutdown,
	}
}

func (input joinInput) request() (clusterJoinRequest, error) {
	if input.nodeID == "" {
		return clusterJoinRequest{}, errors.New("--node-id is required")
	}
	if input.raftAdvertiseAddr == "" {
		return clusterJoinRequest{}, errors.New("--raft-advertise is required")
	}
	if len(input.peers) == 0 {
		return clusterJoinRequest{}, errors.New("at least one --peer is required")
	}
	return clusterJoinRequest{
		NodeID:            input.nodeID,
		RaftBindAddr:      input.raftBindAddr,
		RaftAdvertiseAddr: input.raftAdvertiseAddr,
		VIPInterface:      input.vipInterface,
		Peers:             []string(input.peers),
	}, nil
}
