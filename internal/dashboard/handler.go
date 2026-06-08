package dashboard

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"reverseproxy-poc/internal/admin"
	"reverseproxy-poc/internal/runtime"
	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/state"
)

//go:embed static/index.html
var dashboardHTML []byte

//go:embed static/cluster-lifecycle.html
var clusterLifecycleHTML []byte

type RaftJoiner interface {
	JoinRaft(ctx context.Context, nodeID, raftAddress string) error
}

type ClusterStatusProvider interface {
	ClusterStatus(ctx context.Context) ClusterView
}

type ClusterLifecycle interface {
	BootstrapCluster(ctx context.Context, request ClusterBootstrapRequest) error
	JoinCluster(ctx context.Context, request NodeJoinClusterRequest) error
	ClusterLifecycleStatus(ctx context.Context) NodeClusterStatusView
}

type VIPStatusProvider interface {
	VIPStatus() VIPStatusView
}

type ClusterBootstrapRequest struct {
	NodeID            string                    `json:"node_id"`
	RaftBindAddr      string                    `json:"raft_bind_addr,omitempty"`
	RaftAdvertiseAddr string                    `json:"raft_advertise_addr"`
	RaftTiming        *ClusterRaftTimingRequest `json:"raft_timing,omitempty"`
	VIPInterface      string                    `json:"vip_interface,omitempty"`
	VIP               *ClusterVIPRequest        `json:"vip,omitempty"`
}

type NodeJoinClusterRequest struct {
	NodeID            string   `json:"node_id"`
	RaftBindAddr      string   `json:"raft_bind_addr,omitempty"`
	RaftAdvertiseAddr string   `json:"raft_advertise_addr"`
	VIPInterface      string   `json:"vip_interface,omitempty"`
	Peers             []string `json:"peers"`
}

type ClusterVIPRequest struct {
	Address           string `json:"address"`
	GARPCount         int    `json:"garp_count,omitempty"`
	GARPInterval      string `json:"garp_interval,omitempty"`
	AcquireDelay      string `json:"acquire_delay,omitempty"`
	ReleaseOnShutdown bool   `json:"release_on_shutdown,omitempty"`
}

type ClusterRaftTimingRequest struct {
	HeartbeatTimeout   string `json:"heartbeat_timeout,omitempty"`
	ElectionTimeout    string `json:"election_timeout,omitempty"`
	LeaderLeaseTimeout string `json:"leader_lease_timeout,omitempty"`
	CommitTimeout      string `json:"commit_timeout,omitempty"`
}

type NodeClusterStatusView struct {
	State             string `json:"state"`
	NodeID            string `json:"node_id,omitempty"`
	RaftAdvertiseAddr string `json:"raft_advertise_addr,omitempty"`
	RaftDataDir       string `json:"raft_data_dir,omitempty"`
	LastError         string `json:"last_error,omitempty"`
}

type raftJoinRequest struct {
	NodeID      string `json:"node_id"`
	RaftAddress string `json:"raft_address"`
}

func NewHandler(state *runtime.State, service admin.Service) http.Handler {
	return NewHandlerWithRaft(state, service, nil)
}

func NewHandlerWithRaft(state *runtime.State, service admin.Service, joiner RaftJoiner) http.Handler {
	return NewHandlerWithProviders(state, service, joiner, nil, nil, nil)
}

func NewHandlerWithProviders(
	state *runtime.State,
	service admin.Service,
	joiner RaftJoiner,
	clusterProvider ClusterStatusProvider,
	vipProvider VIPStatusProvider,
	lifecycle ClusterLifecycle,
) http.Handler {
	if service == nil {
		panic("dashboard admin service is required")
	}

	mux := http.NewServeMux()
	if joiner != nil {
		registerRaftAPI(mux, joiner)
	}
	if lifecycle != nil {
		registerClusterLifecycleAPI(mux, lifecycle)
	}
	registerConfigAPI(mux, service)
	registerRuntimeAPI(mux, state, clusterProvider, vipProvider)
	registerClusterLifecyclePage(mux)
	registerSPA(mux)

	return mux
}

