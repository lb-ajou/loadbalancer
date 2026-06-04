package app

import (
	"context"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/dashboard"
)

func (a *App) raftClusterStatus(ctx context.Context) dashboard.ClusterView {
	node := a.raftNode.Raft
	leaderAddr, leaderID := node.LeaderWithID()
	stats := node.Stats()
	return dashboard.ClusterView{
		Enabled:      true,
		QuorumStatus: quorumStatus(ctx, node),
		Leader:       dashboard.ClusterLeaderView{ID: string(leaderID), Address: string(leaderAddr)},
		Local:        localClusterView(a, stats),
		Members:      clusterMembers(node, leaderID, leaderAddr),
		RaftTiming:   clusterRaftTimingView(a),
	}
}

func clusterRaftTimingView(a *App) *dashboard.ClusterRaftTimingView {
	timing := a.state.Snapshot().RaftTiming
	if timing.HeartbeatTimeout == "" && timing.ElectionTimeout == "" &&
		timing.LeaderLeaseTimeout == "" && timing.CommitTimeout == "" {
		return nil
	}
	return &dashboard.ClusterRaftTimingView{
		HeartbeatTimeout:   timing.HeartbeatTimeout,
		ElectionTimeout:    timing.ElectionTimeout,
		LeaderLeaseTimeout: timing.LeaderLeaseTimeout,
		CommitTimeout:      timing.CommitTimeout,
	}
}

func localClusterView(a *App, stats map[string]string) dashboard.ClusterLocalView {
	identity := a.state.Snapshot().RaftIdentity
	return dashboard.ClusterLocalView{
		ID:           identity.NodeID,
		Address:      identity.AdvertiseAddr,
		State:        raftStateString(a.raftNode.Raft.State()),
		LastLogIndex: stats["last_log_index"],
		CommitIndex:  stats["commit_index"],
		AppliedIndex: stats["applied_index"],
		Term:         stats["term"],
	}
}

func clusterMembers(node *raft.Raft, leaderID raft.ServerID, leaderAddr raft.ServerAddress) []dashboard.ClusterMemberView {
	future := node.GetConfiguration()
	if err := future.Error(); err != nil {
		return nil
	}
	servers := future.Configuration().Servers
	members := make([]dashboard.ClusterMemberView, 0, len(servers))
	for _, server := range servers {
		members = append(members, clusterMember(server, leaderID, leaderAddr))
	}
	return members
}

func clusterMember(server raft.Server, leaderID raft.ServerID, leaderAddr raft.ServerAddress) dashboard.ClusterMemberView {
	return dashboard.ClusterMemberView{
		ID:       string(server.ID),
		Address:  string(server.Address),
		Role:     suffrageString(server.Suffrage),
		IsLeader: server.ID == leaderID || server.Address == leaderAddr,
	}
}

func quorumStatus(ctx context.Context, node *raft.Raft) string {
	if node.State() != raft.Leader {
		return "unknown"
	}
	if err := verifyLeaderWithTimeout(ctx, node); err != nil {
		return "unavailable"
	}
	return "available"
}

func verifyLeaderWithTimeout(ctx context.Context, node *raft.Raft) error {
	verifyCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- node.VerifyLeader().Error()
	}()
	select {
	case err := <-done:
		return err
	case <-verifyCtx.Done():
		return verifyCtx.Err()
	}
}

func raftStateString(state raft.RaftState) string {
	switch state {
	case raft.Leader:
		return "leader"
	case raft.Follower:
		return "follower"
	case raft.Candidate:
		return "candidate"
	case raft.Shutdown:
		return "shutdown"
	default:
		return "unknown"
	}
}

func suffrageString(suffrage raft.ServerSuffrage) string {
	switch suffrage {
	case raft.Voter:
		return "voter"
	case raft.Nonvoter:
		return "nonvoter"
	case raft.Staging:
		return "staging"
	default:
		return "unknown"
	}
}
