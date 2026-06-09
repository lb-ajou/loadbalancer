package boot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return AppConfig{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return AppConfig{}, errors.New("decode config: config must contain a single JSON object")
	}
	return normalizeFileConfig(cfg)
}

func normalizeFileConfig(cfg fileConfig) (AppConfig, error) {
	return Normalize(cfg.appConfig())
}

func (cfg fileConfig) appConfig() AppConfig {
	return AppConfig{
		ProxyListenAddr:     cfg.ProxyListenAddr,
		DashboardListenAddr: cfg.DashboardListenAddr,
		RaftDataDir:         cfg.RaftDataDir,
	}
}
