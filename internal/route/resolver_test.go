package route

import (
	"testing"

	"reverseproxy-poc/internal/spec"
)

func TestBuildTableAndResolve_AcrossMultipleProxyConfigs(t *testing.T) {
	configs := []spec.LoadedConfig{
		{
			Source: "default",
			Config: spec.Config{
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
				},
			},
		},
		{
			Source: "admin",
			Config: spec.Config{
				Routes: []spec.RouteConfig{
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
			},
		},
	}

	routes, err := BuildTable(configs)
	if err != nil {
		t.Fatalf("BuildTable() error = %v", err)
	}

	matched, ok := Resolve(routes, "api.example.com", "/api/admin/users")
	if !ok {
		t.Fatal("Resolve() returned no route")
	}

	if got, want := matched.GlobalID, "admin:api-admin"; got != want {
		t.Fatalf("Resolve() GlobalID = %q, want %q", got, want)
	}
	if got, want := matched.UpstreamPool, "admin:pool-admin"; got != want {
		t.Fatalf("Resolve() UpstreamPool = %q, want %q", got, want)
	}
}
