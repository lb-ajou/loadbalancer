package upstream

import (
	"fmt"

	"reverseproxy-poc/internal/spec"
)

func BuildRegistry(configs []spec.LoadedConfig) (*Registry, error) {
	pools := make([]Pool, 0)
	for _, loaded := range configs {
		compiled, err := BuildPools(loaded.Source, loaded.Config)
		if err != nil {
			return nil, err
		}
		pools = append(pools, compiled...)
	}

	return NewRegistry(pools)
}

func BuildPools(source string, cfg spec.Config) ([]Pool, error) {
	pools := make([]Pool, 0, len(cfg.UpstreamPools))
	for localID, poolCfg := range cfg.UpstreamPools {
		pool, err := buildPool(source, localID, poolCfg)
		if err != nil {
			return nil, fmt.Errorf("build upstream pool %q from source %q: %w", localID, source, err)
		}
		pools = append(pools, pool)
	}

	return pools, nil
}

func GlobalPoolID(source, localID string) string {
	return source + ":" + localID
}

func buildPool(source, localID string, poolCfg spec.UpstreamPool) (Pool, error) {
	return Pool{
		GlobalID:    GlobalPoolID(source, localID),
		LocalID:     localID,
		Source:      source,
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
