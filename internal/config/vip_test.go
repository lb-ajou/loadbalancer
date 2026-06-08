package config

import "testing"

func TestVIPConfigActiveWithAddress(t *testing.T) {
	cfg := VIPConfig{Address: "10.10.0.100/24"}

	if !cfg.Active() {
		t.Fatal("Active() = false, want true when address is present")
	}
}
