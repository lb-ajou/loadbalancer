package route

import (
	"sort"
	"strings"
)

func Sort(routes []Route) {
	sort.SliceStable(routes, func(i, j int) bool {
		return Less(routes[i], routes[j])
	})
}

func Less(a, b Route) bool {
	aRank, aDepth := sortKey(a.Path)
	bRank, bDepth := sortKey(b.Path)

	if aRank != bRank {
		return aRank > bRank
	}
	if aDepth != bDepth {
		return aDepth > bDepth
	}

	return a.GlobalID < b.GlobalID
}

func sortKey(path PathMatcher) (rank int, depth int) {
	switch path.Kind {
	case PathKindExact:
		return 4, 0
	case PathKindPrefix:
		return 3, prefixDepth(path.Value)
	case PathKindRegex:
		return 2, 0
	case PathKindAny:
		return 1, 0
	default:
		return 0, 0
	}
}

func prefixDepth(value string) int {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return 0
	}

	return len(strings.Split(trimmed, "/"))
}
