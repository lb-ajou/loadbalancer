package config

type RaftConfig struct {
	Identity RaftIdentity
	Timing   RaftTiming
}

type RaftIdentity struct {
	NodeID        string
	BindAddr      string
	AdvertiseAddr string
}

type RaftTiming struct {
	HeartbeatTimeout   string
	ElectionTimeout    string
	LeaderLeaseTimeout string
	CommitTimeout      string
}
