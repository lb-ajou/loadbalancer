package runtime

import "testing"

func TestConfigActiveWithAddress(t *testing.T) {
	cfg := Config{Address: "10.10.0.100/24"}

	if !cfg.Active() {
		t.Fatal("Active() = false, want true when address is present")
	}
}
