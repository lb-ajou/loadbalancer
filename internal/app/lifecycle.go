package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/dashboard"
	"reverseproxy-poc/internal/raft"
	appruntime "reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/state"
)

const localLeaderWaitTimeout = 5 * time.Second

func (a *App) BootstrapCluster(ctx context.Context, request dashboard.ClusterBootstrapRequest) error {
	a.clusterMu.Lock()
	defer a.clusterMu.Unlock()

	cfg, raftCfg, localVIP, err := a.bootstrapConfig(request)
	if err != nil {
		return err
	}
	if err := a.ensureCleanCluster(); err != nil {
		return err
	}
	store, err := a.startRaft(cfg, raftCfg, localVIP, true)
	if err != nil {
		return err
	}
	if err := a.persistLocalRaftConfig(cfg, raftCfg); err != nil {
		return err
	}
	return a.applyBootstrapClusterState(ctx, store, request)
}

func (a *App) JoinCluster(ctx context.Context, request dashboard.NodeJoinClusterRequest) error {
	a.clusterMu.Lock()
	defer a.clusterMu.Unlock()

	timing, err := fetchClusterRaftTiming(ctx, newRaftJoinHTTPClient(), request.Peers)
	if err != nil {
		return err
	}
	cfg, raftCfg, localVIP, err := a.joinConfig(request, timing)
	if err != nil {
		return err
	}
	if err := a.ensureCleanCluster(); err != nil {
		return err
	}
	node, err := a.startRaft(cfg, raftCfg, localVIP, false)
	if err != nil {
		return err
	}
	if err := postRaftJoin(ctx, newRaftJoinHTTPClient(), request.Peers, raftCfg.Identity.NodeID, raftCfg.Identity.AdvertiseAddr); err != nil {
		a.clearStartedRaft()
		_ = node.raft.Shutdown()
		return fmt.Errorf("join raft cluster: %w", err)
	}
	return a.persistLocalRaftConfig(cfg, raftCfg)
}

func (a *App) ClusterLifecycleStatus(ctx context.Context) dashboard.NodeClusterStatusView {
	a.clusterMu.Lock()
	defer a.clusterMu.Unlock()

	view := a.baseClusterLifecycleStatus()
	if a.raftNode != nil {
		view.State = "clustered"
		return view
	}
	hasState, err := raftstore.HasExistingState(a.cfg.RaftDataDir)
	if err != nil {
		view.State = "check_error"
		view.LastError = err.Error()
		return view
	}
	if hasState {
		view.State = "existing_state"
		return view
	}
	return view
}

func (a *App) baseClusterLifecycleStatus() dashboard.NodeClusterStatusView {
	return dashboard.NodeClusterStatusView{
		State:             "unconfigured",
		NodeID:            a.raftCfg.Identity.NodeID,
		RaftAdvertiseAddr: a.raftCfg.Identity.AdvertiseAddr,
		RaftDataDir:       a.cfg.RaftDataDir,
	}
}

type startedRaft struct {
	raft  *raftstore.Node
	store *raftstore.Store
}

func (a *App) startRaft(cfg boot.AppConfig, raftCfg config.RaftConfig, localVIP config.VIPConfig, bootstrap bool) (startedRaft, error) {
	fsm := raftstore.NewFSMWithConfig(cfg, a.projectRaftApply(cfg, raftCfg, localVIP))
	node, err := a.newRaftNode(cfg, raftCfg, bootstrap, fsm)
	if err != nil {
		return startedRaft{}, err
	}
	store := raftstore.NewStore(node.Raft, fsm)
	a.raftNode = node
	a.cfg = cfg
	a.raftCfg = raftCfg
	a.raftStore.Set(store)
	snapshot, err := state.ProjectSnapshot(cfg, raftCfg, localVIP, fsm.DesiredState())
	if err != nil {
		_ = node.Shutdown()
		return startedRaft{}, err
	}
	a.applyRaftSnapshot(snapshot, node.Raft)
	return startedRaft{raft: node, store: store}, nil
}

func (a *App) projectRaftApply(cfg boot.AppConfig, raftCfg config.RaftConfig, localVIP config.VIPConfig) func(state.DesiredState) {
	return func(desired state.DesiredState) {
		if a.raftNode == nil {
			return
		}
		snapshot, err := state.ProjectSnapshot(cfg, raftCfg, localVIP, desired)
		if err != nil {
			a.logger.Printf("failed to project raft configuration: %v", err)
			return
		}
		a.applyRaftSnapshot(snapshot, a.raftNode.Raft)
	}
}

