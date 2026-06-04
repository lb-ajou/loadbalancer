package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInfoHandlerIncludesLBNodeHeader(t *testing.T) {
	cfg := serverConfig{Server: "backend-a", Scenario: "openstack-vip", Port: "18081", Version: "v1", HealthStatus: 200}
	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	req.Header.Set("X-AjouLB-LB-Node", "node-3")
	rec := httptest.NewRecorder()

	infoHandler(cfg, newInfoResponse(cfg))(rec, req)

	var got infoResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if got.LBNode != "node-3" {
		t.Fatalf("LBNode = %q, want node-3", got.LBNode)
	}
}
