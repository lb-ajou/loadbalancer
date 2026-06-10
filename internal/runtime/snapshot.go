package runtime

import (
	"time"

	"loadbalancer/internal/boot"
	"loadbalancer/internal/config"
	"loadbalancer/internal/route"
	"loadbalancer/internal/spec"
	"loadbalancer/internal/upstream"
)

type Snapshot struct {
	AppConfig    boot.AppConfig
	RaftIdentity config.RaftIdentity
	RaftTiming   config.RaftTiming
	VIP          config.VIPConfig
	ProxyConfig  spec.Config
	RouteTable   []route.Route
	Upstreams    *upstream.Registry
	AppliedAt    time.Time
}
