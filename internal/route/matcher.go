package route

import (
	"net"
	"strings"
)

func MatchHost(hosts []string, reqHost string) bool {
	normalizedHost := NormalizeHost(reqHost)
	for _, host := range hosts {
		if host == normalizedHost {
			return true
		}
	}

	return false
}

func MatchPath(path PathMatcher, reqPath string) bool {
	normalizedPath := NormalizePath(reqPath)

	switch path.Kind {
	case PathKindAny:
		return true
	case PathKindExact:
		return normalizedPath == path.Value
	case PathKindPrefix:
		return MatchSegmentPrefix(path.Value, normalizedPath)
	case PathKindRegex:
		return path.Regex != nil && path.Regex.MatchString(normalizedPath)
	default:
		return false
	}
}

func MatchSegmentPrefix(prefix, reqPath string) bool {
	if prefix == "/" {
		return strings.HasPrefix(reqPath, "/")
	}

	base := strings.TrimRight(prefix, "/")
	return reqPath == base || strings.HasPrefix(reqPath, base+"/")
}

func MatchRoute(route Route, reqHost, reqPath string) bool {
	if !route.Enabled {
		return false
	}

	return MatchHost(route.Hosts, reqHost) && MatchPath(route.Path, reqPath)
}

func NormalizeHost(hostport string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err == nil && host != "" && port != "" {
		return host
	}

	return hostport
}

func NormalizePath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}

	return rawPath
}
