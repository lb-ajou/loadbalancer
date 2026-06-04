package boot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type fileConfig struct {
	ProxyListenAddr     string `json:"proxyListenAddr"`
	DashboardListenAddr string `json:"dashboardListenAddr"`
	RaftDataDir         string `json:"raftDataDir,omitempty"`
}

func Load(path string) (AppConfig, error) {
	if path == "" {
		return AppConfig{}, errors.New("config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config: %w", err)
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("decode config: %w", err)
	}
	return normalizeLoadedConfig(cfg)
}

func normalizeLoadedConfig(cfg fileConfig) (AppConfig, error) {
	return Normalize(cfg.appConfig())
}

func (cfg fileConfig) appConfig() AppConfig {
	return AppConfig{
		ProxyListenAddr:     cfg.ProxyListenAddr,
		DashboardListenAddr: cfg.DashboardListenAddr,
		RaftDataDir:         cfg.RaftDataDir,
	}
}
