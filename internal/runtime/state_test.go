package runtime

import (
	"testing"

	"loadbalancer/internal/boot"
	"loadbalancer/internal/config"
	"loadbalancer/internal/route"
	"loadbalancer/internal/spec"
	"loadbalancer/internal/upstream"
)

func TestNewSnapshot_CopiesSlices(t *testing.T) {
	appCfg := boot.AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
	}
	proxyConfig := spec.Config{
		Routes: []spec.RouteConfig{{
			ID: "r-api",
			Match: spec.RouteMatchConfig{
				Hosts: []string{"api.example.com"},
				Path:  &spec.PathMatchConfig{Type: spec.PathMatchPrefix, Value: "/api"},
			},
		}},
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {
				Upstreams:   []string{"10.0.0.11:8080"},
				HealthCheck: &spec.HealthCheckConfig{Path: "/health"},
			},
		},
	}
	routes := []route.Route{
		{ID: "r-api", Hosts: []string{"api.example.com"}},
	}

	snapshot := NewSnapshot(appCfg, config.RaftIdentity{}, config.RaftTiming{}, config.VIPConfig{}, proxyConfig, routes, nil)

	proxyConfig.Routes[0].ID = "changed"
	proxyConfig.Routes[0].Match.Hosts[0] = "changed.example.com"
	proxyConfig.Routes[0].Match.Path.Value = "/changed"
	pool := proxyConfig.UpstreamPools["pool-api"]
	pool.Upstreams[0] = "10.0.0.12:8080"
	pool.HealthCheck.Path = "/changed"
	proxyConfig.UpstreamPools["pool-api"] = pool
	routes[0].ID = "changed"
	routes[0].Hosts[0] = "changed.example.com"

	if got, want := snapshot.ProxyConfig.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("snapshot.ProxyConfig.Routes[0].ID = %q, want %q", got, want)
	}
	if got, want := snapshot.ProxyConfig.Routes[0].Match.Hosts[0], "api.example.com"; got != want {
		t.Fatalf("snapshot.ProxyConfig.Routes[0].Match.Hosts[0] = %q, want %q", got, want)
	}
	if got, want := snapshot.ProxyConfig.Routes[0].Match.Path.Value, "/api"; got != want {
		t.Fatalf("snapshot.ProxyConfig.Routes[0].Match.Path.Value = %q, want %q", got, want)
	}
	if got, want := snapshot.ProxyConfig.UpstreamPools["pool-api"].Upstreams[0], "10.0.0.11:8080"; got != want {
		t.Fatalf("snapshot.ProxyConfig.UpstreamPools[pool-api].Upstreams[0] = %q, want %q", got, want)
	}
	if got, want := snapshot.ProxyConfig.UpstreamPools["pool-api"].HealthCheck.Path, "/health"; got != want {
		t.Fatalf("snapshot.ProxyConfig.UpstreamPools[pool-api].HealthCheck.Path = %q, want %q", got, want)
	}
	if got, want := snapshot.RouteTable[0].ID, "r-api"; got != want {
		t.Fatalf("snapshot.RouteTable[0].ID = %q, want %q", got, want)
	}
	if got, want := snapshot.RouteTable[0].Hosts[0], "api.example.com"; got != want {
		t.Fatalf("snapshot.RouteTable[0].Hosts[0] = %q, want %q", got, want)
	}
}

func TestNewSnapshot_ProjectsRuntimeVIP(t *testing.T) {
	vip := config.VIPConfig{Interface: "eth0", Address: "10.10.0.100/24"}

	snapshot := NewSnapshot(boot.AppConfig{}, config.RaftIdentity{}, config.RaftTiming{}, vip, spec.Config{}, nil, nil)

	if got, want := snapshot.VIP.Interface, "eth0"; got != want {
		t.Fatalf("snapshot.VIP.Interface = %q, want %q", got, want)
	}
	if got, want := snapshot.VIP.Address, "10.10.0.100/24"; got != want {
		t.Fatalf("snapshot.VIP.Address = %q, want %q", got, want)
	}
}

func TestStateSwap_ReplacesSnapshot(t *testing.T) {
	initial := NewSnapshot(
		boot.AppConfig{ProxyListenAddr: ":8080", DashboardListenAddr: ":9090"},
		config.RaftIdentity{},
		config.RaftTiming{},
		config.VIPConfig{},
		spec.Config{},
		nil,
		nil,
	)

	state := NewState(initial)

	next := NewSnapshot(
		boot.AppConfig{ProxyListenAddr: ":8081", DashboardListenAddr: ":9091"},
		config.RaftIdentity{},
		config.RaftTiming{},
		config.VIPConfig{},
		spec.Config{},
		nil,
		nil,
	)

	state.Swap(next)

	if got, want := state.Snapshot().AppConfig.ProxyListenAddr, ":8081"; got != want {
		t.Fatalf("state.Snapshot().AppConfig.ProxyListenAddr = %q, want %q", got, want)
	}
}

