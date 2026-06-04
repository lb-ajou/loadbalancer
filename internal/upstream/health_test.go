package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPoolCheckTarget_MarksHealthyOnExpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/health")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pool := newHealthTestPool(server.Listener.Addr().String(), 200)

	ok := pool.CheckTarget(context.Background(), server.Client(), 0)
	if !ok {
		t.Fatal("CheckTarget() returned false")
	}

	state := pool.SnapshotStates()[0]
	if !state.Healthy {
		t.Fatal("state.Healthy = false, want true")
	}
	if state.LastCheckedAt.IsZero() {
		t.Fatal("state.LastCheckedAt is zero")
	}
	if state.LastError != "" {
		t.Fatalf("state.LastError = %q, want empty", state.LastError)
	}
}

func TestPoolCheckTarget_MarksUnhealthyOnUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	pool := newHealthTestPool(server.Listener.Addr().String(), 200)

	ok := pool.CheckTarget(context.Background(), server.Client(), 0)
	if ok {
		t.Fatal("CheckTarget() returned true, want false")
	}

	state := pool.SnapshotStates()[0]
	if state.Healthy {
		t.Fatal("state.Healthy = true, want false")
	}
	if !strings.Contains(state.LastError, "unexpected status") {
		t.Fatalf("state.LastError = %q, want unexpected status", state.LastError)
	}
}

func TestPoolCheckTargets_ChecksAllTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := server.Listener.Addr().String()
	pool := &Pool{
		Targets: []Target{
			{Raw: addr},
			{Raw: addr},
		},
		HealthCheck: &HealthCheck{
			Path:         "/health",
			Interval:     "30s",
			Timeout:      "1s",
			ExpectStatus: 200,
		},
		targetState: []TargetState{
			{Healthy: true},
			{Healthy: true},
		},
	}

	pool.CheckTargets(context.Background(), server.Client())

	states := pool.SnapshotStates()
	for i, state := range states {
		if !state.Healthy {
			t.Fatalf("states[%d].Healthy = false, want true", i)
		}
		if state.LastCheckedAt.IsZero() {
			t.Fatalf("states[%d].LastCheckedAt is zero", i)
		}
	}
}

func TestPoolHealthInterval(t *testing.T) {
	pool := &Pool{
		HealthCheck: &HealthCheck{
			Interval: "30s",
		},
	}

	got, err := pool.HealthInterval()
	if err != nil {
		t.Fatalf("HealthInterval() error = %v", err)
	}
	if got != 30*time.Second {
		t.Fatalf("HealthInterval() = %v, want %v", got, 30*time.Second)
	}
}

func newHealthTestPool(addr string, expectStatus int) *Pool {
	return &Pool{
		Targets: []Target{
			{Raw: addr},
		},
		HealthCheck: &HealthCheck{
			Path:         "/health",
			Interval:     "30s",
			Timeout:      "1s",
			ExpectStatus: expectStatus,
		},
		targetState: []TargetState{
			{Healthy: true},
		},
	}
}
