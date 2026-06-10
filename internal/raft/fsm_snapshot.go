package raftstore

import (
	"encoding/json"
	"io"

	"github.com/hashicorp/raft"

	"loadbalancer/internal/spec"
	control "loadbalancer/internal/state"
)

type fsmSnapshot struct {
	state control.DesiredState
}

func newFSMSnapshot(state control.DesiredState) raft.FSMSnapshot {
	return &fsmSnapshot{state: cloneDesiredState(state)}
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if err := json.NewEncoder(sink).Encode(s.state); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}

func decodeSnapshot(reader io.Reader) (control.DesiredState, error) {
	var state control.DesiredState
	if err := json.NewDecoder(reader).Decode(&state); err != nil {
		return control.DesiredState{}, err
	}
	state.ProxyConfig = cloneConfig(state.ProxyConfig)
	return cloneDesiredState(state), nil
}

func cloneDesiredState(state control.DesiredState) control.DesiredState {
	cloned := state
	cloned.ProxyConfig = cloneConfig(state.ProxyConfig)
	if state.VIP != nil {
		vip := control.NormalizeClusterVIP(*state.VIP)
		cloned.VIP = &vip
	}
	if state.RaftTiming != nil {
		timing := *state.RaftTiming
		cloned.RaftTiming = &timing
	}
	return cloned
}

func cloneConfig(cfg spec.Config) spec.Config {
	cloned := cfg
	if cfg.Routes == nil {
		cloned.Routes = []spec.RouteConfig{}
	} else {
		cloned.Routes = make([]spec.RouteConfig, 0, len(cfg.Routes))
		for _, route := range cfg.Routes {
			cloned.Routes = append(cloned.Routes, cloneRoute(route))
		}
	}

	cloned.UpstreamPools = make(map[string]spec.UpstreamPool, len(cfg.UpstreamPools))
	for id, pool := range cfg.UpstreamPools {
		cloned.UpstreamPools[id] = cloneUpstreamPool(pool)
	}
	return cloned
}

func cloneRoute(route spec.RouteConfig) spec.RouteConfig {
	cloned := route
	cloned.Match.Hosts = append([]string(nil), route.Match.Hosts...)
	if route.Match.Path != nil {
		path := *route.Match.Path
		cloned.Match.Path = &path
	}
	return cloned
}

func cloneUpstreamPool(pool spec.UpstreamPool) spec.UpstreamPool {
	cloned := pool
	cloned.Upstreams = append([]string(nil), pool.Upstreams...)
	if pool.HealthCheck != nil {
		healthCheck := *pool.HealthCheck
		cloned.HealthCheck = &healthCheck
	}
	return cloned
}
