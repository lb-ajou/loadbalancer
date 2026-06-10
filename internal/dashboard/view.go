package dashboard

import (
	"sort"
	"time"

	"loadbalancer/internal/route"
	"loadbalancer/internal/runtime"
	"loadbalancer/internal/spec"
	"loadbalancer/internal/upstream"
)

type RuntimeView struct {
	AppliedAt time.Time             `json:"applied_at"`
	Routes    []RouteView           `json:"routes"`
	Upstreams []RuntimeUpstreamView `json:"upstreams"`
}

type RuntimeUpstreamView struct {
	ID          string                `json:"id"`
	Targets     []RuntimeTargetView   `json:"targets"`
	HealthCheck *upstream.HealthCheck `json:"health_check,omitempty"`
}

type RuntimeTargetView struct {
	Address           string     `json:"address"`
	Healthy           bool       `json:"healthy"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	ActiveConnections uint64     `json:"active_connections"`
}

type StatusView struct {
	Node    StatusNodeView    `json:"node"`
	Raft    StatusRaftView    `json:"raft"`
	VIP     VIPStatusView     `json:"vip"`
	Runtime StatusRuntimeView `json:"runtime"`
}

type StatusNodeView struct {
	ID                  string         `json:"id,omitempty"`
	ProxyListenAddr     string         `json:"proxy_listen_addr"`
	DashboardListenAddr string         `json:"dashboard_listen_addr"`
	AppliedAt           time.Time      `json:"applied_at"`
	Projection          ProjectionView `json:"projection"`
}

type ProjectionView struct {
	Status    string `json:"status"`
	LastError string `json:"last_error,omitempty"`
}

type StatusRaftView struct {
	Enabled       bool   `json:"enabled"`
	State         string `json:"state"`
	IsLeader      bool   `json:"is_leader"`
	LeaderID      string `json:"leader_id,omitempty"`
	LeaderAddress string `json:"leader_address,omitempty"`
	HasLeader     bool   `json:"has_leader"`
	QuorumStatus  string `json:"quorum_status"`
}

type VIPStatusView struct {
	Enabled   bool   `json:"enabled"`
	Interface string `json:"interface,omitempty"`
	Address   string `json:"address,omitempty"`
	Owned     bool   `json:"owned"`
	LastError string `json:"last_error,omitempty"`
}

type StatusRuntimeView struct {
	RouteCount           int `json:"route_count"`
	UpstreamPoolCount    int `json:"upstream_pool_count"`
	TargetCount          int `json:"target_count"`
	HealthyTargetCount   int `json:"healthy_target_count"`
	UnhealthyTargetCount int `json:"unhealthy_target_count"`
}

type ClusterView struct {
	Enabled      bool                   `json:"enabled"`
	QuorumStatus string                 `json:"quorum_status,omitempty"`
	Leader       ClusterLeaderView      `json:"leader"`
	Local        ClusterLocalView       `json:"local"`
	Members      []ClusterMemberView    `json:"members"`
	RaftTiming   *ClusterRaftTimingView `json:"raft_timing,omitempty"`
}

type ClusterRaftTimingView struct {
	HeartbeatTimeout   string `json:"heartbeat_timeout,omitempty"`
	ElectionTimeout    string `json:"election_timeout,omitempty"`
	LeaderLeaseTimeout string `json:"leader_lease_timeout,omitempty"`
	CommitTimeout      string `json:"commit_timeout,omitempty"`
}

type ClusterLeaderView struct {
	ID      string `json:"id,omitempty"`
	Address string `json:"address,omitempty"`
}

type ClusterLocalView struct {
	ID           string `json:"id,omitempty"`
	Address      string `json:"address,omitempty"`
	State        string `json:"state,omitempty"`
	LastLogIndex string `json:"last_log_index,omitempty"`
	CommitIndex  string `json:"commit_index,omitempty"`
	AppliedIndex string `json:"applied_index,omitempty"`
	Term         string `json:"term,omitempty"`
}

type ClusterMemberView struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Role     string `json:"role"`
	IsLeader bool   `json:"is_leader"`
}

type RouteView struct {
	ID           string          `json:"id"`
	Enabled      bool            `json:"enabled"`
	Hosts        []string        `json:"hosts"`
	Path         PathMatcherView `json:"path"`
	Algorithm    string          `json:"algorithm"`
	UpstreamPool string          `json:"upstream_pool"`
}

type PathMatcherView struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

func buildRuntimeView(snapshot runtime.Snapshot) RuntimeView {
	return RuntimeView{
		AppliedAt: snapshot.AppliedAt,
		Routes:    buildRouteViews(snapshot.RouteTable),
		Upstreams: buildRuntimeUpstreamViews(snapshot.Upstreams),
	}
}

func buildStatusView(snapshot runtime.Snapshot, cluster ClusterView, vip VIPStatusView) StatusView {
	return StatusView{
		Node:    buildStatusNodeView(snapshot),
		Raft:    buildStatusRaftView(cluster),
		VIP:     vip,
		Runtime: buildStatusRuntimeView(snapshot),
	}
}

func disabledClusterView() ClusterView {
	return ClusterView{Enabled: false, Local: ClusterLocalView{State: "disabled"}}
}

func vipStatusFromSnapshot(snapshot runtime.Snapshot) VIPStatusView {
	cfg := snapshot.VIP
	return VIPStatusView{Enabled: cfg.Active(), Interface: cfg.Interface, Address: cfg.Address}
}

func buildStatusNodeView(snapshot runtime.Snapshot) StatusNodeView {
	cfg := snapshot.AppConfig
	return StatusNodeView{
		ID:                  snapshot.RaftIdentity.NodeID,
		ProxyListenAddr:     cfg.ProxyListenAddr,
		DashboardListenAddr: cfg.DashboardListenAddr,
		AppliedAt:           snapshot.AppliedAt,
		Projection:          ProjectionView{Status: "ok"},
	}
}

func buildStatusRaftView(cluster ClusterView) StatusRaftView {
	if !cluster.Enabled {
		return StatusRaftView{Enabled: false, State: "disabled", QuorumStatus: "disabled"}
	}
	return StatusRaftView{
		Enabled:       true,
		State:         cluster.Local.State,
		IsLeader:      cluster.Local.ID != "" && cluster.Local.ID == cluster.Leader.ID,
		LeaderID:      cluster.Leader.ID,
		LeaderAddress: cluster.Leader.Address,
		HasLeader:     cluster.Leader.Address != "",
		QuorumStatus:  quorumStatusString(cluster),
	}
}

func quorumStatusString(cluster ClusterView) string {
	if cluster.QuorumStatus == "" {
		return "unknown"
	}
	return cluster.QuorumStatus
}

func buildStatusRuntimeView(snapshot runtime.Snapshot) StatusRuntimeView {
	views := buildRuntimeUpstreamViews(snapshot.Upstreams)
	return StatusRuntimeView{
		RouteCount:           len(snapshot.RouteTable),
		UpstreamPoolCount:    len(views),
		TargetCount:          countTargets(views),
		HealthyTargetCount:   countTargetsByHealth(views, true),
		UnhealthyTargetCount: countTargetsByHealth(views, false),
	}
}

func buildRouteViews(routes []route.Route) []RouteView {
	views := make([]RouteView, 0, len(routes))
	for _, item := range routes {
		views = append(views, buildRouteView(item))
	}
	return views
}

func buildRuntimeUpstreamViews(registry *upstream.Registry) []RuntimeUpstreamView {
	if registry == nil {
		return nil
	}
	pools := sortedUpstreamPools(registry.All())
	views := make([]RuntimeUpstreamView, 0, len(pools))
	for _, pool := range pools {
		views = append(views, buildRuntimeUpstreamView(pool))
	}
	return views
}

func routeAlgorithmString(algorithm spec.RouteAlgorithm) string {
	if algorithm == "" {
		return string(spec.RouteAlgorithmRoundRobin)
	}

	return string(algorithm)
}

func buildPathMatcherView(path route.PathMatcher) PathMatcherView {
	return PathMatcherView{
		Kind:  pathKindString(path.Kind),
		Value: path.Value,
	}
}

func pathKindString(kind route.PathKind) string {
	switch kind {
	case route.PathKindExact:
		return "exact"
	case route.PathKindPrefix:
		return "prefix"
	case route.PathKindRegex:
		return "regex"
	case route.PathKindAny:
		return "any"
	default:
		return "unknown"
	}
}

func buildRouteView(item route.Route) RouteView {
	return RouteView{
		ID:           item.ID,
		Enabled:      item.Enabled,
		Hosts:        append([]string(nil), item.Hosts...),
		Path:         buildPathMatcherView(item.Path),
		Algorithm:    item.Algorithm,
		UpstreamPool: item.UpstreamPool,
	}
}

func sortedUpstreamPools(pools []*upstream.Pool) []*upstream.Pool {
	sort.Slice(pools, func(i, j int) bool {
		return pools[i].ID < pools[j].ID
	})
	return pools
}

func buildRuntimeUpstreamView(pool *upstream.Pool) RuntimeUpstreamView {
	return RuntimeUpstreamView{
		ID:          pool.ID,
		Targets:     buildRuntimeTargetViews(pool),
		HealthCheck: pool.HealthCheck,
	}
}

func buildRuntimeTargetViews(pool *upstream.Pool) []RuntimeTargetView {
	states := pool.SnapshotStates()
	views := make([]RuntimeTargetView, 0, len(pool.Targets))
	for index, target := range pool.Targets {
		views = append(views, buildRuntimeTargetView(pool, states, index, target))
	}
	return views
}

func buildRuntimeTargetView(pool *upstream.Pool, states []upstream.TargetState, index int, target upstream.Target) RuntimeTargetView {
	state := targetStateAt(states, index)
	return RuntimeTargetView{
		Address:           target.Raw,
		Healthy:           state.Healthy,
		LastCheckedAt:     checkedAtPtr(pool, state),
		LastError:         state.LastError,
		ActiveConnections: pool.ActiveConnections(index),
	}
}

func targetStateAt(states []upstream.TargetState, index int) upstream.TargetState {
	if index >= 0 && index < len(states) {
		return states[index]
	}
	return upstream.TargetState{Healthy: true}
}

func checkedAtPtr(pool *upstream.Pool, state upstream.TargetState) *time.Time {
	if pool.HealthCheck == nil || state.LastCheckedAt.IsZero() {
		return nil
	}
	checkedAt := state.LastCheckedAt
	return &checkedAt
}

func countTargets(views []RuntimeUpstreamView) int {
	count := 0
	for _, view := range views {
		count += len(view.Targets)
	}
	return count
}

func countTargetsByHealth(views []RuntimeUpstreamView, healthy bool) int {
	count := 0
	for _, view := range views {
		count += countPoolTargetsByHealth(view, healthy)
	}
	return count
}

func countPoolTargetsByHealth(view RuntimeUpstreamView, healthy bool) int {
	count := 0
	for _, target := range view.Targets {
		if target.Healthy == healthy {
			count++
		}
	}
	return count
}
