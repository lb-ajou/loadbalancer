package runtime

import (
	"time"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/route"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/upstream"
)

type Snapshot struct {
	AppConfig    boot.AppConfig
	RaftIdentity config.RaftIdentity
	RaftTiming   config.RaftTiming
	VIP          config.VIPConfig
	ProxyConfigs []spec.LoadedConfig
	RouteTable   []route.Route
	Upstreams    *upstream.Registry
	AppliedAt    time.Time
}
