package raftstate

type Config struct {
	Identity Identity
	Timing   Timing
}

type Identity struct {
	NodeID        string
	BindAddr      string
	AdvertiseAddr string
}

type Timing struct {
	HeartbeatTimeout   string
	ElectionTimeout    string
	LeaderLeaseTimeout string
	CommitTimeout      string
}