func TestState_CopiesSnapshotsAtBoundaries(t *testing.T) {
	initial := Snapshot{
		ProxyConfig: spec.Config{
			Routes: []spec.RouteConfig{{
				ID:    "r-api",
				Match: spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
			}},
			UpstreamPools: map[string]spec.UpstreamPool{
				"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
			},
		},
		RouteTable: []route.Route{{
			ID:    "r-api",
			Hosts: []string{"api.example.com"},
		}},
	}

	state := NewState(initial)
	initial.ProxyConfig.Routes[0].ID = "changed"
	initialPool := initial.ProxyConfig.UpstreamPools["pool-api"]
	initialPool.Upstreams[0] = "10.0.0.12:8080"
	initial.ProxyConfig.UpstreamPools["pool-api"] = initialPool
	initial.RouteTable[0].ID = "changed"
	initial.RouteTable[0].Hosts[0] = "changed.example.com"

	snapshot := state.Snapshot()
	snapshot.ProxyConfig.Routes[0].ID = "mutated"
	snapshotPool := snapshot.ProxyConfig.UpstreamPools["pool-api"]
	snapshotPool.Upstreams[0] = "10.0.0.13:8080"
	snapshot.ProxyConfig.UpstreamPools["pool-api"] = snapshotPool
	snapshot.RouteTable[0].ID = "mutated"
	snapshot.RouteTable[0].Hosts[0] = "mutated.example.com"

	stored := state.Snapshot()
	if got, want := stored.ProxyConfig.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("stored.ProxyConfig.Routes[0].ID = %q, want %q", got, want)
	}
	if got, want := stored.ProxyConfig.UpstreamPools["pool-api"].Upstreams[0], "10.0.0.11:8080"; got != want {
		t.Fatalf("stored.ProxyConfig.UpstreamPools[pool-api].Upstreams[0] = %q, want %q", got, want)
	}
	if got, want := stored.RouteTable[0].ID, "r-api"; got != want {
		t.Fatalf("stored.RouteTable[0].ID = %q, want %q", got, want)
	}
	if got, want := stored.RouteTable[0].Hosts[0], "api.example.com"; got != want {
		t.Fatalf("stored.RouteTable[0].Hosts[0] = %q, want %q", got, want)
	}

	next := Snapshot{
		ProxyConfig: spec.Config{
			Routes: []spec.RouteConfig{{ID: "r-admin"}},
			UpstreamPools: map[string]spec.UpstreamPool{
				"pool-admin": {Upstreams: []string{"10.0.0.21:8080"}},
			},
		},
		RouteTable: []route.Route{{
			ID:    "r-admin",
			Hosts: []string{"admin.example.com"},
		}},
	}

	swapped := state.Swap(next)
	next.ProxyConfig.Routes[0].ID = "changed"
	nextPool := next.ProxyConfig.UpstreamPools["pool-admin"]
	nextPool.Upstreams[0] = "10.0.0.22:8080"
	next.ProxyConfig.UpstreamPools["pool-admin"] = nextPool
	next.RouteTable[0].ID = "changed"
	next.RouteTable[0].Hosts[0] = "changed.example.com"
	swapped.ProxyConfig.Routes[0].ID = "mutated"
	swappedPool := swapped.ProxyConfig.UpstreamPools["pool-admin"]
	swappedPool.Upstreams[0] = "10.0.0.23:8080"
	swapped.ProxyConfig.UpstreamPools["pool-admin"] = swappedPool
	swapped.RouteTable[0].ID = "mutated"
	swapped.RouteTable[0].Hosts[0] = "mutated.example.com"

	stored = state.Snapshot()
	if got, want := stored.ProxyConfig.Routes[0].ID, "r-admin"; got != want {
		t.Fatalf("stored.ProxyConfig.Routes[0].ID after Swap = %q, want %q", got, want)
	}
	if got, want := stored.ProxyConfig.UpstreamPools["pool-admin"].Upstreams[0], "10.0.0.21:8080"; got != want {
		t.Fatalf("stored.ProxyConfig.UpstreamPools[pool-admin].Upstreams[0] after Swap = %q, want %q", got, want)
	}
	if got, want := stored.RouteTable[0].ID, "r-admin"; got != want {
		t.Fatalf("stored.RouteTable[0].ID after Swap = %q, want %q", got, want)
	}
	if got, want := stored.RouteTable[0].Hosts[0], "admin.example.com"; got != want {
		t.Fatalf("stored.RouteTable[0].Hosts[0] after Swap = %q, want %q", got, want)
	}
}

func TestNewSnapshot_PreservesUpstreamRegistryReference(t *testing.T) {
	registry, err := upstream.NewRegistry([]upstream.Pool{
		{
			ID: "api",
			Targets: []upstream.Target{
				{Raw: "localhost:8080"},
			},
		},
	})
	if err != nil {
		t.Fatalf("upstream.NewRegistry() error = %v", err)
	}

	snapshot := NewSnapshot(
		boot.AppConfig{},
		config.RaftIdentity{},
		config.RaftTiming{},
		config.VIPConfig{},
		spec.Config{},
		nil,
		registry,
	)

	if snapshot.Upstreams != registry {
		t.Fatal("snapshot.Upstreams does not preserve the upstream registry reference")
	}
}
