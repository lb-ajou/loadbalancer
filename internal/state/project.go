package state

import (
	"fmt"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/route"
	appruntime "reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/upstream"
)

func ProjectSnapshot(appCfg boot.AppConfig, raftCfg config.RaftConfig, localVIP config.VIPConfig, desired DesiredState) (appruntime.Snapshot, error) {
	runtimeVIP := projectClusterVIP(localVIP, desired.VIP)
	raftTiming := projectClusterRaftTiming(raftCfg.Timing, desired.RaftTiming)
	proxyConfig := normalizeConfig(desired.ProxyConfig)
	if errs := proxyConfig.Validate(); len(errs) > 0 {
		return appruntime.Snapshot{}, spec.ValidationErrors(errs)
	}
	routes, err := route.BuildTable(proxyConfig)
	if err != nil {
		return appruntime.Snapshot{}, fmt.Errorf("build route table: %w", err)
	}
	upstreams, err := upstream.BuildRegistry(proxyConfig)
	if err != nil {
		return appruntime.Snapshot{}, fmt.Errorf("build upstream registry: %w", err)
	}
	snapshot := appruntime.NewSnapshot(appCfg, raftCfg.Identity, raftTiming, runtimeVIP, proxyConfig, routes, upstreams)
	if !desired.AppliedAt.IsZero() {
		snapshot.AppliedAt = desired.AppliedAt
	}
	return snapshot, nil
}

func projectClusterVIP(runtimeVIP config.VIPConfig, vip *ClusterVIPConfig) config.VIPConfig {
	if vip == nil {
		return runtimeVIP
	}
	normalized := NormalizeClusterVIP(*vip)
	runtimeVIP.Address = normalized.Address
	runtimeVIP.GARPCount = normalized.GARPCount
	runtimeVIP.GARPInterval = normalized.GARPInterval
	runtimeVIP.AcquireDelay = normalized.AcquireDelay
	runtimeVIP.ReleaseOnShutdown = normalized.ReleaseOnShutdown
	return runtimeVIP
}

func projectClusterRaftTiming(runtimeTiming config.RaftTiming, timing *ClusterRaftTimingConfig) config.RaftTiming {
	if timing == nil {
		return runtimeTiming
	}
	runtimeTiming.HeartbeatTimeout = timing.HeartbeatTimeout
	runtimeTiming.ElectionTimeout = timing.ElectionTimeout
	runtimeTiming.LeaderLeaseTimeout = timing.LeaderLeaseTimeout
	runtimeTiming.CommitTimeout = timing.CommitTimeout
	return runtimeTiming
}

func normalizeConfig(cfg spec.Config) spec.Config {
	if cfg.Routes == nil {
		cfg.Routes = []spec.RouteConfig{}
	}
	if cfg.UpstreamPools == nil {
		cfg.UpstreamPools = map[string]spec.UpstreamPool{}
	}
	return cfg
}
