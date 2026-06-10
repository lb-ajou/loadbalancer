package upstream

import (
	"testing"

	"loadbalancer/internal/spec"
)

func TestBuildRegistry_UsesPoolIDsDirectly(t *testing.T) {
	cfg := spec.Config{
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {
				Upstreams: []string{"10.0.0.11:8080"},
			},
		},
	}

	registry, err := BuildRegistry(cfg)
	if err != nil {
		t.Fatalf("BuildRegistry() error = %v", err)
	}

	if _, ok := registry.Get("pool-api"); !ok {
		t.Fatal("registry.Get(pool-api) returned no pool")
	}
}

func TestBuildPools_CopiesHealthCheck(t *testing.T) {
	cfg := spec.Config{
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {
				Upstreams: []string{"10.0.0.11:8080"},
				HealthCheck: &spec.HealthCheckConfig{
					Path:         "/health",
					Interval:     "30s",
					Timeout:      "3s",
					ExpectStatus: 200,
				},
			},
		},
	}

	pools, err := BuildPools(cfg)
	if err != nil {
		t.Fatalf("BuildPools() error = %v", err)
	}

	if pools[0].HealthCheck == nil {
		t.Fatal("BuildPools() did not copy health check")
	}
	if got, want := pools[0].HealthCheck.Path, "/health"; got != want {
		t.Fatalf("HealthCheck.Path = %q, want %q", got, want)
	}
}

func TestBuildPools_InitializesHealthyTargetState(t *testing.T) {
	cfg := spec.Config{
		UpstreamPools: map[string]spec.UpstreamPool{
			"pool-api": {
				Upstreams: []string{"10.0.0.11:8080", "10.0.0.12:8080"},
			},
		},
	}

	pools, err := BuildPools(cfg)
	if err != nil {
		t.Fatalf("BuildPools() error = %v", err)
	}

	states := pools[0].SnapshotStates()
	if got, want := len(states), 2; got != want {
		t.Fatalf("len(SnapshotStates()) = %d, want %d", got, want)
	}
	for i, state := range states {
		if !state.Healthy {
			t.Fatalf("states[%d].Healthy = false, want true", i)
		}
		if !state.LastCheckedAt.IsZero() {
			t.Fatalf("states[%d].LastCheckedAt = %v, want zero value", i, state.LastCheckedAt)
		}
		if state.LastError != "" {
			t.Fatalf("states[%d].LastError = %q, want empty", i, state.LastError)
		}
	}
}
