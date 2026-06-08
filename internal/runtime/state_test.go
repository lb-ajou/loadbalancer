package runtime

import (
	"testing"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/route"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/upstream"
)

func TestNewSnapshot_CopiesSlices(t *testing.T) {
	appCfg := boot.AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
	}
	proxyCfgs := []spec.LoadedConfig{
		{Source: "default"},
	}
	routes := []route.Route{
		{GlobalID: "default:r-api"},
	}

	snapshot := NewSnapshot(appCfg, config.RaftIdentity{}, config.RaftTiming{}, config.VIPConfig{}, proxyCfgs, routes, nil)

	proxyCfgs[0].Source = "changed"
	routes[0].GlobalID = "changed"

	if got, want := snapshot.ProxyConfigs[0].Source, "default"; got != want {
		t.Fatalf("snapshot.ProxyConfigs[0].Source = %q, want %q", got, want)
	}
	if got, want := snapshot.RouteTable[0].GlobalID, "default:r-api"; got != want {
		t.Fatalf("snapshot.RouteTable[0].GlobalID = %q, want %q", got, want)
	}
}

func TestNewSnapshot_ProjectsRuntimeVIP(t *testing.T) {
	vip := config.VIPConfig{Interface: "eth0", Address: "10.10.0.100/24"}

	snapshot := NewSnapshot(boot.AppConfig{}, config.RaftIdentity{}, config.RaftTiming{}, vip, nil, nil, nil)

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
		nil,
		nil,
		nil,
	)

	state := NewState(initial)

	next := NewSnapshot(
		boot.AppConfig{ProxyListenAddr: ":8081", DashboardListenAddr: ":9091"},
		config.RaftIdentity{},
		config.RaftTiming{},
		config.VIPConfig{},
		nil,
		nil,
		nil,
	)

	state.Swap(next)

	if got, want := state.Snapshot().AppConfig.ProxyListenAddr, ":8081"; got != want {
		t.Fatalf("state.Snapshot().AppConfig.ProxyListenAddr = %q, want %q", got, want)
	}
}

func TestNewSnapshot_PreservesUpstreamRegistryReference(t *testing.T) {
	registry, err := upstream.NewRegistry([]upstream.Pool{
		{
			GlobalID: "default:api",
			LocalID:  "api",
			Source:   "default",
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
		nil,
		nil,
		registry,
	)

	if snapshot.Upstreams != registry {
		t.Fatal("snapshot.Upstreams does not preserve the upstream registry reference")
	}
}
