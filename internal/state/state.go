package state

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"reverseproxy-poc/internal/spec"
)

const (
	DefaultVIPGARPCount    = 5
	DefaultVIPGARPInterval = "200ms"
	DefaultVIPAcquireDelay = "500ms"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type DesiredState struct {
	ProxyConfig spec.Config
	VIP         *ClusterVIPConfig
	RaftTiming  *ClusterRaftTimingConfig
	Version     uint64
	AppliedAt   time.Time
}

type ClusterVIPConfig struct {
	Address           string `json:"address,omitempty"`
	GARPCount         int    `json:"garpCount,omitempty"`
	GARPInterval      string `json:"garpInterval,omitempty"`
	AcquireDelay      string `json:"acquireDelay,omitempty"`
	ReleaseOnShutdown bool   `json:"releaseOnShutdown,omitempty"`
}

type ClusterRaftTimingConfig struct {
	HeartbeatTimeout   string `json:"heartbeatTimeout,omitempty"`
	ElectionTimeout    string `json:"electionTimeout,omitempty"`
	LeaderLeaseTimeout string `json:"leaderLeaseTimeout,omitempty"`
	CommitTimeout      string `json:"commitTimeout,omitempty"`
}

type AppliedProxyConfig struct {
	Routes        []spec.RouteConfig
	UpstreamPools map[string]spec.UpstreamPool
	AppliedAt     time.Time
}

type StateError struct {
	StatusCode    int
	Code          string
	Message       string
	LeaderAddress string
	Err           error
}

func (e *StateError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *StateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewNotLeaderError(leader string) *StateError {
	return &StateError{
		StatusCode:    http.StatusConflict,
		Code:          "not_raft_leader",
		Message:       "configuration writes must be sent to the raft leader",
		LeaderAddress: leader,
	}
}

func NewClusterNotConfiguredError() *StateError {
	return &StateError{
		StatusCode: http.StatusConflict,
		Code:       "cluster_not_configured",
		Message:    "cluster must be bootstrapped or joined before configuration writes",
	}
}

func NewClusterAlreadyConfiguredError() *StateError {
	return &StateError{
		StatusCode: http.StatusConflict,
		Code:       "cluster_already_configured",
		Message:    "node already has raft state or a running raft node",
	}
}

func IsNotLeader(err error) bool {
	var stateErr *StateError
	return errors.As(err, &stateErr) && stateErr.Code == "not_raft_leader"
}

func ValidateIdentifier(value, field string) error {
	if identifierPattern.MatchString(value) {
		return nil
	}
	codeField := strings.ReplaceAll(field, " ", "_")
	return &StateError{
		StatusCode: http.StatusBadRequest,
		Code:       "invalid_" + codeField,
		Message:    field + " must contain only letters, numbers, dot, underscore, or hyphen",
	}
}

func ValidateClusterVIP(vip ClusterVIPConfig) error {
	vip = NormalizeClusterVIP(vip)
	if err := validateClusterVIPAddress(vip.Address); err != nil {
		return err
	}
	if vip.GARPCount < 1 {
		return invalidVIPError("vip garp count must be at least 1")
	}
	return validateClusterVIPDurations(vip)
}

func NormalizeClusterVIP(vip ClusterVIPConfig) ClusterVIPConfig {
	if vip.GARPCount == 0 {
		vip.GARPCount = DefaultVIPGARPCount
	}
	if vip.GARPInterval == "" {
		vip.GARPInterval = DefaultVIPGARPInterval
	}
	if vip.AcquireDelay == "" {
		vip.AcquireDelay = DefaultVIPAcquireDelay
	}
	return vip
}

func ValidateClusterRaftTiming(timing ClusterRaftTimingConfig) error {
	heartbeat, err := parseRaftTimeout(timing.HeartbeatTimeout, "raft heartbeat timeout")
	if err != nil {
		return err
	}
	election, err := parseRaftTimeout(timing.ElectionTimeout, "raft election timeout")
	if err != nil {
		return err
	}
	return validateRaftTimingTail(timing, heartbeat, election)
}

func validateRaftTimingTail(timing ClusterRaftTimingConfig, heartbeat, election time.Duration) error {
	lease, err := parseRaftTimeout(timing.LeaderLeaseTimeout, "raft leader lease timeout")
	if err != nil {
		return err
	}
	if _, err := parseRaftTimeout(timing.CommitTimeout, "raft commit timeout"); err != nil {
		return err
	}
	return validateRaftTimingOrder(heartbeat, election, lease)
}

func parseRaftTimeout(value, name string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, invalidRaftTimingError(name + " must be a positive duration")
	}
	return timeout, nil
}

func validateRaftTimingOrder(heartbeat, election, lease time.Duration) error {
	if heartbeat > 0 && election > 0 && election < heartbeat {
		return invalidRaftTimingError("raft election timeout must be greater than or equal to heartbeat timeout")
	}
	if heartbeat > 0 && lease > heartbeat {
		return invalidRaftTimingError("raft leader lease timeout must be less than or equal to heartbeat timeout")
	}
	return nil
}

func validateClusterVIPAddress(address string) error {
	prefix, err := netip.ParsePrefix(address)
	if err != nil {
		return invalidVIPError("vip address must be CIDR")
	}
	if !prefix.Addr().Is4() {
		return invalidVIPError("vip address must be IPv4")
	}
	return nil
}

func validateClusterVIPDurations(vip ClusterVIPConfig) error {
	if _, err := time.ParseDuration(vip.GARPInterval); err != nil {
		return invalidVIPError("vip garp interval must be a duration")
	}
	if _, err := time.ParseDuration(vip.AcquireDelay); err != nil {
		return invalidVIPError("vip acquire delay must be a duration")
	}
	return nil
}

func invalidVIPError(message string) *StateError {
	return &StateError{
		StatusCode: http.StatusBadRequest,
		Code:       "invalid_vip",
		Message:    message,
	}
}

func invalidRaftTimingError(message string) *StateError {
	return &StateError{
		StatusCode: http.StatusBadRequest,
		Code:       "invalid_raft_timing",
		Message:    message,
	}
}
