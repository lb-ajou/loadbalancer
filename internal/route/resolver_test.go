package route

import (
	"testing"

	"loadbalancer/internal/spec"
)

func TestBuildTableAndResolve_UsesPlainRouteAndPoolIDs(t *testing.T) {
	cfg := spec.Config{
		Routes: []spec.RouteConfig{
			{
				ID:      "api",
				Enabled: true,
				Match: spec.RouteMatchConfig{
					Hosts: []string{"api.example.com"},
					Path: &spec.PathMatchConfig{
						Type:  spec.PathMatchPrefix,
						Value: "/api/",
					},
				},
				UpstreamPool: "pool-api",
			},
			{
				ID:      "api-admin",
				Enabled: true,
				Match: spec.RouteMatchConfig{
					Hosts: []string{"api.example.com"},
					Path: &spec.PathMatchConfig{
						Type:  spec.PathMatchPrefix,
						Value: "/api/admin/",
					},
				},
				UpstreamPool: "pool-admin",
			},
		},
	}

	routes, err := BuildTable(cfg)
	if err != nil {
		t.Fatalf("BuildTable() error = %v", err)
	}

	matched, ok := Resolve(routes, "api.example.com", "/api/admin/users")
	if !ok {
		t.Fatal("Resolve() returned no route")
	}

	if got, want := matched.ID, "api-admin"; got != want {
		t.Fatalf("Resolve() ID = %q, want %q", got, want)
	}
	if got, want := matched.UpstreamPool, "pool-admin"; got != want {
		t.Fatalf("Resolve() UpstreamPool = %q, want %q", got, want)
	}
}
