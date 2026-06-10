package raftstore

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/hashicorp/raft"

	"loadbalancer/internal/spec"
	control "loadbalancer/internal/state"
)

type raftApplier interface {
	State() raft.RaftState
	Leader() raft.ServerAddress
	Apply(cmd []byte, timeout time.Duration) raft.ApplyFuture
}

type Store struct {
	raft    raftApplier
	fsm     *FSM
	timeout time.Duration
}

func NewStore(node raftApplier, fsm *FSM) *Store {
	return &Store{raft: node, fsm: fsm, timeout: 5 * time.Second}
}

func (s *Store) GetConfig(_ context.Context) (control.AppliedProxyConfig, error) {
	state := s.fsm.DesiredState()
	cfg := cloneConfig(state.ProxyConfig)
	return control.AppliedProxyConfig{
		Routes:        cfg.Routes,
		UpstreamPools: cfg.UpstreamPools,
		AppliedAt:     state.AppliedAt,
	}, nil
}

func (s *Store) ReplaceConfig(ctx context.Context, cfg spec.Config) (control.AppliedProxyConfig, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return control.AppliedProxyConfig{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandReplaceConfig, Config: cfg}); err != nil {
		return control.AppliedProxyConfig{}, err
	}
	return s.GetConfig(ctx)
}

func (s *Store) SetClusterVIP(ctx context.Context, vip control.ClusterVIPConfig) error {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return err
	}
	return s.apply(ctx, Command{Type: CommandSetClusterVIP, VIP: &vip})
}

func (s *Store) ClearClusterVIP(ctx context.Context) error {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return err
	}
	return s.apply(ctx, Command{Type: CommandClearClusterVIP})
}

func (s *Store) SetClusterRaftTiming(ctx context.Context, timing control.ClusterRaftTimingConfig) error {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return err
	}
	return s.apply(ctx, Command{Type: CommandSetClusterRaftTiming, RaftTiming: &timing})
}

func (s *Store) ensureLeaderWrite(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.raft.State() != raft.Leader {
		return control.NewNotLeaderError(string(s.raft.Leader()))
	}
	return nil
}

func (s *Store) apply(ctx context.Context, cmd Command) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.raft.State() != raft.Leader {
		return control.NewNotLeaderError(string(s.raft.Leader()))
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		return err
	}
	future := s.raft.Apply(data, s.timeout)
	if err := future.Error(); err != nil {
		if isRaftLeadershipError(err) {
			return control.NewNotLeaderError(string(s.raft.Leader()))
		}
		return err
	}
	if resp, ok := future.Response().(ApplyResponse); ok && resp.Error != "" {
		statusCode := resp.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusUnprocessableEntity
		}
		code := resp.Code
		if code == "" {
			code = "raft_apply_rejected"
		}
		stateErr := &control.StateError{
			StatusCode: statusCode,
			Code:       code,
			Message:    resp.Error,
		}
		if len(resp.ValidationErrors) > 0 {
			stateErr.Err = spec.ValidationErrors(resp.ValidationErrors)
		}
		return stateErr
	}
	return nil
}

func isRaftLeadershipError(err error) bool {
	return errors.Is(err, raft.ErrNotLeader) ||
		errors.Is(err, raft.ErrLeadershipLost) ||
		errors.Is(err, raft.ErrLeadershipTransferInProgress)
}
