package boot

type AppConfig struct {
	ProxyListenAddr     string `json:"proxyListenAddr"`
	DashboardListenAddr string `json:"dashboardListenAddr"`
	RaftDataDir         string `json:"raftDataDir,omitempty"`
}

const DefaultRaftDataDir = "data/raft"

func Normalize(cfg AppConfig) (AppConfig, error) {
	applyBaseDefaults(&cfg)
	applyRaftDefaults(&cfg)
	if err := Validate(cfg); err != nil {
		return AppConfig{}, err
	}
	return cfg, nil
}

func applyBaseDefaults(cfg *AppConfig) {
	if cfg.ProxyListenAddr == "" {
		cfg.ProxyListenAddr = ":8080"
	}
	if cfg.DashboardListenAddr == "" {
		cfg.DashboardListenAddr = ":9090"
	}
}

func applyRaftDefaults(cfg *AppConfig) {
	defaultString := func(value, fallback string) string {
		if value == "" {
			return fallback
		}
		return value
	}
	cfg.RaftDataDir = defaultString(cfg.RaftDataDir, DefaultRaftDataDir)
}

func Default() AppConfig {
	return AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
		RaftDataDir:         DefaultRaftDataDir,
	}
}
