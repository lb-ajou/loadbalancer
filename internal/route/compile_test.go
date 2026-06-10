package route

import (
	"testing"

	"loadbalancer/internal/spec"
)

func hostRoute(id, pool string, path *spec.PathMatchConfig) spec.RouteConfig {
	return spec.RouteConfig{
		ID: id, Enabled: true,
		Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}, Path: path},
		UpstreamPool: pool,
	}
}

func tableConfig() spec.Config {
	return spec.Config{Routes: []spec.RouteConfig{
		hostRoute("catchall", "pool-default", nil),
		hostRoute("api", "pool-api", prefixPath("/api/")),
		hostRoute("login", "pool-admin", exactPath("/login")),
	}}
}

func prefixPath(value string) *spec.PathMatchConfig {
	return &spec.PathMatchConfig{Type: spec.PathMatchPrefix, Value: value}
}

func exactPath(value string) *spec.PathMatchConfig {
	return &spec.PathMatchConfig{Type: spec.PathMatchExact, Value: value}
}

func regexRouteConfig() spec.Config {
	return spec.Config{Routes: []spec.RouteConfig{{
		ID: "user", Enabled: true, Algorithm: spec.RouteAlgorithmStickyCookie,
		Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}, Path: regexPath("^/users/[0-9]+$")},
		UpstreamPool: "pool-api",
	}}}
}

func regexPath(value string) *spec.PathMatchConfig {
	return &spec.PathMatchConfig{Type: spec.PathMatchRegex, Value: value}
}

func requireRouteOrder(t *testing.T, routes []Route) {
	t.Helper()
	if got := routes[0].ID; got != "login" {
		t.Fatalf("routes[0].ID = %q, want %q", got, "login")
	}
	if got := routes[1].ID; got != "api" {
		t.Fatalf("routes[1].ID = %q, want %q", got, "api")
	}
	if got := routes[2].ID; got != "catchall" {
		t.Fatalf("routes[2].ID = %q, want %q", got, "catchall")
	}
}

func TestBuildTable_CompilesAndSortsRoutes(t *testing.T) {
	routes, err := BuildTable(tableConfig())
	if err != nil {
		t.Fatalf("BuildTable() error = %v", err)
	}
	requireRouteOrder(t, routes)
	if got, want := routes[1].UpstreamPool, "pool-api"; got != want {
		t.Fatalf("routes[1].UpstreamPool = %q, want %q", got, want)
	}
}

func TestBuildRoutes_CompilesRegex(t *testing.T) {
	routes, err := BuildRoutes(regexRouteConfig())
	if err != nil {
		t.Fatalf("BuildRoutes() error = %v", err)
	}

	if routes[0].Path.Regex == nil {
		t.Fatal("BuildRoutes() did not compile regex")
	}
	if got, want := routes[0].Algorithm, "sticky_cookie"; got != want {
		t.Fatalf("routes[0].Algorithm = %q, want %q", got, want)
	}
}
