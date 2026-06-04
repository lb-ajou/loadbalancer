package spec

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

func (c Config) Validate() []ValidationError {
	var errs []ValidationError
	poolIDs := make(map[string]struct{}, len(c.UpstreamPools))
	errs = append(errs, validateUpstreamPools(c.UpstreamPools, poolIDs)...)
	routeIDs := make(map[string]struct{}, len(c.Routes))
	errs = append(errs, validateRoutes(c.Routes, routeIDs, poolIDs)...)
	return errs
}

func validateRoute(index int, route RouteConfig, routeIDs map[string]struct{}, poolIDs map[string]struct{}) []ValidationError {
	var errs []ValidationError
	base := fmt.Sprintf("routes[%d]", index)
	errs = append(errs, validateRouteID(base, route.ID, routeIDs)...)
	errs = append(errs, validateRouteHosts(base, route.Match.Hosts)...)
	if route.Match.Path != nil {
		errs = append(errs, validatePathMatch(base+".match.path", *route.Match.Path)...)
	}
	errs = append(errs, validateRouteAlgorithm(base, route.Algorithm)...)
	errs = append(errs, validateRouteUpstreamPool(base, route.UpstreamPool, poolIDs)...)
	return errs
}

func validatePathMatch(field string, path PathMatchConfig) []ValidationError {
	if errs := validatePathValue(field, path.Value); errs != nil {
		return errs
	}
	return validateTypedPathMatch(field, path)
}

func validatePathValue(field, value string) []ValidationError {
	if strings.TrimSpace(value) != "" {
		return nil
	}
	return []ValidationError{{Field: field + ".value", Message: "path value must not be empty"}}
}

func validateTypedPathMatch(field string, path PathMatchConfig) []ValidationError {
	switch path.Type {
	case PathMatchExact:
		return validateExactPathMatch(field, path.Value)
	case PathMatchPrefix:
		return validatePrefixPathMatch(field, path.Value)
	case PathMatchRegex:
		return validateRegexPathMatch(field, path.Value)
	default:
		return []ValidationError{{Field: field + ".type", Message: "path.type must be one of: exact, prefix, regex"}}
	}
}

func validateUpstreamPool(poolID string, pool UpstreamPool) []ValidationError {
	var errs []ValidationError
	base := "upstream_pools." + poolID
	errs = append(errs, validateUpstreamEntries(base, pool.Upstreams)...)
	if pool.HealthCheck != nil {
		errs = append(errs, validateHealthCheck(base+".health_check", *pool.HealthCheck)...)
	}
	return errs
}

func validateHealthCheck(field string, hc HealthCheckConfig) []ValidationError {
	var errs []ValidationError
	errs = append(errs, validateHealthCheckPath(field, hc.Path)...)
	errs = append(errs, validateHealthCheckDuration(field+".interval", hc.Interval)...)
	errs = append(errs, validateHealthCheckDuration(field+".timeout", hc.Timeout)...)
	errs = append(errs, validateHealthCheckStatus(field, hc.ExpectStatus)...)
	return errs
}

func validateUpstreamPools(pools map[string]UpstreamPool, poolIDs map[string]struct{}) []ValidationError {
	var errs []ValidationError
	for poolID, pool := range pools {
		errs = append(errs, validateNamedUpstreamPool(poolID, pool, poolIDs)...)
	}
	return errs
}

func validateNamedUpstreamPool(poolID string, pool UpstreamPool, poolIDs map[string]struct{}) []ValidationError {
	if strings.TrimSpace(poolID) == "" {
		return []ValidationError{{Field: "upstream_pools", Message: "pool id must not be empty"}}
	}
	poolIDs[poolID] = struct{}{}
	return validateUpstreamPool(poolID, pool)
}

func validateRoutes(routes []RouteConfig, routeIDs, poolIDs map[string]struct{}) []ValidationError {
	var errs []ValidationError
	for i, route := range routes {
		errs = append(errs, validateRoute(i, route, routeIDs, poolIDs)...)
	}
	return errs
}

func validateRouteID(base, routeID string, routeIDs map[string]struct{}) []ValidationError {
	if strings.TrimSpace(routeID) == "" {
		return []ValidationError{{Field: base + ".id", Message: "route id must not be empty"}}
	}
	if _, exists := routeIDs[routeID]; exists {
		return []ValidationError{{Field: base + ".id", Message: "duplicate route id"}}
	}
	routeIDs[routeID] = struct{}{}
	return nil
}

