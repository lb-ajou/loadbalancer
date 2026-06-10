package runtime

import (
	"sync"
	"time"

	"loadbalancer/internal/boot"
	"loadbalancer/internal/config"
	"loadbalancer/internal/route"
	"loadbalancer/internal/spec"
	"loadbalancer/internal/upstream"
)

type State struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewState(snapshot Snapshot) *State {
	return &State{
		snapshot: copySnapshot(snapshot),
	}
}

func NewSnapshot(
	appCfg boot.AppConfig,
	raftIdentity config.RaftIdentity,
	raftTiming config.RaftTiming,
	vip config.VIPConfig,
	proxyConfig spec.Config,
	routes []route.Route,
	upstreams *upstream.Registry,
) Snapshot {
	return copySnapshot(Snapshot{
		AppConfig:    appCfg,
		RaftIdentity: raftIdentity,
		RaftTiming:   raftTiming,
		VIP:          vip,
		ProxyConfig:  proxyConfig,
		RouteTable:   routes,
		Upstreams:    upstreams,
		AppliedAt:    time.Now(),
	})
}

func copySnapshot(snapshot Snapshot) Snapshot {
	snapshot.ProxyConfig = copyProxyConfig(snapshot.ProxyConfig)
	snapshot.RouteTable = copyRoutes(snapshot.RouteTable)
	return snapshot
}

func copyProxyConfig(cfg spec.Config) spec.Config {
	copyCfg := cfg
	copyCfg.Routes = copyRouteConfigs(cfg.Routes)
	copyCfg.UpstreamPools = copyUpstreamPools(cfg.UpstreamPools)
	return copyCfg
}

func copyRouteConfigs(routes []spec.RouteConfig) []spec.RouteConfig {
	if routes == nil {
		return nil
	}
	copied := make([]spec.RouteConfig, len(routes))
	for i, routeCfg := range routes {
		copied[i] = routeCfg
		copied[i].Match.Hosts = append([]string(nil), routeCfg.Match.Hosts...)
		if routeCfg.Match.Path != nil {
			path := *routeCfg.Match.Path
			copied[i].Match.Path = &path
		}
	}
	return copied
}

func copyUpstreamPools(pools map[string]spec.UpstreamPool) map[string]spec.UpstreamPool {
	if pools == nil {
		return nil
	}
	copied := make(map[string]spec.UpstreamPool, len(pools))
	for id, pool := range pools {
		poolCopy := pool
		poolCopy.Upstreams = append([]string(nil), pool.Upstreams...)
		if pool.HealthCheck != nil {
			healthCheck := *pool.HealthCheck
			poolCopy.HealthCheck = &healthCheck
		}
		copied[id] = poolCopy
	}
	return copied
}

func copyRoutes(routes []route.Route) []route.Route {
	if routes == nil {
		return nil
	}
	copied := make([]route.Route, len(routes))
	for i, route := range routes {
		copied[i] = route
		copied[i].Hosts = append([]string(nil), route.Hosts...)
	}
	return copied
}

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return copySnapshot(s.snapshot)
}

func (s *State) Swap(snapshot Snapshot) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot = copySnapshot(snapshot)

	return copySnapshot(s.snapshot)
}
