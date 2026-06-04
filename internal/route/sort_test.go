package route

import "testing"

func TestSort(t *testing.T) {
	routes := []Route{
		{
			GlobalID: "default:any",
			Path:     PathMatcher{Kind: PathKindAny},
		},
		{
			GlobalID: "default:regex",
			Path: PathMatcher{
				Kind:  PathKindRegex,
				Value: "^/api/.+/debug$",
			},
		},
		{
			GlobalID: "default:api",
			Path: PathMatcher{
				Kind:  PathKindPrefix,
				Value: "/api/",
			},
		},
		{
			GlobalID: "default:users",
			Path: PathMatcher{
				Kind:  PathKindPrefix,
				Value: "/users/",
			},
		},
		{
			GlobalID: "default:admin",
			Path: PathMatcher{
				Kind:  PathKindPrefix,
				Value: "/api/admin/",
			},
		},
		{
			GlobalID: "default:login",
			Path: PathMatcher{
				Kind:  PathKindExact,
				Value: "/login",
			},
		},
	}

	Sort(routes)

	want := []string{
		"default:login",
		"default:admin",
		"default:api",
		"default:users",
		"default:regex",
		"default:any",
	}

	for i, route := range routes {
		if route.GlobalID != want[i] {
			t.Fatalf("routes[%d].GlobalID = %q, want %q", i, route.GlobalID, want[i])
		}
	}
}
