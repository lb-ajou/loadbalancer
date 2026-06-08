package runtime

import (
	"sync"
	"time"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/route"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/upstream"
)

type State struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewState(snapshot Snapshot) *State {
	return &State{
		snapshot: snapshot,
	}
}

func NewSnapshot(
	appCfg boot.AppConfig,
	raftIdentity config.RaftIdentity,
	raftTiming config.RaftTiming,
	vip config.VIPConfig,
	proxyCfgs []spec.LoadedConfig,
	routes []route.Route,
	upstreams *upstream.Registry,
) Snapshot {
	proxyCfgsCopy := append([]spec.LoadedConfig(nil), proxyCfgs...)
	routesCopy := append([]route.Route(nil), routes...)

	return Snapshot{
		AppConfig:    appCfg,
		RaftIdentity: raftIdentity,
		RaftTiming:   raftTiming,
		VIP:          vip,
		ProxyConfigs: proxyCfgsCopy,
		RouteTable:   routesCopy,
		Upstreams:    upstreams,
		AppliedAt:    time.Now(),
	}
}

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.snapshot
}

func (s *State) Swap(snapshot Snapshot) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot = snapshot

	return s.snapshot
}
