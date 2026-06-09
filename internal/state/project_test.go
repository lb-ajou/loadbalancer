package state

import (
	"testing"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/spec"
)

func TestProjectSnapshot_BuildsRuntimeFromProxyConfig(t *testing.T) {
	appCfg := boot.AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
	}
	desired := DesiredState{
		ProxyConfig: spec.Config{
			Routes: []spec.RouteConfig{{
				ID:      "r-api",
				Enabled: true,
				Match: spec.RouteMatchConfig{
					Hosts: []string{"api.example.com"},
				},
				UpstreamPool: "pool-api",
			}},
			UpstreamPools: map[string]spec.UpstreamPool{
				"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
			},
		},
	}

	snapshot, err := ProjectSnapshot(appCfg, config.RaftConfig{}, config.VIPConfig{}, desired)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if got, want := len(snapshot.ProxyConfig.Routes), 1; got != want {
		t.Fatalf("len(snapshot.ProxyConfig.Routes) = %d, want %d", got, want)
	}
	if got, want := snapshot.RouteTable[0].ID, "r-api"; got != want {
		t.Fatalf("snapshot.RouteTable[0].ID = %q, want %q", got, want)
	}
	if _, ok := snapshot.Upstreams.Get("pool-api"); !ok {
		t.Fatal("snapshot.Upstreams.Get(pool-api) returned no pool")
	}
}

func TestProjectSnapshot_RejectsInvalidDesiredConfig(t *testing.T) {
	_, err := ProjectSnapshot(boot.AppConfig{}, config.RaftConfig{}, config.VIPConfig{}, DesiredState{
		ProxyConfig: spec.Config{
			Routes: []spec.RouteConfig{{
				ID:           "r-api",
				Enabled:      true,
				Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
				UpstreamPool: "missing",
			}},
			UpstreamPools: map[string]spec.UpstreamPool{},
		},
	})
	if err == nil {
		t.Fatal("ProjectSnapshot() error = nil, want validation error")
	}
}

func TestProjectSnapshot_ProjectsRaftVIPWithLocalInterface(t *testing.T) {
	appCfg := boot.AppConfig{}
	localVIP := config.VIPConfig{Interface: "eth0"}
	state := DesiredState{
		VIP: &ClusterVIPConfig{
			Address:           "10.10.0.100/24",
			GARPCount:         3,
			GARPInterval:      "100ms",
			AcquireDelay:      "300ms",
			ReleaseOnShutdown: true,
		},
	}

	snapshot, err := ProjectSnapshot(appCfg, config.RaftConfig{}, localVIP, state)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if got, want := snapshot.VIP.Interface, "eth0"; got != want {
		t.Fatalf("VIP.Interface = %q, want %q", got, want)
	}
	if got, want := snapshot.VIP.Address, "10.10.0.100/24"; got != want {
		t.Fatalf("VIP.Address = %q, want %q", got, want)
	}
	if !snapshot.VIP.Active() {
		t.Fatal("VIP.Active() = false, want true")
	}
}

func TestProjectSnapshot_NormalizesClusterVIPDefaults(t *testing.T) {
	appCfg := boot.AppConfig{}
	localVIP := config.VIPConfig{Interface: "eth0"}
	state := DesiredState{
		VIP: &ClusterVIPConfig{Address: "10.10.0.100/24"},
	}

	snapshot, err := ProjectSnapshot(appCfg, config.RaftConfig{}, localVIP, state)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if got, want := snapshot.VIP.GARPCount, DefaultVIPGARPCount; got != want {
		t.Fatalf("VIP.GARPCount = %d, want %d", got, want)
	}
	if got, want := snapshot.VIP.GARPInterval, DefaultVIPGARPInterval; got != want {
		t.Fatalf("VIP.GARPInterval = %q, want %q", got, want)
	}
}

func TestProjectSnapshot_ProjectsClusterRaftTiming(t *testing.T) {
	state := DesiredState{
		RaftTiming: &ClusterRaftTimingConfig{
			HeartbeatTimeout:   "3s",
			ElectionTimeout:    "5s",
			LeaderLeaseTimeout: "2s",
			CommitTimeout:      "250ms",
		},
	}

	snapshot, err := ProjectSnapshot(boot.AppConfig{}, config.RaftConfig{}, config.VIPConfig{}, state)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if got, want := snapshot.RaftTiming.HeartbeatTimeout, "3s"; got != want {
		t.Fatalf("RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}
