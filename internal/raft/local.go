package raftstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const localNodeConfigFile = "node.json"

type LocalNodeConfig struct {
	NodeID        string `json:"node_id"`
	BindAddr      string `json:"bind_addr"`
	AdvertiseAddr string `json:"advertise_addr"`
}

func SaveLocalNodeConfig(dataDir string, cfg LocalNodeConfig) error {
	if dataDir == "" {
		return fmt.Errorf("raft data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create raft data dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode raft local node config: %w", err)
	}
	return writeLocalNodeConfig(dataDir, data)
}

func LoadLocalNodeConfig(dataDir string) (LocalNodeConfig, bool, error) {
	data, err := os.ReadFile(localNodeConfigPath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return LocalNodeConfig{}, false, nil
	}
	if err != nil {
		return LocalNodeConfig{}, false, fmt.Errorf("read raft local node config: %w", err)
	}
	var cfg LocalNodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return LocalNodeConfig{}, false, fmt.Errorf("decode raft local node config: %w", err)
	}
	return cfg, true, nil
}

func writeLocalNodeConfig(dataDir string, data []byte) error {
	path := localNodeConfigPath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write raft local node config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace raft local node config: %w", err)
	}
	return nil
}

func localNodeConfigPath(dataDir string) string {
	return filepath.Join(dataDir, localNodeConfigFile)
}
