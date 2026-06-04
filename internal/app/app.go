package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/admin"
	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/dashboard"
	"reverseproxy-poc/internal/proxy"
	"reverseproxy-poc/internal/raft"
	"reverseproxy-poc/internal/raftstate"
	appruntime "reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/state"
	"reverseproxy-poc/internal/upstream"
	internalvip "reverseproxy-poc/internal/vip"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
)

const raftJoinTimeout = 30 * time.Second

type App struct {
	logger           *log.Logger
	cfg              boot.AppConfig
	raftCfg          raftstate.Config
	state            *appruntime.State
	mu               sync.Mutex
	clusterMu        sync.Mutex
	runCtx           context.Context
	healthCtx        context.Context
	healthCancel     context.CancelFunc
	healthChecker    *upstream.Checker
	proxyHandler     http.Handler
	dashboardHandler http.Handler
	proxyServer      *http.Server
	dashboardServer  *http.Server
	raftNode         *raftstore.Node
	raftStore        *raftStoreProxy
	vipController    vipRunner
	vipCancel        context.CancelFunc
	vipDone          <-chan struct{}
}

func New(cfg boot.AppConfig, _ string, logger *log.Logger) (*App, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	normalizedCfg, err := boot.Normalize(cfg)
	if err != nil {
		return nil, err
	}
	return newUnconfiguredApp(normalizedCfg, logger)
}

func newUnconfiguredApp(cfg boot.AppConfig, logger *log.Logger) (*App, error) {
	snapshot, err := state.ProjectSnapshot(cfg, raftstate.Config{}, vipruntime.Config{}, state.DesiredState{})
	if err != nil {
		return nil, err
	}

	state := appruntime.NewState(snapshot)
	app := newApp(cfg, logger, state, snapshot)
	app.raftStore = &raftStoreProxy{}
	app.dashboardHandler = dashboard.NewHandlerWithProviders(state, admin.NewWithConfigState(app.raftStore), app, app, app, app)
	app.proxyServer = newServer(cfg.ProxyListenAddr, app.proxyHandler)
	app.dashboardServer = newServer(cfg.DashboardListenAddr, app.dashboardHandler)

	hasState, err := raftstore.HasExistingState(cfg.RaftDataDir)
	if err != nil {
		return nil, err
	}
	if hasState {
		restoreRaftCfg, err := app.restoreRaftConfig(cfg)
		if err != nil {
			return nil, err
		}
		if _, err := app.startRaft(cfg, restoreRaftCfg, vipruntime.Config{}, false); err != nil {
			return nil, err
		}
	}
	return app, nil
}

func (a *App) restoreRaftConfig(cfg boot.AppConfig) (raftstate.Config, error) {
	localCfg, ok, err := raftstore.LoadLocalNodeConfig(cfg.RaftDataDir)
	if err != nil {
		return raftstate.Config{}, err
	}
	if !ok {
		return raftstate.Config{}, nil
	}
	return raftstate.Config{Identity: raftstate.Identity{
		NodeID:        localCfg.NodeID,
		BindAddr:      localCfg.BindAddr,
		AdvertiseAddr: localCfg.AdvertiseAddr,
	}}, nil
}

func newApp(cfg boot.AppConfig, logger *log.Logger, state *appruntime.State, snapshot appruntime.Snapshot) *App {
	proxyHandler := proxy.NewHandler(state)
	return &App{
		logger:        logger,
		cfg:           cfg,
		state:         state,
		healthChecker: upstream.NewChecker(snapshot.Upstreams),
		proxyHandler:  proxyHandler,
		proxyServer:   newServer(cfg.ProxyListenAddr, proxyHandler),
	}
}

func (a *App) Snapshot() appruntime.Snapshot {
	return a.state.Snapshot()
}

func (a *App) ClusterStatus(ctx context.Context) dashboard.ClusterView {
	if a.raftNode == nil || a.raftNode.Raft == nil {
		return dashboard.ClusterView{Enabled: false, QuorumStatus: "disabled"}
	}
	return a.raftClusterStatus(ctx)
}

