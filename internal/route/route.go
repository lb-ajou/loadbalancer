package route

import "regexp"

type Route struct {
	ID           string
	Enabled      bool
	Hosts        []string
	Path         PathMatcher
	Algorithm    string
	UpstreamPool string
}

type PathKind int

const (
	PathKindAny PathKind = iota
	PathKindExact
	PathKindPrefix
	PathKindRegex
)

type PathMatcher struct {
	Kind  PathKind
	Value string
	Regex *regexp.Regexp
}
