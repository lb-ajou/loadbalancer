package upstream

import (
	"testing"
	"time"
)

func TestPoolNextTarget_RoundRobin(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	requireNextTargets(t, pool, "10.0.0.11:8080", "10.0.0.12:8080", "10.0.0.11:8080")
}

func TestPoolNextTarget_SkipsUnhealthyTargets(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080", "10.0.0.13:8080")
	requireSetTargetUnhealthy(t, pool, 1, "connection refused")
	requireNextTargets(t, pool, "10.0.0.11:8080", "10.0.0.13:8080", "10.0.0.11:8080")
}

func TestPoolNextTarget_ReturnsFalseWhenNoHealthyTargets(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	requireSetTargetUnhealthy(t, pool, 0, "timeout")
	requireSetTargetUnhealthy(t, pool, 1, "timeout")
	if _, ok := pool.NextTarget(); ok {
		t.Fatal("NextTarget() returned healthy target when none should exist")
	}
}

func TestPoolHashTarget_ReusesStableTarget(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	first := requireHashTarget(t, pool, "client-a")
	second := requireHashTarget(t, pool, "client-a")
	if first.Raw != second.Raw {
		t.Fatalf("HashTarget() mismatch: %q != %q", first.Raw, second.Raw)
	}
}

func TestPoolHashTarget_SkipsUnhealthyTargets(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	requireSetTargetUnhealthy(t, pool, 0, "down")
	target := requireHashTarget(t, pool, "client-a")
	if got, want := target.Raw, "10.0.0.12:8080"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestPoolLeastConnectionTarget_PrefersLowerActiveCount(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	first, releaseFirst := requireLeastConnectionTarget(t, pool)
	if got, want := first.Raw, "10.0.0.11:8080"; got != want {
		t.Fatalf("first target = %q, want %q", got, want)
	}
	second, releaseSecond := requireLeastConnectionTarget(t, pool)
	if got, want := second.Raw, "10.0.0.12:8080"; got != want {
		t.Fatalf("second target = %q, want %q", got, want)
	}
	releaseSecond()
	releaseFirst()
}

func TestPoolLeastConnectionTarget_UsesRoundRobinOnTie(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	first, releaseFirst := requireLeastConnectionTarget(t, pool)
	releaseFirst()
	second, releaseSecond := requireLeastConnectionTarget(t, pool)
	releaseSecond()
	if got, want := first.Raw, "10.0.0.11:8080"; got != want {
		t.Fatalf("first target = %q, want %q", got, want)
	}
	if got, want := second.Raw, "10.0.0.12:8080"; got != want {
		t.Fatalf("second target = %q, want %q", got, want)
	}
}

func TestPoolLeastConnectionTarget_SkipsUnhealthyTargets(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080", "10.0.0.12:8080")
	requireSetTargetUnhealthy(t, pool, 0, "down")
	target, release := requireLeastConnectionTarget(t, pool)
	defer release()
	if got, want := target.Raw, "10.0.0.12:8080"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestPoolLeastConnectionTarget_ReleaseDecrementsCounterOnce(t *testing.T) {
	pool := newTestPool("10.0.0.11:8080")
	_, release := requireLeastConnectionTarget(t, pool)
	if got, want := pool.ActiveConnections(0), uint64(1); got != want {
		t.Fatalf("ActiveConnections() = %d, want %d", got, want)
	}
	release()
	release()
	if got, want := pool.ActiveConnections(0), uint64(0); got != want {
		t.Fatalf("ActiveConnections() = %d, want %d", got, want)
	}
}

func newTestPool(raws ...string) *Pool {
	targets := make([]Target, 0, len(raws))
	states := make([]TargetState, 0, len(raws))
	for _, raw := range raws {
		targets = append(targets, Target{Raw: raw})
		states = append(states, TargetState{Healthy: true})
	}
	return &Pool{Targets: targets, targetState: states, active: make([]uint64, len(raws))}
}

func requireNextTargets(t *testing.T, pool *Pool, wants ...string) {
	t.Helper()
	for i, want := range wants {
		target, ok := pool.NextTarget()
		if !ok {
			t.Fatalf("NextTarget() returned no target on call %d", i+1)
		}
		if target.Raw != want {
			t.Fatalf("target on call %d = %q, want %q", i+1, target.Raw, want)
		}
	}
}

func requireSetTargetUnhealthy(t *testing.T, pool *Pool, index int, reason string) {
	t.Helper()
	if ok := pool.SetTargetUnhealthy(index, time.Now(), reason); !ok {
		t.Fatal("SetTargetUnhealthy() returned false")
	}
}

func requireHashTarget(t *testing.T, pool *Pool, key string) Target {
	t.Helper()
	target, ok := pool.HashTarget(key)
	if !ok {
		t.Fatal("HashTarget() returned no target")
	}
	return target
}

func requireLeastConnectionTarget(t *testing.T, pool *Pool) (Target, func()) {
	t.Helper()
	target, release, ok := pool.LeastConnectionTarget()
	if !ok {
		t.Fatal("LeastConnectionTarget() returned no target")
	}
	return target, release
}