func registerRaftAPI(mux *http.ServeMux, joiner RaftJoiner) {
	mux.HandleFunc("/api/cluster/join", raftJoinHandler(joiner))
}

func registerClusterLifecycleAPI(mux *http.ServeMux, lifecycle ClusterLifecycle) {
	mux.HandleFunc("/api/node/cluster-status", nodeClusterStatusHandler(lifecycle))
	mux.HandleFunc("/api/cluster/bootstrap", clusterBootstrapHandler(lifecycle))
	mux.HandleFunc("/api/node/join-cluster", nodeJoinClusterHandler(lifecycle))
}

func nodeClusterStatusHandler(lifecycle ClusterLifecycle) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}
		writeJSON(w, lifecycle.ClusterLifecycleStatus(r.Context()))
	}
}

func clusterBootstrapHandler(lifecycle ClusterLifecycle) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}
		var request ClusterBootstrapRequest
		if err := decodeJSONBody(r, &request); err != nil {
			writeAPIError(w, err)
			return
		}
		if err := validateClusterBootstrapRequest(request); err != nil {
			writeAPIError(w, err)
			return
		}
		if err := lifecycle.BootstrapCluster(r.Context(), request); err != nil {
			writeRaftJoinError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func nodeJoinClusterHandler(lifecycle ClusterLifecycle) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}
		var request NodeJoinClusterRequest
		if err := decodeJSONBody(r, &request); err != nil {
			writeAPIError(w, err)
			return
		}
		if err := validateNodeJoinClusterRequest(request); err != nil {
			writeAPIError(w, err)
			return
		}
		if err := lifecycle.JoinCluster(r.Context(), request); err != nil {
			writeRaftJoinError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func validateClusterBootstrapRequest(request ClusterBootstrapRequest) error {
	if request.NodeID == "" || request.RaftAdvertiseAddr == "" {
		return invalidLifecycleRequest("node_id and raft_advertise_addr are required")
	}
	if err := validateRaftNodeFields(request.NodeID, request.RaftAdvertiseAddr); err != nil {
		return err
	}
	if err := validateOptionalClusterRaftTiming(request.RaftTiming); err != nil {
		return err
	}
	if request.VIP != nil && request.VIPInterface == "" {
		return invalidLifecycleRequest("vip_interface is required when vip is provided")
	}
	return validateOptionalClusterVIP(request.VIP)
}

func validateOptionalClusterRaftTiming(request *ClusterRaftTimingRequest) error {
	if request == nil {
		return nil
	}
	return stateErrorToAPIError(state.ValidateClusterRaftTiming(state.ClusterRaftTimingConfig{
		HeartbeatTimeout:   request.HeartbeatTimeout,
		ElectionTimeout:    request.ElectionTimeout,
		LeaderLeaseTimeout: request.LeaderLeaseTimeout,
		CommitTimeout:      request.CommitTimeout,
	}))
}

func validateOptionalClusterVIP(request *ClusterVIPRequest) error {
	if request == nil {
		return nil
	}
	return stateErrorToAPIError(state.ValidateClusterVIP(state.ClusterVIPConfig{
		Address:           request.Address,
		GARPCount:         request.GARPCount,
		GARPInterval:      request.GARPInterval,
		AcquireDelay:      request.AcquireDelay,
		ReleaseOnShutdown: request.ReleaseOnShutdown,
	}))
}

func validateNodeJoinClusterRequest(request NodeJoinClusterRequest) error {
	if request.NodeID == "" || request.RaftAdvertiseAddr == "" || len(request.Peers) == 0 {
		return invalidLifecycleRequest("node_id, raft_advertise_addr, and peers are required")
	}
	return validateRaftNodeFields(request.NodeID, request.RaftAdvertiseAddr)
}

func validateRaftNodeFields(nodeID, raftAddress string) error {
	if err := state.ValidateIdentifier(nodeID, "node_id"); err != nil {
		return stateErrorToAPIError(err)
	}
	if _, err := net.ResolveTCPAddr("tcp", raftAddress); err != nil {
		return &admin.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_raft_address",
			Message:    "raft_advertise_addr must be a host:port TCP address",
			Err:        err,
		}
	}
	return nil
}

