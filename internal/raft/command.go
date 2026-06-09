package raftstore

import (
	"encoding/json"

	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
)

type CommandType string

const (
	CommandReplaceConfig        CommandType = "replace_config"
	CommandSetClusterVIP        CommandType = "set_cluster_vip"
	CommandClearClusterVIP      CommandType = "clear_cluster_vip"
	CommandSetClusterRaftTiming CommandType = "set_cluster_raft_timing"
)

type Command struct {
	Type       CommandType                      `json:"type"`
	Config     spec.Config                      `json:"config,omitempty"`
	VIP        *control.ClusterVIPConfig        `json:"vip,omitempty"`
	RaftTiming *control.ClusterRaftTimingConfig `json:"raft_timing,omitempty"`
}

type ApplyResponse struct {
	Error            string                 `json:"error,omitempty"`
	StatusCode       int                    `json:"status_code,omitempty"`
	Code             string                 `json:"code,omitempty"`
	ValidationErrors []spec.ValidationError `json:"validation_errors,omitempty"`
}

func EncodeCommand(cmd Command) ([]byte, error) {
	return json.Marshal(cmd)
}

func DecodeCommand(data []byte) (Command, error) {
	var cmd Command
	err := json.Unmarshal(data, &cmd)
	return cmd, err
}
