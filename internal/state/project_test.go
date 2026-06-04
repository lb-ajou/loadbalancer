package state

import (
	"testing"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/raftstate"
	"reverseproxy-poc/internal/spec"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
)

func TestProjectSnapshot_BuildsRuntimeFromNamespaceMap(t *testing.T) {
	appCfg := boot.AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
	}
	state := DesiredState{
		Namespaces: map[string]spec.Config{
			"default": {
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
		},
	}

	snapshot, err := ProjectSnapshot(appCfg, raftstate.Config{}, vipruntime.Config{}, state)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if got, want := len(snapshot.ProxyConfigs), 1; got != want {
		t.Fatalf("len(snapshot.ProxyConfigs) = %d, want %d", got, want)
	}
	if got, want := snapshot.ProxyConfigs[0].Source, "default"; got != want {
		t.Fatalf("snapshot.ProxyConfigs[0].Source = %q, want %q", got, want)
	}
	if got, want := snapshot.ProxyConfigs[0].Path, "raft://namespaces/default"; got != want {
		t.Fatalf("snapshot.ProxyConfigs[0].Path = %q, want %q", got, want)
	}
	if got, want := len(snapshot.RouteTable), 1; got != want {
		t.Fatalf("len(snapshot.RouteTable) = %d, want %d", got, want)
	}
	if got, want := snapshot.RouteTable[0].GlobalID, "default:r-api"; got != want {
		t.Fatalf("snapshot.RouteTable[0].GlobalID = %q, want %q", got, want)
	}
	if _, ok := snapshot.Upstreams.Get("default:pool-api"); !ok {
		t.Fatal("snapshot.Upstreams.Get(default:pool-api) returned no pool")
	}
}

func TestDesiredStatePathUsesRaftNamespaceMetadata(t *testing.T) {
	if got, want := DesiredStatePath("admin"), "raft://namespaces/admin"; got != want {
		t.Fatalf("DesiredStatePath(admin) = %q, want %q", got, want)
	}
}

func TestProjectSnapshot_RejectsInvalidDesiredConfig(t *testing.T) {
	_, err := ProjectSnapshot(boot.AppConfig{}, raftstate.Config{}, vipruntime.Config{}, DesiredState{
		Namespaces: map[string]spec.Config{
			"default": {
				Routes: []spec.RouteConfig{{
					ID:           "r-api",
					Enabled:      true,
					Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
					UpstreamPool: "missing",
				}},
				UpstreamPools: map[string]spec.UpstreamPool{},
			},
		},
	})
	if err == nil {
		t.Fatal("ProjectSnapshot() error = nil, want validation error")
	}
}

func TestProjectSnapshot_ProjectsRaftVIPWithLocalInterface(t *testing.T) {
	appCfg := boot.AppConfig{}
	localVIP := vipruntime.Config{Interface: "eth0"}
	state := DesiredState{
		Namespaces: map[string]spec.Config{},
		VIP: &ClusterVIPConfig{
			Address:           "10.10.0.100/24",
			GARPCount:         3,
			GARPInterval:      "100ms",
			AcquireDelay:      "300ms",
			ReleaseOnShutdown: true,
		},
	}

	snapshot, err := ProjectSnapshot(appCfg, raftstate.Config{}, localVIP, state)
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
	localVIP := vipruntime.Config{Interface: "eth0"}
	state := DesiredState{
		Namespaces: map[string]spec.Config{},
		VIP:        &ClusterVIPConfig{Address: "10.10.0.100/24"},
	}

	snapshot, err := ProjectSnapshot(appCfg, raftstate.Config{}, localVIP, state)
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
		Namespaces: map[string]spec.Config{},
		RaftTiming: &ClusterRaftTimingConfig{
			HeartbeatTimeout:   "3s",
			ElectionTimeout:    "5s",
			LeaderLeaseTimeout: "2s",
			CommitTimeout:      "250ms",
		},
	}

	snapshot, err := ProjectSnapshot(boot.AppConfig{}, raftstate.Config{}, vipruntime.Config{}, state)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if got, want := snapshot.RaftTiming.HeartbeatTimeout, "3s"; got != want {
		t.Fatalf("RaftTiming.HeartbeatTimeout = %q, want %q", got, want)
	}
}
