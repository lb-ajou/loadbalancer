package state

import "testing"

func TestValidateClusterVIPAcceptsValidConfig(t *testing.T) {
	vip := ClusterVIPConfig{
		Address:           "10.10.0.100/24",
		GARPCount:         3,
		GARPInterval:      "100ms",
		AcquireDelay:      "300ms",
		ReleaseOnShutdown: true,
	}

	if err := ValidateClusterVIP(vip); err != nil {
		t.Fatalf("ValidateClusterVIP() error = %v", err)
	}
}

func TestNormalizeClusterVIPAppliesPolicyDefaults(t *testing.T) {
	vip := NormalizeClusterVIP(ClusterVIPConfig{Address: "10.10.0.100/24"})

	if vip.GARPCount != DefaultVIPGARPCount ||
		vip.GARPInterval != DefaultVIPGARPInterval ||
		vip.AcquireDelay != DefaultVIPAcquireDelay {
		t.Fatalf("NormalizeClusterVIP() = %+v, want default policy", vip)
	}
}

func TestValidateClusterVIPAcceptsAddressOnlyWithDefaults(t *testing.T) {
	vip := ClusterVIPConfig{Address: "10.10.0.100/24"}

	if err := ValidateClusterVIP(vip); err != nil {
		t.Fatalf("ValidateClusterVIP() error = %v", err)
	}
}

func TestValidateClusterVIPRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		vip  ClusterVIPConfig
	}{
		{name: "bad address", vip: ClusterVIPConfig{Address: "bad"}},
		{name: "ipv6 address", vip: ClusterVIPConfig{Address: "2001:db8::10/64"}},
		{name: "bad garp interval", vip: ClusterVIPConfig{
			Address:      "10.10.0.100/24",
			GARPInterval: "soon",
		}},
		{name: "bad acquire delay", vip: ClusterVIPConfig{
			Address:      "10.10.0.100/24",
			AcquireDelay: "soon",
		}},
		{name: "negative garp count", vip: ClusterVIPConfig{
			Address:   "10.10.0.100/24",
			GARPCount: -1,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateClusterVIP(tt.vip); err == nil {
				t.Fatal("ValidateClusterVIP() error = nil, want error")
			}
		})
	}
}

func TestValidateClusterRaftTimingAcceptsValidConfig(t *testing.T) {
	timing := ClusterRaftTimingConfig{
		HeartbeatTimeout:   "3s",
		ElectionTimeout:    "5s",
		LeaderLeaseTimeout: "2s",
		CommitTimeout:      "250ms",
	}

	if err := ValidateClusterRaftTiming(timing); err != nil {
		t.Fatalf("ValidateClusterRaftTiming() error = %v", err)
	}
}

func TestValidateClusterRaftTimingRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		timing ClusterRaftTimingConfig
	}{
		{name: "bad heartbeat", timing: ClusterRaftTimingConfig{HeartbeatTimeout: "soon"}},
		{name: "bad election", timing: ClusterRaftTimingConfig{ElectionTimeout: "soon"}},
		{name: "bad leader lease", timing: ClusterRaftTimingConfig{LeaderLeaseTimeout: "soon"}},
		{name: "bad commit", timing: ClusterRaftTimingConfig{CommitTimeout: "soon"}},
		{name: "zero heartbeat", timing: ClusterRaftTimingConfig{HeartbeatTimeout: "0s"}},
		{name: "negative heartbeat", timing: ClusterRaftTimingConfig{HeartbeatTimeout: "-1s"}},
		{name: "election below heartbeat", timing: ClusterRaftTimingConfig{
			HeartbeatTimeout: "5s",
			ElectionTimeout:  "3s",
		}},
		{name: "lease above heartbeat", timing: ClusterRaftTimingConfig{
			HeartbeatTimeout:   "3s",
			LeaderLeaseTimeout: "5s",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateClusterRaftTiming(tt.timing); err == nil {
				t.Fatal("ValidateClusterRaftTiming() error = nil, want error")
			}
		})
	}
}
