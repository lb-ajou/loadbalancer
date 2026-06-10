package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"loadbalancer/internal/spec"
	"loadbalancer/internal/state"
)

type Service interface {
	GetConfig(ctx context.Context) (ConfigView, error)
	ReplaceConfig(ctx context.Context, cfg spec.Config) (ConfigView, error)
}

type ConfigView struct {
	Routes        []spec.RouteConfig           `json:"routes"`
	UpstreamPools map[string]spec.UpstreamPool `json:"upstream_pools"`
	AppliedAt     time.Time                    `json:"applied_at,omitempty"`
}

type APIError struct {
	StatusCode       int                    `json:"-"`
	Code             string                 `json:"code,omitempty"`
	Message          string                 `json:"message"`
	LeaderAddress    string                 `json:"leader_address,omitempty"`
	ValidationErrors []spec.ValidationError `json:"errors,omitempty"`
	Err              error                  `json:"-"`
}

func (e *APIError) Error() string {
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

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type service struct {
	store stateStore
}

type stateStore interface {
	GetConfig(ctx context.Context) (state.AppliedProxyConfig, error)
	ReplaceConfig(ctx context.Context, cfg spec.Config) (state.AppliedProxyConfig, error)
}

func NewWithConfigState(store stateStore) Service {
	return newService(store)
}

func newService(store stateStore) Service {
	return &service{store: store}
}

func (s *service) GetConfig(ctx context.Context) (ConfigView, error) {
	item, err := s.store.GetConfig(ctx)
	if err != nil {
		return ConfigView{}, toAPIError(err)
	}
	return configViewFromStore(item), nil
}

func (s *service) ReplaceConfig(ctx context.Context, cfg spec.Config) (ConfigView, error) {
	item, err := s.store.ReplaceConfig(ctx, cfg)
	if err != nil {
		return ConfigView{}, toAPIError(err)
	}
	return configViewFromStore(item), nil
}

func configViewFromStore(item state.AppliedProxyConfig) ConfigView {
	return ConfigView{
		Routes:        item.Routes,
		UpstreamPools: item.UpstreamPools,
		AppliedAt:     item.AppliedAt,
	}
}

func toAPIError(err error) error {
	if err == nil {
		return nil
	}

	var stateErr *state.StateError
	if errors.As(err, &stateErr) {
		apiErr := &APIError{
			StatusCode:    stateErr.StatusCode,
			Code:          stateErr.Code,
			Message:       stateErr.Message,
			LeaderAddress: stateErr.LeaderAddress,
			Err:           stateErr.Err,
		}

		var validationErrs spec.ValidationErrors
		if errors.As(stateErr.Err, &validationErrs) {
			apiErr.ValidationErrors = validationErrs
		}

		return apiErr
	}

	return err
}
