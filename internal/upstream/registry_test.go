package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckerStart_UpdatesPoolHealthState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	registry, err := NewRegistry([]Pool{
		{
			GlobalID: "default:pool-api",
			Targets: []Target{
				{Raw: server.Listener.Addr().String()},
			},
			HealthCheck: &HealthCheck{
				Path:         "/health",
				Interval:     "20ms",
				Timeout:      "1s",
				ExpectStatus: 200,
			},
			targetState: []TargetState{
				{Healthy: false},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	checker := NewChecker(registry)
	checker.client = server.Client()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker.Start(ctx)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		pool, ok := registry.Get("default:pool-api")
		if !ok {
			t.Fatal("registry.Get() returned no pool")
		}
		state := pool.SnapshotStates()[0]
		if state.Healthy && !state.LastCheckedAt.IsZero() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("checker did not update pool health state in time")
}
