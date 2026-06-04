package upstream

import (
	"hash/fnv"
	"sync/atomic"
)

func (p *Pool) NextTarget() (Target, bool) {
	if len(p.Targets) == 0 {
		return Target{}, false
	}
	healthyIndexes := p.healthyTargetIndexes()
	if len(healthyIndexes) == 0 {
		return Target{}, false
	}
	index := atomic.AddUint64(&p.next, 1) - 1
	targetIndex := healthyIndexes[index%uint64(len(healthyIndexes))]
	return p.Targets[targetIndex], true
}

func (p *Pool) HashTarget(key string) (Target, bool) {
	healthyIndexes := p.healthyTargetIndexes()
	if len(healthyIndexes) == 0 {
		return Target{}, false
	}
	targetIndex := healthyIndexes[hashIndex(key, len(healthyIndexes))]
	return p.Targets[targetIndex], true
}

func (p *Pool) LeastConnectionTarget() (Target, func(), bool) {
	healthyIndexes := p.healthyTargetIndexes()
	index, ok := p.leastConnectionIndex(healthyIndexes)
	if !ok {
		return Target{}, noopRelease, false
	}
	atomic.AddUint64(&p.active[index], 1)
	return p.Targets[index], p.releaseFunc(index), true
}

func (p *Pool) healthyTargetIndexes() []int {
	if indexes, ok := p.cachedHealthyIndexes(); ok {
		return indexes
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return collectHealthyIndexes(p.Targets, p.targetState)
}

func (p *Pool) leastConnectionIndex(healthyIndexes []int) (int, bool) {
	if len(healthyIndexes) == 0 {
		return 0, false
	}
	candidates := p.lowestActiveIndexes(healthyIndexes)
	return p.nextHealthyIndex(candidates), true
}

func (p *Pool) lowestActiveIndexes(healthyIndexes []int) []int {
	lowest := p.ActiveConnections(healthyIndexes[0])
	indexes := make([]int, 0, len(healthyIndexes))
	for _, index := range healthyIndexes {
		active := p.ActiveConnections(index)
		if active < lowest {
			lowest = active
			indexes = indexes[:0]
		}
		if active == lowest {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (p *Pool) nextHealthyIndex(indexes []int) int {
	index := atomic.AddUint64(&p.next, 1) - 1
	return indexes[index%uint64(len(indexes))]
}

func (p *Pool) releaseFunc(index int) func() {
	var once atomic.Bool
	return func() {
		if once.CompareAndSwap(false, true) {
			atomic.AddUint64(&p.active[index], ^uint64(0))
		}
	}
}

func noopRelease() {}

func hashIndex(key string, size int) int {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(key))
	return int(hasher.Sum64() % uint64(size))
}