func invalidLifecycleRequest(message string) error {
	return &admin.APIError{StatusCode: http.StatusBadRequest, Code: "invalid_request", Message: message}
}

func raftJoinHandler(joiner RaftJoiner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, newMethodNotAllowedError())
			return
		}

		var request raftJoinRequest
		if err := decodeJSONBody(r, &request); err != nil {
			writeAPIError(w, err)
			return
		}
		if err := validateRaftJoinRequest(request); err != nil {
			writeAPIError(w, err)
			return
		}

		if err := joiner.JoinRaft(r.Context(), request.NodeID, request.RaftAddress); err != nil {
			writeRaftJoinError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func validateRaftJoinRequest(request raftJoinRequest) error {
	if request.NodeID == "" || request.RaftAddress == "" {
		return &admin.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_request",
			Message:    "node_id and raft_address are required",
		}
	}
	if err := state.ValidateIdentifier(request.NodeID, "node_id"); err != nil {
		return stateErrorToAPIError(err)
	}
	if _, err := net.ResolveTCPAddr("tcp", request.RaftAddress); err != nil {
		return &admin.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "invalid_raft_address",
			Message:    "raft_address must be a host:port TCP address",
			Err:        err,
		}
	}
	return nil
}

func stateErrorToAPIError(err error) error {
	var stateErr *state.StateError
	if errors.As(err, &stateErr) {
		return &admin.APIError{
			StatusCode: stateErr.StatusCode,
			Code:       stateErr.Code,
			Message:    stateErr.Message,
			Err:        stateErr.Err,
		}
	}
	return err
}

func writeRaftJoinError(w http.ResponseWriter, err error) {
	var stateErr *state.StateError
	if errors.As(err, &stateErr) {
		writeAPIError(w, &admin.APIError{
			StatusCode:    stateErr.StatusCode,
			Code:          stateErr.Code,
			Message:       stateErr.Message,
			LeaderAddress: stateErr.LeaderAddress,
			Err:           stateErr.Err,
		})
		return
	}
	writeAPIError(w, err)
}

func registerSPA(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(dashboardHTML))
	})
}

func registerClusterLifecyclePage(mux *http.ServeMux) {
	mux.HandleFunc("/cluster-lifecycle", clusterLifecyclePageHandler())
}

func clusterLifecyclePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "cluster-lifecycle.html", time.Time{}, bytes.NewReader(clusterLifecycleHTML))
	}
}

func newMethodNotAllowedError() *admin.APIError {
	return &admin.APIError{
		StatusCode: http.StatusMethodNotAllowed,
		Message:    "method not allowed",
	}
}

type errorResponse struct {
	Message       string                 `json:"message"`
	Code          string                 `json:"code,omitempty"`
	LeaderAddress string                 `json:"leader_address,omitempty"`
	Errors        []spec.ValidationError `json:"errors,omitempty"`
}

func writeJSON(w http.ResponseWriter, value interface{}) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, statusCode int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, err error) {
	var adminErr *admin.APIError
	if errors.As(err, &adminErr) {
		response := errorResponse{
			Message:       adminErr.Message,
			Code:          adminErr.Code,
			LeaderAddress: adminErr.LeaderAddress,
			Errors:        adminErr.ValidationErrors,
		}

		var stateErr *state.StateError
		if errors.As(adminErr.Err, &stateErr) {
			if response.Code == "" {
				response.Code = stateErr.Code
			}
			if response.LeaderAddress == "" {
				response.LeaderAddress = stateErr.LeaderAddress
			}
		}

		writeJSONStatus(w, adminErr.StatusCode, response)
		return
	}

	writeJSONStatus(w, http.StatusInternalServerError, errorResponse{Message: "internal server error"})
}
