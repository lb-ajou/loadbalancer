package spec

import "testing"

func validConfig() Config {
	return Config{
		Routes: []RouteConfig{{
			ID: "r-api", Enabled: true, Algorithm: RouteAlgorithmStickyCookie,
			Match: RouteMatchConfig{
				Hosts: []string{"api.example.com"},
				Path:  &PathMatchConfig{Type: PathMatchPrefix, Value: "/api/"},
			},
			UpstreamPool: "pool-api",
		}},
		UpstreamPools: map[string]UpstreamPool{"pool-api": {Upstreams: []string{"10.0.0.11:8080"}}},
	}
}

func requireValidationError(t *testing.T, cfg Config) {
	t.Helper()
	if errs := cfg.Validate(); len(errs) == 0 {
		t.Fatal("Validate() returned no errors")
	}
}

func TestConfigValidate_Success(t *testing.T) {
	if errs := validConfig().Validate(); len(errs) > 0 {
		t.Fatalf("Validate() errors = %v", errs)
	}
}

func TestConfigValidate_AcceptsFiveTupleHash(t *testing.T) {
	cfg := validConfig()
	cfg.Routes[0].Algorithm = RouteAlgorithmFiveTupleHash

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("Validate() errors = %v", errs)
	}
}

func TestConfigValidate_AcceptsLeastConnection(t *testing.T) {
	cfg := validConfig()
	cfg.Routes[0].Algorithm = RouteAlgorithmLeastConnection

	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("Validate() errors = %v", errs)
	}
}

func TestConfigValidate_UnknownUpstreamPool(t *testing.T) {
	cfg := validConfig()
	cfg.Routes[0].UpstreamPool = "missing-pool"
	cfg.Routes[0].Match.Path = nil
	requireValidationError(t, cfg)
}

func TestConfigValidate_PrefixMustEndWithSlashUnlessRoot(t *testing.T) {
	cfg := validConfig()
	cfg.Routes[0].Algorithm = ""
	cfg.Routes[0].Match.Path.Value = "/api"
	requireValidationError(t, cfg)
}

func TestConfigValidate_RejectsUnknownAlgorithm(t *testing.T) {
	cfg := validConfig()
	cfg.Routes[0].Algorithm = RouteAlgorithm("least_conn")
	cfg.Routes[0].Match.Path = nil
	requireValidationError(t, cfg)
}
