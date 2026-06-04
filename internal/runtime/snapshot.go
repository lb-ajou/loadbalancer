package runtime

import (
	"time"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/raftstate"
	"reverseproxy-poc/internal/route"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/upstream"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
)

type Snapshot struct {
	AppConfig    boot.AppConfig
	RaftIdentity raftstate.Identity
	RaftTiming   raftstate.Timing
	VIP          vipruntime.Config
	ProxyConfigs []spec.LoadedConfig
	RouteTable   []route.Route
	Upstreams    *upstream.Registry
	AppliedAt    time.Time
}