func validateRouteHosts(base string, hosts []string) []ValidationError {
	if len(hosts) == 0 {
		return []ValidationError{{Field: base + ".match.hosts", Message: "hosts must contain at least one host"}}
	}
	var errs []ValidationError
	for i, host := range hosts {
		errs = append(errs, validateRouteHost(base, i, host)...)
	}
	return errs
}

func validateRouteHost(base string, index int, host string) []ValidationError {
	field := fmt.Sprintf("%s.match.hosts[%d]", base, index)
	var errs []ValidationError
	if strings.TrimSpace(host) == "" {
		errs = append(errs, ValidationError{Field: field, Message: "host must not be empty"})
	}
	if strings.Contains(host, "*") {
		errs = append(errs, ValidationError{Field: field, Message: "wildcard host is not supported in the current schema"})
	}
	return errs
}

func validateRouteAlgorithm(base string, algorithm RouteAlgorithm) []ValidationError {
	if algorithm == "" || isValidRouteAlgorithm(algorithm) {
		return nil
	}
	return []ValidationError{{Field: base + ".algorithm", Message: "algorithm must be one of: round_robin, sticky_cookie, 5_tuple_hash, least_connection"}}
}

func isValidRouteAlgorithm(algorithm RouteAlgorithm) bool {
	return algorithm == RouteAlgorithmRoundRobin ||
		algorithm == RouteAlgorithmStickyCookie ||
		algorithm == RouteAlgorithmFiveTupleHash ||
		algorithm == RouteAlgorithmLeastConnection
}

func validateRouteUpstreamPool(base, poolID string, poolIDs map[string]struct{}) []ValidationError {
	if strings.TrimSpace(poolID) == "" {
		return []ValidationError{{Field: base + ".upstream_pool", Message: "upstream_pool must not be empty"}}
	}
	if _, exists := poolIDs[poolID]; exists {
		return nil
	}
	return []ValidationError{{Field: base + ".upstream_pool", Message: "referenced upstream_pool does not exist"}}
}

func validateExactPathMatch(field, value string) []ValidationError {
	if strings.HasPrefix(value, "/") {
		return nil
	}
	return []ValidationError{{Field: field + ".value", Message: "exact path must start with '/'"}}
}

func validatePrefixPathMatch(field, value string) []ValidationError {
	var errs []ValidationError
	if !strings.HasPrefix(value, "/") {
		errs = append(errs, ValidationError{Field: field + ".value", Message: "prefix path must start with '/'"})
	}
	if value != "/" && !strings.HasSuffix(value, "/") {
		errs = append(errs, ValidationError{Field: field + ".value", Message: "prefix path must be '/' or end with '/'"})
	}
	return errs
}

func validateRegexPathMatch(field, value string) []ValidationError {
	if _, err := regexp.Compile(value); err == nil {
		return nil
	}
	return []ValidationError{{Field: field + ".value", Message: "invalid regex: " + regexpError(value)}}
}

func regexpError(value string) string {
	_, err := regexp.Compile(value)
	return err.Error()
}

func validateUpstreamEntries(base string, upstreams []string) []ValidationError {
	if len(upstreams) == 0 {
		return []ValidationError{{Field: base + ".upstreams", Message: "upstreams must contain at least one entry"}}
	}
	var errs []ValidationError
	for i, item := range upstreams {
		errs = append(errs, validateUpstreamEntry(base, i, item)...)
	}
	return errs
}

func validateUpstreamEntry(base string, index int, upstream string) []ValidationError {
	field := fmt.Sprintf("%s.upstreams[%d]", base, index)
	if strings.TrimSpace(upstream) == "" {
		return []ValidationError{{Field: field, Message: "upstream must not be empty"}}
	}
	host, port, err := net.SplitHostPort(upstream)
	if err == nil && host != "" && port != "" {
		return nil
	}
	return []ValidationError{{Field: field, Message: "upstream must be in host:port or [ipv6]:port form"}}
}

func validateHealthCheckPath(field, path string) []ValidationError {
	if strings.HasPrefix(path, "/") {
		return nil
	}
	return []ValidationError{{Field: field + ".path", Message: "health_check.path must start with '/'"}}
}

func validateHealthCheckDuration(field string, duration Duration) []ValidationError {
	if _, err := duration.Parse(); err == nil {
		return nil
	}
	return []ValidationError{{Field: field, Message: "invalid duration: " + durationError(duration)}}
}

func durationError(duration Duration) string {
	_, err := duration.Parse()
	return err.Error()
}

func validateHealthCheckStatus(field string, status int) []ValidationError {
	if status >= 100 && status <= 599 {
		return nil
	}
	return []ValidationError{{Field: field + ".expect_status", Message: "expect_status must be between 100 and 599"}}
}