func (a *App) VIPStatus() dashboard.VIPStatusView {
	cfg := a.state.Snapshot().VIP
	view := dashboard.VIPStatusView{Enabled: cfg.Active(), Interface: cfg.Interface, Address: cfg.Address}
	if provider, ok := a.vipController.(interface{ Status() internalvip.Status }); ok {
		status := provider.Status()
		view.Owned = status.Owned
		view.LastError = status.LastError
	}
	return view
}

func (a *App) JoinRaft(ctx context.Context, nodeID, raftAddress string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRaftServer(nodeID, raftAddress); err != nil {
		return err
	}
	if a.raftNode == nil || a.raftNode.Raft == nil {
		return state.NewNotLeaderError("")
	}
	if a.raftNode.Raft.State() != raft.Leader {
		return state.NewNotLeaderError(string(a.raftNode.Raft.Leader()))
	}
	err := a.raftNode.Raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(raftAddress), 0, 0).Error()
	if errors.Is(err, raft.ErrNotLeader) ||
		errors.Is(err, raft.ErrLeadershipLost) ||
		errors.Is(err, raft.ErrLeadershipTransferInProgress) {
		return state.NewNotLeaderError(string(a.raftNode.Raft.Leader()))
	}
	return err
}

func validateRaftServer(nodeID, raftAddress string) error {
	if err := state.ValidateIdentifier(nodeID, "node_id"); err != nil {
		return err
	}
	if _, err := net.ResolveTCPAddr("tcp", raftAddress); err != nil {
		return &state.StateError{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_raft_address",
			Message:    "raft address must be a host:port TCP address",
			Err:        err,
		}
	}
	return nil
}

type raftTiming struct {
	HeartbeatTimeout   time.Duration
	ElectionTimeout    time.Duration
	LeaderLeaseTimeout time.Duration
	CommitTimeout      time.Duration
}

func raftTimingFromConfig(cfg raftstate.Config) (raftTiming, error) {
	heartbeat, err := parseOptionalDuration(cfg.Timing.HeartbeatTimeout)
	if err != nil {
		return raftTiming{}, err
	}
	return raftTimingFromConfigTail(cfg, heartbeat)
}

func raftTimingFromConfigTail(cfg raftstate.Config, heartbeat time.Duration) (raftTiming, error) {
	election, err := parseOptionalDuration(cfg.Timing.ElectionTimeout)
	if err != nil {
		return raftTiming{}, err
	}
	return raftTimingFromConfigLease(cfg, heartbeat, election)
}

func raftTimingFromConfigLease(cfg raftstate.Config, heartbeat, election time.Duration) (raftTiming, error) {
	lease, err := parseOptionalDuration(cfg.Timing.LeaderLeaseTimeout)
	if err != nil {
		return raftTiming{}, err
	}
	return raftTimingFromConfigCommit(cfg, heartbeat, election, lease)
}

func raftTimingFromConfigCommit(cfg raftstate.Config, heartbeat, election, lease time.Duration) (raftTiming, error) {
	commit, err := parseOptionalDuration(cfg.Timing.CommitTimeout)
	if err != nil {
		return raftTiming{}, err
	}
	return raftTiming{
		HeartbeatTimeout:   heartbeat,
		ElectionTimeout:    election,
		LeaderLeaseTimeout: lease,
		CommitTimeout:      commit,
	}, nil
}

func parseOptionalDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

type raftJoinRequest struct {
	NodeID      string `json:"node_id"`
	RaftAddress string `json:"raft_address"`
}

type raftJoinErrorResponse struct {
	Message       string `json:"message"`
	Code          string `json:"code,omitempty"`
	LeaderAddress string `json:"leader_address,omitempty"`
}

