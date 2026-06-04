package spec

import "time"

type Config struct {
	Name          string                  `json:"name,omitempty"`
	Routes        []RouteConfig           `json:"routes"`
	UpstreamPools map[string]UpstreamPool `json:"upstream_pools"`
}

type RouteConfig struct {
	ID           string           `json:"id"`
	Enabled      bool             `json:"enabled"`
	Match        RouteMatchConfig `json:"match"`
	Algorithm    RouteAlgorithm   `json:"algorithm,omitempty"`
	UpstreamPool string           `json:"upstream_pool"`
}

type RouteAlgorithm string

const (
	RouteAlgorithmRoundRobin      RouteAlgorithm = "round_robin"
	RouteAlgorithmStickyCookie    RouteAlgorithm = "sticky_cookie"
	RouteAlgorithmFiveTupleHash   RouteAlgorithm = "5_tuple_hash"
	RouteAlgorithmLeastConnection RouteAlgorithm = "least_connection"
)

type RouteMatchConfig struct {
	Hosts []string         `json:"hosts"`
	Path  *PathMatchConfig `json:"path,omitempty"`
}

type PathMatchType string

const (
	PathMatchExact  PathMatchType = "exact"
	PathMatchPrefix PathMatchType = "prefix"
	PathMatchRegex  PathMatchType = "regex"
)

type PathMatchConfig struct {
	Type  PathMatchType `json:"type"`
	Value string        `json:"value"`
}

type UpstreamPool struct {
	Upstreams   []string           `json:"upstreams"`
	HealthCheck *HealthCheckConfig `json:"health_check,omitempty"`
}

type HealthCheckConfig struct {
	Path         string   `json:"path"`
	Interval     Duration `json:"interval"`
	Timeout      Duration `json:"timeout"`
	ExpectStatus int      `json:"expect_status"`
}

type Duration string

func (d Duration) Parse() (time.Duration, error) {
	return time.ParseDuration(string(d))
}

type LoadedConfig struct {
	Source string
	Path   string
	Config Config
}
