package raftstore

import (
	"path/filepath"
	"testing"
)

func TestLocalNodeConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := LocalNodeConfig{
		NodeID:        "node-1",
		BindAddr:      "0.0.0.0:7001",
		AdvertiseAddr: "10.0.0.11:7001",
	}

	if err := SaveLocalNodeConfig(dir, want); err != nil {
		t.Fatalf("SaveLocalNodeConfig() error = %v", err)
	}
	got, ok, err := LoadLocalNodeConfig(dir)
	if err != nil {
		t.Fatalf("LoadLocalNodeConfig() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadLocalNodeConfig() ok = false, want true")
	}
	if got != want {
		t.Fatalf("LocalNodeConfig = %+v, want %+v", got, want)
	}
}

func TestLoadLocalNodeConfigMissing(t *testing.T) {
	_, ok, err := LoadLocalNodeConfig(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("LoadLocalNodeConfig() error = %v", err)
	}
	if ok {
		t.Fatal("LoadLocalNodeConfig() ok = true, want false")
	}
}
