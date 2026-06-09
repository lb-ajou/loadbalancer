package upstream

import (
	"fmt"

	"reverseproxy-poc/internal/spec"
)

func BuildRegistry(cfg spec.Config) (*Registry, error) {
	pools, err := BuildPools(cfg)
	if err != nil {
		return nil, err
	}
	return NewRegistry(pools)
}

func BuildPools(cfg spec.Config) ([]Pool, error) {
	pools := make([]Pool, 0, len(cfg.UpstreamPools))
	for id, poolCfg := range cfg.UpstreamPools {
		pool, err := buildPool(id, poolCfg)
		if err != nil {
			return nil, fmt.Errorf("build upstream pool %q: %w", id, err)
		}
		pools = append(pools, pool)
	}

	return pools, nil
}

func buildPool(id string, poolCfg spec.UpstreamPool) (Pool, error) {
	return Pool{
		ID:          id,
		Targets:     buildTargets(poolCfg.Upstreams),
		HealthCheck: buildHealthCheck(poolCfg.HealthCheck),
		targetState: healthyTargetStates(len(poolCfg.Upstreams)),
		active:      make([]uint64, len(poolCfg.Upstreams)),
	}, nil
}

func buildTargets(upstreams []string) []Target {
	targets := make([]Target, 0, len(upstreams))
	for _, upstream := range upstreams {
		targets = append(targets, Target{Raw: upstream})
	}
	return targets
}

func healthyTargetStates(size int) []TargetState {
	states := make([]TargetState, 0, size)
	for i := 0; i < size; i++ {
		states = append(states, TargetState{Healthy: true})
	}
	return states
}

func buildHealthCheck(hc *spec.HealthCheckConfig) *HealthCheck {
	if hc == nil {
		return nil
	}

	return &HealthCheck{
		Path:         hc.Path,
		Interval:     string(hc.Interval),
		Timeout:      string(hc.Timeout),
		ExpectStatus: hc.ExpectStatus,
	}
}
