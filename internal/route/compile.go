package route

import (
	"fmt"
	"regexp"

	"loadbalancer/internal/spec"
)

func BuildTable(cfg spec.Config) ([]Route, error) {
	routes, err := BuildRoutes(cfg)
	if err != nil {
		return nil, err
	}
	Sort(routes)

	return routes, nil
}

func BuildRoutes(cfg spec.Config) ([]Route, error) {
	routes := make([]Route, 0, len(cfg.Routes))
	for _, routeCfg := range cfg.Routes {
		compiled, err := compileRoute(routeCfg)
		if err != nil {
			return nil, err
		}
		routes = append(routes, compiled)
	}
	return routes, nil
}

func routeAlgorithmString(algorithm spec.RouteAlgorithm) string {
	if algorithm == "" {
		return string(spec.RouteAlgorithmRoundRobin)
	}

	return string(algorithm)
}

func compileRoute(routeCfg spec.RouteConfig) (Route, error) {
	path, err := compilePathMatcher(routeCfg.Match.Path)
	if err != nil {
		return Route{}, fmt.Errorf("compile route %q: %w", routeCfg.ID, err)
	}
	return compiledRoute(routeCfg, path), nil
}

func compilePathMatcher(path *spec.PathMatchConfig) (PathMatcher, error) {
	if path == nil {
		return PathMatcher{Kind: PathKindAny}, nil
	}
	switch path.Type {
	case spec.PathMatchExact:
		return pathMatcher(PathKindExact, path.Value), nil
	case spec.PathMatchPrefix:
		return pathMatcher(PathKindPrefix, path.Value), nil
	case spec.PathMatchRegex:
		return compileRegexPathMatcher(path.Value)
	default:
		return PathMatcher{}, fmt.Errorf("unsupported path match type %q", path.Type)
	}
}

func pathMatcher(kind PathKind, value string) PathMatcher {
	return PathMatcher{Kind: kind, Value: value}
}

func compileRegexPathMatcher(value string) (PathMatcher, error) {
	compiled, err := regexp.Compile(value)
	if err != nil {
		return PathMatcher{}, err
	}
	return PathMatcher{Kind: PathKindRegex, Value: value, Regex: compiled}, nil
}

func compiledRoute(routeCfg spec.RouteConfig, path PathMatcher) Route {
	return Route{
		ID:           routeCfg.ID,
		Enabled:      routeCfg.Enabled,
		Hosts:        append([]string(nil), routeCfg.Match.Hosts...),
		Path:         path,
		Algorithm:    routeAlgorithmString(routeCfg.Algorithm),
		UpstreamPool: routeCfg.UpstreamPool,
	}
}
