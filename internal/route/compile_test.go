package route

import (
	"testing"

	"reverseproxy-poc/internal/spec"
)

func loadedConfig(source string, routes ...spec.RouteConfig) spec.LoadedConfig {
	return spec.LoadedConfig{Source: source, Config: spec.Config{Routes: routes}}
}

func hostRoute(id, pool string, path *spec.PathMatchConfig) spec.RouteConfig {
	return spec.RouteConfig{
		ID: id, Enabled: true,
		Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}, Path: path},
		UpstreamPool: pool,
	}
}

func tableConfigs() []spec.LoadedConfig {
	return []spec.LoadedConfig{
		loadedConfig("default", hostRoute("catchall", "pool-default", nil), hostRoute("api", "pool-api", prefixPath("/api/"))),
		loadedConfig("admin", hostRoute("login", "pool-admin", exactPath("/login"))),
	}
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
	if got := routes[0].GlobalID; got != "admin:login" {
		t.Fatalf("routes[0].GlobalID = %q, want %q", got, "admin:login")
	}
	if got := routes[1].GlobalID; got != "default:api" {
		t.Fatalf("routes[1].GlobalID = %q, want %q", got, "default:api")
	}
	if got := routes[2].GlobalID; got != "default:catchall" {
		t.Fatalf("routes[2].GlobalID = %q, want %q", got, "default:catchall")
	}
}

func TestBuildTable_GlobalizesIDsAndSortsRoutes(t *testing.T) {
	routes, err := BuildTable(tableConfigs())
	if err != nil {
		t.Fatalf("BuildTable() error = %v", err)
	}
	requireRouteOrder(t, routes)
	if got, want := routes[1].UpstreamPool, "default:pool-api"; got != want {
		t.Fatalf("routes[1].UpstreamPool = %q, want %q", got, want)
	}
}

func TestBuildRoutes_CompilesRegex(t *testing.T) {
	routes, err := BuildRoutes("default", regexRouteConfig())
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
