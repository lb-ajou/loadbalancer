package boot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if got, want := cfg.ProxyListenAddr, ":8080"; got != want {
		t.Fatalf("ProxyListenAddr = %q, want %q", got, want)
	}
	if got, want := cfg.DashboardListenAddr, ":9090"; got != want {
		t.Fatalf("DashboardListenAddr = %q, want %q", got, want)
	}
}

func TestDefaultSetsRaftDataDir(t *testing.T) {
	cfg := Default()
	if cfg.RaftDataDir != DefaultRaftDataDir {
		t.Fatalf("RaftDataDir = %q, want %q", cfg.RaftDataDir, DefaultRaftDataDir)
	}
}

func TestNormalize_AppliesRaftDataDirDefault(t *testing.T) {
	cfg, err := Normalize(AppConfig{})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.RaftDataDir != DefaultRaftDataDir {
		t.Fatalf("RaftDataDir = %q, want %q", cfg.RaftDataDir, DefaultRaftDataDir)
	}
}

func TestNormalize_PreservesExplicitRaftDataDir(t *testing.T) {
	cfg, err := Normalize(AppConfig{
		ProxyListenAddr:     ":8080",
		DashboardListenAddr: ":9090",
		RaftDataDir:         "data/node-2",
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got, want := cfg.RaftDataDir, "data/node-2"; got != want {
		t.Fatalf("RaftDataDir = %q, want %q", got, want)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	writeConfigFile(t, path, `{"proxyListenAddr":":18080"}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.ProxyListenAddr, ":18080"; got != want {
		t.Fatalf("ProxyListenAddr = %q, want %q", got, want)
	}
	if got, want := cfg.DashboardListenAddr, ":9090"; got != want {
		t.Fatalf("DashboardListenAddr = %q, want %q", got, want)
	}
}

func TestLoad_RootAppConfigIsSingleNodeRaft(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "app.json"))
	if err != nil {
		t.Fatalf("Load(root app config) error = %v", err)
	}
	if cfg.RaftDataDir == "" {
		t.Fatal("RaftDataDir is empty")
	}
}

func TestLoad_IgnoresStaticRaftRuntimeFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	writeConfigFile(t, path, `{
	  "raftNodeId": "node-file",
	  "raftBindAddr": "127.0.0.1:7101",
	  "raftAdvertiseAddr": "127.0.0.1:7101",
	  "raftHeartbeatTimeout": "3s",
	  "raftElectionTimeout": "5s",
	  "raftLeaderLeaseTimeout": "2s",
	  "raftCommitTimeout": "250ms"
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := cfg.RaftDataDir, DefaultRaftDataDir; got != want {
		t.Fatalf("RaftDataDir = %q, want %q", got, want)
	}
}

func TestValidate_AllowsCleanNodeWithoutRaftIdentity(t *testing.T) {
	cfg := Default()

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RaftModeAcceptsRequiredNodeSettings(t *testing.T) {
	cfg := Default()

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func validRaftConfig() AppConfig {
	cfg := Default()
	cfg.RaftDataDir = "data/node-1"
	return cfg
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
