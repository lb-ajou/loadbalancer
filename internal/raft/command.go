package raftstore

import (
	"encoding/json"

	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
)

type CommandType string

const (
	CommandCreateNamespace        CommandType = "create_namespace"
	CommandDeleteNamespace        CommandType = "delete_namespace"
	CommandReplaceNamespaceConfig CommandType = "replace_namespace_config"
	CommandCreateRoute            CommandType = "create_route"
	CommandUpdateRoute            CommandType = "update_route"
	CommandDeleteRoute            CommandType = "delete_route"
	CommandCreateUpstreamPool     CommandType = "create_upstream_pool"
	CommandUpdateUpstreamPool     CommandType = "update_upstream_pool"
	CommandDeleteUpstreamPool     CommandType = "delete_upstream_pool"
	CommandSetClusterVIP          CommandType = "set_cluster_vip"
	CommandClearClusterVIP        CommandType = "clear_cluster_vip"
	CommandSetClusterRaftTiming   CommandType = "set_cluster_raft_timing"
)

type Command struct {
	Type       CommandType                      `json:"type"`
	Namespace  string                           `json:"namespace,omitempty"`
	RouteID    string                           `json:"route_id,omitempty"`
	PoolID     string                           `json:"pool_id,omitempty"`
	Config     spec.Config                      `json:"config,omitempty"`
	Route      spec.RouteConfig                 `json:"route,omitempty"`
	Pool       spec.UpstreamPool                `json:"pool,omitempty"`
	VIP        *control.ClusterVIPConfig        `json:"vip,omitempty"`
	RaftTiming *control.ClusterRaftTimingConfig `json:"raft_timing,omitempty"`
}

type ApplyResponse struct {
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
}

func EncodeCommand(cmd Command) ([]byte, error) {
	return json.Marshal(cmd)
}

func DecodeCommand(data []byte) (Command, error) {
	var cmd Command
	err := json.Unmarshal(data, &cmd)
	return cmd, err
}
