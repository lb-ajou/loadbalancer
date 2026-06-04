package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type infoResponse struct {
	Server       string `json:"server"`
	Scenario     string `json:"scenario"`
	Hostname     string `json:"hostname"`
	Port         string `json:"port"`
	Version      string `json:"version"`
	HealthStatus int    `json:"health_status"`
	LBNode       string `json:"lb_node,omitempty"`
}

func main() {
	cfg := loadServerConfig()
	info := newInfoResponse(cfg)
	log.Printf("test server %s listening on :%s", cfg.Server, cfg.Port)
	serve(cfg, info)
}

type serverConfig struct {
	Server       string
	Scenario     string
	Port         string
	Version      string
	HealthStatus int
	SleepMillis  int
}

func loadServerConfig() serverConfig {
	return serverConfig{
		Server:       envOrDefault("SERVER_NAME", "test-server"),
		Scenario:     envOrDefault("SCENARIO_NAME", "unknown"),
		Port:         envOrDefault("PORT", "8080"),
		Version:      envOrDefault("SERVER_VERSION", "v1"),
		HealthStatus: envIntOrDefault("HEALTH_STATUS", http.StatusOK),
		SleepMillis:  envIntOrDefault("SLEEP_MS", 0),
	}
}

func newInfoResponse(cfg serverConfig) infoResponse {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("hostname: %v", err)
	}
	return infoResponse{
		Server:       cfg.Server,
		Scenario:     cfg.Scenario,
		Hostname:     hostname,
		Port:         cfg.Port,
		Version:      cfg.Version,
		HealthStatus: cfg.HealthStatus,
	}
}

func serve(cfg serverConfig, info infoResponse) {
	mux := newMux(cfg, info)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func newMux(cfg serverConfig, info infoResponse) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(cfg))
	mux.HandleFunc("/api/info", infoHandler(cfg, info))
	return mux
}

func healthHandler(cfg serverConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, cfg.HealthStatus, map[string]interface{}{
			"ok":            cfg.HealthStatus >= 200 && cfg.HealthStatus < 300,
			"server":        cfg.Server,
			"scenario":      cfg.Scenario,
			"health_status": cfg.HealthStatus,
		})
	}
}

func infoHandler(cfg serverConfig, info infoResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sleepForMillis(cfg.SleepMillis)
		info.LBNode = r.Header.Get("X-AjouLB-LB-Node")
		writeJSON(w, http.StatusOK, info)
	}
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envIntOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func sleepForMillis(value int) {
	if value > 0 {
		time.Sleep(time.Duration(value) * time.Millisecond)
	}
}
