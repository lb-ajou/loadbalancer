package route

import "testing"

func TestMatchSegmentPrefix(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		reqPath string
		want    bool
	}{
		{name: "match base path", prefix: "/api/", reqPath: "/api", want: true},
		{name: "match slash path", prefix: "/api/", reqPath: "/api/", want: true},
		{name: "match nested path", prefix: "/api/", reqPath: "/api/v1", want: true},
		{name: "reject string prefix only", prefix: "/api/", reqPath: "/apiv1", want: false},
	}

	for _, tt := range tests {
		if got := MatchSegmentPrefix(tt.prefix, tt.reqPath); got != tt.want {
			t.Fatalf("%s: MatchSegmentPrefix() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestResolve(t *testing.T) {
	routes := []Route{
		{
			GlobalID:     "default:api",
			Enabled:      true,
			Hosts:        []string{"api.example.com"},
			Path:         PathMatcher{Kind: PathKindPrefix, Value: "/api/"},
			UpstreamPool: "default:pool-api",
		},
		{
			GlobalID:     "default:any",
			Enabled:      true,
			Hosts:        []string{"api.example.com"},
			Path:         PathMatcher{Kind: PathKindAny},
			UpstreamPool: "default:pool-default",
		},
	}

	route, ok := Resolve(routes, "api.example.com", "/api/v1/users")
	if !ok {
		t.Fatal("Resolve() returned no match")
	}
	if got, want := route.GlobalID, "default:api"; got != want {
		t.Fatalf("Resolve() global id = %q, want %q", got, want)
	}
}