func (a *App) newRaftNode(cfg boot.AppConfig, raftCfg config.RaftConfig, bootstrap bool, fsm *raftstore.FSM) (*raftstore.Node, error) {
	timing, err := raftTimingFromConfig(raftCfg)
	if err != nil {
		return nil, err
	}
	return raftstore.NewNode(raftstore.NodeOptions{
		NodeID:             raftCfg.Identity.NodeID,
		BindAddr:           raftCfg.Identity.BindAddr,
		AdvertiseAddr:      raftCfg.Identity.AdvertiseAddr,
		DataDir:            cfg.RaftDataDir,
		Bootstrap:          bootstrap,
		FSM:                fsm,
		HeartbeatTimeout:   timing.HeartbeatTimeout,
		ElectionTimeout:    timing.ElectionTimeout,
		LeaderLeaseTimeout: timing.LeaderLeaseTimeout,
		CommitTimeout:      timing.CommitTimeout,
	})
}

func (a *App) applyBootstrapClusterState(ctx context.Context, started startedRaft, request dashboard.ClusterBootstrapRequest) error {
	if request.RaftTiming == nil && request.VIP == nil {
		return nil
	}
	if err := waitForLocalLeader(ctx, started.raft.Raft); err != nil {
		return err
	}
	if err := applyBootstrapRaftTimingState(ctx, started.store, request.RaftTiming); err != nil {
		return err
	}
	return applyBootstrapVIPState(ctx, started.store, request.VIP)
}

func applyBootstrapRaftTimingState(ctx context.Context, store *raftstore.Store, timing *dashboard.ClusterRaftTimingRequest) error {
	if timing == nil {
		return nil
	}
	return store.SetClusterRaftTiming(ctx, clusterRaftTimingConfig(timing))
}

func applyBootstrapVIPState(ctx context.Context, store *raftstore.Store, vip *dashboard.ClusterVIPRequest) error {
	if vip == nil {
		return nil
	}
	return store.SetClusterVIP(ctx, clusterVIPRequestConfig(vip))
}

func (a *App) clearStartedRaft() {
	a.raftNode = nil
	a.raftStore.Set(nil)
}

func clusterVIPRequestConfig(vip *dashboard.ClusterVIPRequest) state.ClusterVIPConfig {
	return state.NormalizeClusterVIP(state.ClusterVIPConfig{
		Address:           vip.Address,
		GARPCount:         vip.GARPCount,
		GARPInterval:      vip.GARPInterval,
		AcquireDelay:      vip.AcquireDelay,
		ReleaseOnShutdown: vip.ReleaseOnShutdown,
	})
}

func clusterRaftTimingConfig(timing *dashboard.ClusterRaftTimingRequest) state.ClusterRaftTimingConfig {
	return state.ClusterRaftTimingConfig{
		HeartbeatTimeout:   timing.HeartbeatTimeout,
		ElectionTimeout:    timing.ElectionTimeout,
		LeaderLeaseTimeout: timing.LeaderLeaseTimeout,
		CommitTimeout:      timing.CommitTimeout,
	}
}

