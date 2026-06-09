package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/state"
)

type stubStateStore struct {
	getCalled     bool
	replaceCalled bool
	config        state.AppliedProxyConfig
	err           error
}

func (s *stubStateStore) GetConfig(context.Context) (state.AppliedProxyConfig, error) {
	s.getCalled = true
	if s.err != nil {
		return state.AppliedProxyConfig{}, s.err
	}
	return s.config, nil
}

func (s *stubStateStore) ReplaceConfig(context.Context, spec.Config) (state.AppliedProxyConfig, error) {
	s.replaceCalled = true
	if s.err != nil {
		return state.AppliedProxyConfig{}, s.err
	}
	return s.config, nil
}

func TestNewWithConfigStateReadsConfig(t *testing.T) {
	appliedAt := time.Unix(1700000000, 0).UTC()
	store := &stubStateStore{
		config: state.AppliedProxyConfig{
			Routes: []spec.RouteConfig{{
				ID:           "r-api",
				Enabled:      true,
				Match:        spec.RouteMatchConfig{Hosts: []string{"api.example.com"}},
				UpstreamPool: "pool-api",
			}},
			UpstreamPools: map[string]spec.UpstreamPool{
				"pool-api": {Upstreams: []string{"10.0.0.11:8080"}},
			},
			AppliedAt: appliedAt,
		},
	}
	service := NewWithConfigState(store)

	view, err := service.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if !store.getCalled {
		t.Fatal("store.GetConfig was not called")
	}
	if got, want := len(view.Routes), 1; got != want {
		t.Fatalf("len(view.Routes) = %d, want %d", got, want)
	}
	if _, ok := view.UpstreamPools["pool-api"]; !ok {
		t.Fatal("view.UpstreamPools[pool-api] missing")
	}
	if got, want := view.AppliedAt, appliedAt; got != want {
		t.Fatalf("view.AppliedAt = %v, want %v", got, want)
	}
}

func TestNewWithConfigStateWritesThroughStore(t *testing.T) {
	store := &stubStateStore{
		config: state.AppliedProxyConfig{
			Routes:        []spec.RouteConfig{{ID: "r-api"}},
			UpstreamPools: map[string]spec.UpstreamPool{},
		},
	}
	service := NewWithConfigState(store)

	view, err := service.ReplaceConfig(context.Background(), spec.Config{})
	if err != nil {
		t.Fatalf("ReplaceConfig() error = %v", err)
	}
	if !store.replaceCalled {
		t.Fatal("store.ReplaceConfig was not called")
	}
	if got, want := view.Routes[0].ID, "r-api"; got != want {
		t.Fatalf("view.Routes[0].ID = %q, want %q", got, want)
	}
}

func TestToAPIErrorPreservesStateErrorMetadata(t *testing.T) {
	err := toAPIError(state.NewNotLeaderError("http://leader:9090"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("toAPIError() error type = %T, want *APIError", err)
	}
	if got, want := apiErr.Code, "not_raft_leader"; got != want {
		t.Fatalf("apiErr.Code = %q, want %q", got, want)
	}
	if got, want := apiErr.LeaderAddress, "http://leader:9090"; got != want {
		t.Fatalf("apiErr.LeaderAddress = %q, want %q", got, want)
	}
}

func TestToAPIErrorPreservesValidationErrors(t *testing.T) {
	err := toAPIError(&state.StateError{
		StatusCode: http.StatusBadRequest,
		Code:       "invalid_config",
		Message:    "invalid proxy config",
		Err: spec.ValidationErrors{{
			Field:   "routes[0].upstream_pool",
			Message: "unknown upstream pool",
		}},
	})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("toAPIError() error type = %T, want *APIError", err)
	}
	if got, want := len(apiErr.ValidationErrors), 1; got != want {
		t.Fatalf("len(apiErr.ValidationErrors) = %d, want %d", got, want)
	}
	if got, want := apiErr.ValidationErrors[0].Field, "routes[0].upstream_pool"; got != want {
		t.Fatalf("apiErr.ValidationErrors[0].Field = %q, want %q", got, want)
	}
}
