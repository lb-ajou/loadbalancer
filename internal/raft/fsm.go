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
	"reverseproxy-poc/internal/raftstate"
	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
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
			Namespaces: map[string]spec.Config{},
			AppliedAt:  time.Now(),
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
	if _, err := control.ProjectSnapshot(f.appCfg, raftstate.Config{}, vipruntime.Config{}, next); err != nil {
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
	if cmd.Namespace != "" {
		if err := control.ValidateNamespaceName(cmd.Namespace); err != nil {
			return err
		}
	}

	switch cmd.Type {
	case CommandCreateNamespace:
		if _, exists := state.Namespaces[cmd.Namespace]; exists {
			return conflictError(fmt.Sprintf("namespace %q already exists", cmd.Namespace))
		}
		state.Namespaces[cmd.Namespace] = ensureNamespace(state.Namespaces, cmd.Namespace)
	case CommandDeleteNamespace:
		if _, exists := state.Namespaces[cmd.Namespace]; !exists {
			return notFoundError(fmt.Sprintf("namespace %q was not found", cmd.Namespace))
		}
		delete(state.Namespaces, cmd.Namespace)
	case CommandReplaceNamespaceConfig:
		state.Namespaces[cmd.Namespace] = cloneConfig(cmd.Config)
	case CommandCreateUpstreamPool:
		cfg := ensureNamespace(state.Namespaces, cmd.Namespace)
		if _, exists := cfg.UpstreamPools[cmd.PoolID]; exists {
			return conflictError(fmt.Sprintf("upstream pool %q already exists", cmd.PoolID))
		}
		cfg.UpstreamPools[cmd.PoolID] = cloneUpstreamPool(cmd.Pool)
		state.Namespaces[cmd.Namespace] = cfg
	case CommandUpdateUpstreamPool:
		cfg, exists := state.Namespaces[cmd.Namespace]
		if !exists {
			return notFoundError(fmt.Sprintf("namespace %q was not found", cmd.Namespace))
		}
		cfg = cloneConfig(cfg)
		if _, exists := cfg.UpstreamPools[cmd.PoolID]; !exists {
			return notFoundError(fmt.Sprintf("upstream pool %q was not found", cmd.PoolID))
		}
		cfg.UpstreamPools[cmd.PoolID] = cloneUpstreamPool(cmd.Pool)
		state.Namespaces[cmd.Namespace] = cfg
	case CommandDeleteUpstreamPool:
		cfg, exists := state.Namespaces[cmd.Namespace]
		if !exists {
			return notFoundError(fmt.Sprintf("namespace %q was not found", cmd.Namespace))
		}
		cfg = cloneConfig(cfg)
		if _, exists := cfg.UpstreamPools[cmd.PoolID]; !exists {
			return notFoundError(fmt.Sprintf("upstream pool %q was not found", cmd.PoolID))
		}
		for _, route := range cfg.Routes {
			if route.UpstreamPool == cmd.PoolID {
				return conflictError(fmt.Sprintf("upstream pool %q is still referenced by route %q", cmd.PoolID, route.ID))
			}
		}
		delete(cfg.UpstreamPools, cmd.PoolID)
		state.Namespaces[cmd.Namespace] = cfg
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
	case CommandCreateRoute:
		cfg := ensureNamespace(state.Namespaces, cmd.Namespace)
		for _, route := range cfg.Routes {
			if route.ID == cmd.Route.ID {
				return conflictError(fmt.Sprintf("route %q already exists", cmd.Route.ID))
			}
		}
		cfg.Routes = append(cfg.Routes, cloneRoute(cmd.Route))
		state.Namespaces[cmd.Namespace] = cfg
	case CommandUpdateRoute:
		if cmd.Route.ID != cmd.RouteID {
			return invalidRequestError("route id in body must match request path")
		}
		cfg, exists := state.Namespaces[cmd.Namespace]
		if !exists {
			return notFoundError(fmt.Sprintf("namespace %q was not found", cmd.Namespace))
		}
		cfg = cloneConfig(cfg)
		for index, route := range cfg.Routes {
			if route.ID == cmd.RouteID {
				cfg.Routes[index] = cloneRoute(cmd.Route)
				state.Namespaces[cmd.Namespace] = cfg
				return nil
			}
		}
		return notFoundError(fmt.Sprintf("route %q was not found", cmd.RouteID))
	case CommandDeleteRoute:
		cfg, exists := state.Namespaces[cmd.Namespace]
		if !exists {
			return notFoundError(fmt.Sprintf("namespace %q was not found", cmd.Namespace))
		}
		cfg = cloneConfig(cfg)
		for index, route := range cfg.Routes {
			if route.ID == cmd.RouteID {
				cfg.Routes = append(cfg.Routes[:index], cfg.Routes[index+1:]...)
				state.Namespaces[cmd.Namespace] = cfg
				return nil
			}
		}
		return notFoundError(fmt.Sprintf("route %q was not found", cmd.RouteID))
	default:
		return invalidRequestError(fmt.Sprintf("unknown command type %q", cmd.Type))
	}

	return nil
}

func applyError(err error) ApplyResponse {
	var stateErr *control.StateError
	if errors.As(err, &stateErr) {
		return ApplyResponse{
			Error:      stateErr.Message,
			StatusCode: stateErr.StatusCode,
			Code:       stateErr.Code,
		}
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

func conflictError(message string) *control.StateError {
	return &control.StateError{
		StatusCode: http.StatusConflict,
		Code:       "resource_conflict",
		Message:    message,
	}
}

func notFoundError(message string) *control.StateError {
	return &control.StateError{
		StatusCode: http.StatusNotFound,
		Code:       "resource_not_found",
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

func ensureNamespace(namespaces map[string]spec.Config, namespace string) spec.Config {
	cfg, exists := namespaces[namespace]
	if !exists {
		cfg = spec.Config{}
	}
	cfg = cloneConfig(cfg)
	if cfg.Routes == nil {
		cfg.Routes = []spec.RouteConfig{}
	}
	if cfg.UpstreamPools == nil {
		cfg.UpstreamPools = map[string]spec.UpstreamPool{}
	}
	namespaces[namespace] = cfg
	return cfg
}

func validateDesiredState(state control.DesiredState) []spec.ValidationError {
	var errs []spec.ValidationError
	for namespace, cfg := range state.Namespaces {
		for _, err := range cfg.Validate() {
			err.Field = "namespaces." + namespace + "." + err.Field
			errs = append(errs, err)
		}
	}
	return errs
}