func waitForLocalLeader(ctx context.Context, node *raft.Raft) error {
	ctx, cancel := context.WithTimeout(ctx, localLeaderWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for node.State() != raft.Leader {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func (a *App) ensureCleanCluster() error {
	if a.raftNode != nil {
		return state.NewClusterAlreadyConfiguredError()
	}
	hasState, err := raftstore.HasExistingState(a.cfg.RaftDataDir)
	if err != nil {
		return err
	}
	if hasState {
		return state.NewClusterAlreadyConfiguredError()
	}
	return nil
}

func (a *App) bootstrapConfig(request dashboard.ClusterBootstrapRequest) (boot.AppConfig, config.RaftConfig, config.VIPConfig, error) {
	cfg, raftCfg := a.baseLifecycleConfig(request.NodeID, request.RaftBindAddr, request.RaftAdvertiseAddr)
	applyBootstrapRaftTiming(&raftCfg, request.RaftTiming)
	localVIP := config.VIPConfig{Interface: request.VIPInterface}
	if request.VIP != nil {
		localVIP = applyBootstrapVIP(localVIP, request.VIP)
	}
	normalized, err := boot.Normalize(cfg)
	return normalized, raftCfg, localVIP, err
}

func applyBootstrapVIP(localVIP config.VIPConfig, request *dashboard.ClusterVIPRequest) config.VIPConfig {
	vip := clusterVIPRequestConfig(request)
	localVIP.Address = vip.Address
	localVIP.GARPCount = vip.GARPCount
	localVIP.GARPInterval = vip.GARPInterval
	localVIP.AcquireDelay = vip.AcquireDelay
	localVIP.ReleaseOnShutdown = vip.ReleaseOnShutdown
	return localVIP
}

func applyBootstrapRaftTiming(cfg *config.RaftConfig, timing *dashboard.ClusterRaftTimingRequest) {
	if timing == nil {
		return
	}
	cfg.Timing.HeartbeatTimeout = timing.HeartbeatTimeout
	cfg.Timing.ElectionTimeout = timing.ElectionTimeout
	cfg.Timing.LeaderLeaseTimeout = timing.LeaderLeaseTimeout
	cfg.Timing.CommitTimeout = timing.CommitTimeout
}

func (a *App) joinConfig(request dashboard.NodeJoinClusterRequest, timing *dashboard.ClusterRaftTimingView) (boot.AppConfig, config.RaftConfig, config.VIPConfig, error) {
	cfg, raftCfg := a.baseLifecycleConfig(request.NodeID, request.RaftBindAddr, request.RaftAdvertiseAddr)
	applyJoinRaftTiming(&raftCfg, timing)
	localVIP := config.VIPConfig{Interface: request.VIPInterface}
	normalized, err := boot.Normalize(cfg)
	return normalized, raftCfg, localVIP, err
}

func applyJoinRaftTiming(cfg *config.RaftConfig, timing *dashboard.ClusterRaftTimingView) {
	if timing == nil {
		return
	}
	cfg.Timing.HeartbeatTimeout = timing.HeartbeatTimeout
	cfg.Timing.ElectionTimeout = timing.ElectionTimeout
	cfg.Timing.LeaderLeaseTimeout = timing.LeaderLeaseTimeout
	cfg.Timing.CommitTimeout = timing.CommitTimeout
}

func (a *App) baseLifecycleConfig(nodeID, bindAddr, advertiseAddr string) (boot.AppConfig, config.RaftConfig) {
	cfg := a.cfg
	raftCfg := a.raftCfg
	raftCfg.Identity.NodeID = nodeID
	raftCfg.Identity.AdvertiseAddr = advertiseAddr
	raftCfg.Identity.BindAddr = bindAddr
	if raftCfg.Identity.BindAddr == "" && raftCfg.Identity.AdvertiseAddr != "" {
		raftCfg.Identity.BindAddr = defaultRaftBindAddrForAdvertise(raftCfg.Identity.AdvertiseAddr)
	}
	return cfg, raftCfg
}

func defaultRaftBindAddrForAdvertise(advertiseAddr string) string {
	_, port, err := net.SplitHostPort(advertiseAddr)
	if err != nil {
		return net.JoinHostPort("0.0.0.0", "7001")
	}
	return net.JoinHostPort("0.0.0.0", port)
}

func (a *App) persistLocalRaftConfig(cfg boot.AppConfig, raftCfg config.RaftConfig) error {
	return raftstore.SaveLocalNodeConfig(cfg.RaftDataDir, raftstore.LocalNodeConfig{
		NodeID:        raftCfg.Identity.NodeID,
		BindAddr:      raftCfg.Identity.BindAddr,
		AdvertiseAddr: raftCfg.Identity.AdvertiseAddr,
	})
}

func (a *App) applyRaftSnapshot(snapshot appruntime.Snapshot, raftNode *raft.Raft) {
	a.state.Swap(snapshot)
	a.swapHealthChecker(snapshot.Upstreams)
	a.reconfigureVIP(snapshot.VIP, raftNode)
}

type raftStoreProxy struct {
	mu    sync.RWMutex
	store *raftstore.Store
}

func (p *raftStoreProxy) Set(store *raftstore.Store) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store = store
}

func (p *raftStoreProxy) current() (*raftstore.Store, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.store == nil {
		return nil, state.NewClusterNotConfiguredError()
	}
	return p.store, nil
}

func (p *raftStoreProxy) GetConfig(ctx context.Context) (state.AppliedProxyConfig, error) {
	store, err := p.current()
	if err != nil {
		return state.AppliedProxyConfig{}, err
	}
	return store.GetConfig(ctx)
}

func (p *raftStoreProxy) ReplaceConfig(ctx context.Context, cfg spec.Config) (state.AppliedProxyConfig, error) {
	store, err := p.current()
	if err != nil {
		return state.AppliedProxyConfig{}, err
	}
	return store.ReplaceConfig(ctx, cfg)
}
