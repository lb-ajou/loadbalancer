package raftstore

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/boot"
	"reverseproxy-poc/internal/config"
	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
)

type FSM struct {
	mu      sync.RWMutex
	state   control.DesiredState
	appCfg  boot.AppConfig
	onApply func(control.DesiredState)
}

func NewFSM() *FSM {
	return NewFSMWithConfig(boot.AppConfig{}, nil)
}

func NewFSMWithConfig(appCfg boot.AppConfig, onApply func(control.DesiredState)) *FSM {
	return &FSM{
		state: control.DesiredState{
			ProxyConfig: spec.Config{
				Routes:        []spec.RouteConfig{},
				UpstreamPools: map[string]spec.UpstreamPool{},
			},
			AppliedAt: time.Now(),
		},
		appCfg:  appCfg,
		onApply: onApply,
	}
}

func (f *FSM) Apply(log *raft.Log) any {
	cmd, err := DecodeCommand(log.Data)
	if err != nil {
		return applyError(invalidRequestError(err.Error()))
	}

	f.mu.RLock()
	next := cloneDesiredState(f.state)
	f.mu.RUnlock()

	if err := f.applyCommand(&next, cmd); err != nil {
		return applyError(err)
	}
	if errs := validateDesiredState(next); len(errs) > 0 {
		return applyError(validationError(spec.ValidationErrors(errs)))
	}
	if _, err := control.ProjectSnapshot(f.appCfg, config.RaftConfig{}, config.VIPConfig{}, next); err != nil {
		return applyError(validationError(err))
	}

	next.Version = log.Index
	next.AppliedAt = time.Now()

	f.mu.Lock()
	f.state = cloneDesiredState(next)
	f.mu.Unlock()

	if f.onApply != nil {
		f.onApply(cloneDesiredState(next))
	}

	return ApplyResponse{}
}

func (f *FSM) DesiredState() control.DesiredState {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return cloneDesiredState(f.state)
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	return newFSMSnapshot(f.DesiredState()), nil
}

func (f *FSM) Restore(reader io.ReadCloser) error {
	defer func() {
		_ = reader.Close()
	}()

	state, err := decodeSnapshot(reader)
	if err != nil {
		return err
	}

	f.mu.Lock()
	f.state = cloneDesiredState(state)
	f.mu.Unlock()

	if f.onApply != nil {
		f.onApply(cloneDesiredState(state))
	}

	return nil
}

func (f *FSM) applyCommand(state *control.DesiredState, cmd Command) error {
	switch cmd.Type {
	case CommandReplaceConfig:
		if errs := cmd.Config.Validate(); len(errs) > 0 {
			return validationError(spec.ValidationErrors(errs))
		}
		state.ProxyConfig = cloneConfig(cmd.Config)
	case CommandSetClusterVIP:
		if cmd.VIP == nil {
			return invalidRequestError("vip is required")
		}
		if err := control.ValidateClusterVIP(*cmd.VIP); err != nil {
			return err
		}
		vip := control.NormalizeClusterVIP(*cmd.VIP)
		state.VIP = &vip
	case CommandClearClusterVIP:
		state.VIP = nil
	case CommandSetClusterRaftTiming:
		if cmd.RaftTiming == nil {
			return invalidRequestError("raft timing is required")
		}
		if err := control.ValidateClusterRaftTiming(*cmd.RaftTiming); err != nil {
			return err
		}
		timing := *cmd.RaftTiming
		state.RaftTiming = &timing
	default:
		return invalidRequestError(fmt.Sprintf("unknown command type %q", cmd.Type))
	}

	return nil
}

func applyError(err error) ApplyResponse {
	var stateErr *control.StateError
	if errors.As(err, &stateErr) {
		resp := ApplyResponse{
			Error:      stateErr.Message,
			StatusCode: stateErr.StatusCode,
			Code:       stateErr.Code,
		}
		var validationErrs spec.ValidationErrors
		if errors.As(stateErr.Err, &validationErrs) {
			resp.ValidationErrors = []spec.ValidationError(validationErrs)
		}
		return resp
	}
	return ApplyResponse{
		Error:      err.Error(),
		StatusCode: http.StatusUnprocessableEntity,
		Code:       "validation_failed",
	}
}

func invalidRequestError(message string) *control.StateError {
	return &control.StateError{
		StatusCode: http.StatusBadRequest,
		Code:       "invalid_request",
		Message:    message,
	}
}

func validationError(err error) *control.StateError {
	return &control.StateError{
		StatusCode: http.StatusUnprocessableEntity,
		Code:       "validation_failed",
		Message:    "validation failed",
		Err:        err,
	}
}

func validateDesiredState(state control.DesiredState) []spec.ValidationError {
	return state.ProxyConfig.Validate()
}
