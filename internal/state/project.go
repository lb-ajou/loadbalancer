package state

import (
	"fmt"
	"sort"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/raftstate"
	"reverseproxy-poc/internal/route"
	appruntime "reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/upstream"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
)

func ProjectSnapshot(appCfg boot.AppConfig, raftCfg raftstate.Config, localVIP vipruntime.Config, desired DesiredState) (appruntime.Snapshot, error) {
	runtimeVIP := projectClusterVIP(localVIP, desired.VIP)
	raftTiming := projectClusterRaftTiming(raftCfg.Timing, desired.RaftTiming)
	loaded, err := LoadedConfigs(desired)
	if err != nil {
		return appruntime.Snapshot{}, err
	}
	routes, err := route.BuildTable(loaded)
	if err != nil {
		return appruntime.Snapshot{}, fmt.Errorf("build route table: %w", err)
	}
	upstreams, err := upstream.BuildRegistry(loaded)
	if err != nil {
		return appruntime.Snapshot{}, fmt.Errorf("build upstream registry: %w", err)
	}
	snapshot := appruntime.NewSnapshot(appCfg, raftCfg.Identity, raftTiming, runtimeVIP, loaded, routes, upstreams)
	if !desired.AppliedAt.IsZero() {
		snapshot.AppliedAt = desired.AppliedAt
	}
	return snapshot, nil
}

func projectClusterVIP(runtimeVIP vipruntime.Config, vip *ClusterVIPConfig) vipruntime.Config {
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

func projectClusterRaftTiming(runtimeTiming raftstate.Timing, timing *ClusterRaftTimingConfig) raftstate.Timing {
	if timing == nil {
		return runtimeTiming
	}
	runtimeTiming.HeartbeatTimeout = timing.HeartbeatTimeout
	runtimeTiming.ElectionTimeout = timing.ElectionTimeout
	runtimeTiming.LeaderLeaseTimeout = timing.LeaderLeaseTimeout
	runtimeTiming.CommitTimeout = timing.CommitTimeout
	return runtimeTiming
}

func LoadedConfigs(desired DesiredState) ([]spec.LoadedConfig, error) {
	namespaces := sortedNamespaces(desired.Namespaces)
	loaded := make([]spec.LoadedConfig, 0, len(namespaces))
	for _, namespace := range namespaces {
		cfg := normalizeConfig(desired.Namespaces[namespace])
		if errs := cfg.Validate(); len(errs) > 0 {
			return nil, spec.ValidationErrors(errs)
		}
		loaded = append(loaded, spec.LoadedConfig{
			Source: namespace,
			Path:   DesiredStatePath(namespace),
			Config: cfg,
		})
	}
	return loaded, nil
}

func DesiredStatePath(namespace string) string {
	return "raft://namespaces/" + namespace
}

func sortedNamespaces(configs map[string]spec.Config) []string {
	namespaces := make([]string, 0, len(configs))
	for namespace := range configs {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	return namespaces
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
