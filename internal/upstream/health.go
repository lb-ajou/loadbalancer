package upstream

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (p *Pool) CheckTarget(ctx context.Context, client *http.Client, index int) bool {
	if p == nil || client == nil || p.HealthCheck == nil {
		return false
	}
	if index < 0 || index >= len(p.Targets) {
		return false
	}

	timeout, err := time.ParseDuration(p.HealthCheck.Timeout)
	if err != nil {
		p.SetTargetUnhealthy(index, time.Now(), "invalid health check timeout: "+err.Error())
		return false
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.healthCheckURL(index), nil)
	if err != nil {
		p.SetTargetUnhealthy(index, time.Now(), "build health check request: "+err.Error())
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		p.SetTargetUnhealthy(index, time.Now(), err.Error())
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != p.HealthCheck.ExpectStatus {
		p.SetTargetUnhealthy(index, time.Now(), fmt.Sprintf("unexpected status: %d", resp.StatusCode))
		return false
	}

	p.SetTargetHealthy(index, time.Now())
	return true
}

func (p *Pool) CheckTargets(ctx context.Context, client *http.Client) {
	if p == nil || client == nil || p.HealthCheck == nil {
		return
	}

	for i := range p.Targets {
		p.CheckTarget(ctx, client, i)
	}
}

func (p *Pool) HealthInterval() (time.Duration, error) {
	if p == nil || p.HealthCheck == nil {
		return 0, fmt.Errorf("health check is not configured")
	}

	return time.ParseDuration(p.HealthCheck.Interval)
}

func (p *Pool) healthCheckURL(index int) string {
	return "http://" + p.Targets[index].Raw + p.HealthCheck.Path
}
