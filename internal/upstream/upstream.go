package upstream

import (
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type Pool struct {
	GlobalID    string
	LocalID     string
	Source      string
	Targets     []Target
	HealthCheck *HealthCheck
	mu          sync.RWMutex
	targetState []TargetState
	active      []uint64
	healthy     atomic.Value
	next        uint64
}

func (p *Pool) ActiveConnections(index int) uint64 {
	if index < 0 || index >= len(p.active) {
		return 0
	}
	return atomic.LoadUint64(&p.active[index])
}

type Target struct {
	Raw string
	URL *url.URL
}

type TargetState struct {
	Healthy       bool
	LastCheckedAt time.Time
	LastError     string
}

type HealthCheck struct {
	Path         string
	Interval     string
	Timeout      string
	ExpectStatus int
}

func (p *Pool) SnapshotStates() []TargetState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return append([]TargetState(nil), p.targetState...)
}

func (p *Pool) SetTargetHealthy(index int, checkedAt time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.setTargetState(index, checkedAt, "", true)
}

func (p *Pool) SetTargetUnhealthy(index int, checkedAt time.Time, lastErr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.setTargetState(index, checkedAt, lastErr, false)
}

func (p *Pool) setTargetState(index int, checkedAt time.Time, lastErr string, healthy bool) bool {
	p.ensureTargetStatesLocked()
	if index < 0 || index >= len(p.targetState) {
		return false
	}
	p.targetState[index] = TargetState{
		Healthy:       healthy,
		LastCheckedAt: checkedAt,
		LastError:     lastErr,
	}
	p.storeHealthyIndexesLocked()
	return true
}

func (p *Pool) ensureTargetStatesLocked() {
	if len(p.targetState) == len(p.Targets) {
		return
	}
	states := make([]TargetState, len(p.Targets))
	for i := range p.Targets {
		states[i] = TargetState{Healthy: true}
		if i < len(p.targetState) {
			states[i] = p.targetState[i]
		}
	}
	p.targetState = states
}

func (p *Pool) cachedHealthyIndexes() ([]int, bool) {
	value := p.healthy.Load()
	if value == nil {
		return nil, false
	}
	indexes, ok := value.([]int)
	return indexes, ok
}

func (p *Pool) storeHealthyIndexesLocked() {
	p.healthy.Store(collectHealthyIndexes(p.Targets, p.targetState))
}

func collectHealthyIndexes(targets []Target, states []TargetState) []int {
	indexes := make([]int, 0, len(targets))
	for i := range targets {
		if i >= len(states) || states[i].Healthy {
			indexes = append(indexes, i)
		}
	}
	return indexes
}