func fetchClusterRaftTiming(ctx context.Context, client *http.Client, peers []string) (*dashboard.ClusterRaftTimingView, error) {
	if client == nil {
		client = newRaftJoinHTTPClient()
	}
	var errs []error
	for _, peer := range peers {
		timing, err := fetchClusterRaftTimingOne(ctx, client, peer)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", peer, err))
			continue
		}
		return timing, nil
	}
	return nil, clusterTimingFetchError(errs)
}

func clusterTimingFetchError(errs []error) error {
	if len(errs) == 0 {
		return fmt.Errorf("cluster peer is required")
	}
	return fmt.Errorf("fetch cluster raft timing: %w", errors.Join(errs...))
}

func fetchClusterRaftTimingOne(ctx context.Context, client *http.Client, peer string) (*dashboard.ClusterRaftTimingView, error) {
	endpoint, err := clusterStatusURL(peer)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeClusterRaftTiming(resp)
}

func decodeClusterRaftTiming(resp *http.Response) (*dashboard.ClusterRaftTimingView, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cluster status request failed with status %d", resp.StatusCode)
	}
	var view dashboard.ClusterView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		return nil, err
	}
	return view.RaftTiming, nil
}

func postRaftJoin(ctx context.Context, client *http.Client, joinAddrs []string, nodeID, raftAddress string) error {
	if client == nil {
		client = newRaftJoinHTTPClient()
	}
	var errs []error
	for _, joinAddr := range joinAddrs {
		if joinAddr == "" {
			continue
		}
		if err := postRaftJoinOne(ctx, client, joinAddr, nodeID, raftAddress); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", joinAddr, err))
			continue
		}
		return nil
	}
	if len(errs) == 0 {
		return fmt.Errorf("raft join address is required")
	}
	return errors.Join(errs...)
}

func postRaftJoinOne(ctx context.Context, client *http.Client, joinAddr, nodeID, raftAddress string) error {
	endpoint, err := raftJoinURL(joinAddr)
	if err != nil {
		return err
	}
	body, err := json.Marshal(raftJoinRequest{NodeID: nodeID, RaftAddress: raftAddress})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	var response raftJoinErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&response)
	if response.Message == "" {
		response.Message = fmt.Sprintf("raft join request failed with status %d", resp.StatusCode)
	}
	if response.Code == "not_raft_leader" {
		return state.NewNotLeaderError(response.LeaderAddress)
	}
	return &state.StateError{
		StatusCode: resp.StatusCode,
		Code:       response.Code,
		Message:    response.Message,
	}
}

func newRaftJoinHTTPClient() *http.Client {
	return &http.Client{Timeout: raftJoinTimeout}
}

func raftJoinURL(joinAddr string) (string, error) {
	return dashboardAPIURL(joinAddr, "/api/cluster/join")
}

func clusterStatusURL(peer string) (string, error) {
	return dashboardAPIURL(peer, "/api/cluster")
}

func dashboardAPIURL(base, apiPath string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("raft join address must be an absolute dashboard URL")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == apiPath {
		return parsed.String(), nil
	}
	parsed.Path = path + apiPath
	return parsed.String(), nil
}

func (a *App) startHealthChecker(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.healthCancel != nil {
		return
	}

	healthCtx, cancel := context.WithCancel(ctx)
	a.runCtx = ctx
	a.healthCtx = healthCtx
	a.healthCancel = cancel

	if a.healthChecker != nil {
		a.healthChecker.Start(healthCtx)
	}
}

func (a *App) stopHealthChecker() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.healthCancel != nil {
		a.healthCancel()
	}

	a.healthCtx = nil
	a.healthCancel = nil
	a.runCtx = nil
}

func (a *App) swapHealthChecker(registry *upstream.Registry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.healthCancel != nil {
		a.healthCancel()
		a.healthCancel = nil
	}

	a.healthChecker = upstream.NewChecker(registry)

	if a.runCtx != nil && a.healthChecker != nil {
		healthCtx, cancel := context.WithCancel(a.runCtx)
		a.healthCtx = healthCtx
		a.healthCancel = cancel
		a.healthChecker.Start(healthCtx)
	}
}
