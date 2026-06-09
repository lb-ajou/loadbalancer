package route

import "testing"

func TestSort(t *testing.T) {
	routes := []Route{
		{
			ID:   "any",
			Path: PathMatcher{Kind: PathKindAny},
		},
		{
			ID: "regex",
			Path: PathMatcher{
				Kind:  PathKindRegex,
				Value: "^/api/.+/debug$",
			},
		},
		{
			ID: "api",
			Path: PathMatcher{
				Kind:  PathKindPrefix,
				Value: "/api/",
			},
		},
		{
			ID: "users",
			Path: PathMatcher{
				Kind:  PathKindPrefix,
				Value: "/users/",
			},
		},
		{
			ID: "admin",
			Path: PathMatcher{
				Kind:  PathKindPrefix,
				Value: "/api/admin/",
			},
		},
		{
			ID: "login",
			Path: PathMatcher{
				Kind:  PathKindExact,
				Value: "/login",
			},
		},
	}

	Sort(routes)

	want := []string{
		"login",
		"admin",
		"api",
		"users",
		"regex",
		"any",
	}

	for i, route := range routes {
		if route.ID != want[i] {
			t.Fatalf("routes[%d].ID = %q, want %q", i, route.ID, want[i])
		}
	}
}
