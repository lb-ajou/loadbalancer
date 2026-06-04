package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reverseproxy-poc/internal/spec"
	"reverseproxy-poc/internal/state"
)

const DefaultNamespace = state.DefaultNamespace

type Service interface {
	ListNamespaces(ctx context.Context) ([]NamespaceView, error)
	CreateNamespace(ctx context.Context, namespace string) (NamespaceView, error)
	DeleteNamespace(ctx context.Context, namespace string) error
	GetNamespaceConfig(ctx context.Context, namespace string) (NamespaceConfigView, error)
	ReplaceNamespaceConfig(ctx context.Context, namespace string, cfg spec.Config) (NamespaceConfigView, error)
	GetNamespaceRoutes(ctx context.Context, namespace string) ([]spec.RouteConfig, error)
	CreateRoute(ctx context.Context, namespace string, route spec.RouteConfig) (spec.RouteConfig, error)
	UpdateRoute(ctx context.Context, namespace, id string, route spec.RouteConfig) (spec.RouteConfig, error)
	DeleteRoute(ctx context.Context, namespace, id string) error
	GetNamespaceUpstreamPools(ctx context.Context, namespace string) (map[string]spec.UpstreamPool, error)
	CreateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error)
	UpdateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error)
	DeleteUpstreamPool(ctx context.Context, namespace, id string) error
}

type NamespaceConfigView struct {
	Namespace     string                       `json:"namespace"`
	Exists        bool                         `json:"exists"`
	Routes        []spec.RouteConfig           `json:"routes"`
	UpstreamPools map[string]spec.UpstreamPool `json:"upstream_pools"`
	AppliedAt     time.Time                    `json:"applied_at,omitempty"`
}

type NamespaceView struct {
	Namespace         string `json:"namespace"`
	Path              string `json:"path"`
	Exists            bool   `json:"exists"`
	RouteCount        int    `json:"route_count"`
	UpstreamPoolCount int    `json:"upstream_pool_count"`
}

type NamespaceListView struct {
	Items            []NamespaceView `json:"items"`
	DefaultNamespace string          `json:"default_namespace"`
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
	ListNamespaces(ctx context.Context) ([]state.NamespaceSummary, error)
	GetNamespaceConfig(ctx context.Context, namespace string) (state.NamespaceConfig, error)
	ReplaceNamespaceConfig(ctx context.Context, namespace string, cfg spec.Config) (state.NamespaceConfig, error)
	CreateNamespace(ctx context.Context, namespace string) (state.NamespaceSummary, error)
	DeleteNamespace(ctx context.Context, namespace string) error
	CreateRoute(ctx context.Context, namespace string, route spec.RouteConfig) (spec.RouteConfig, error)
	UpdateRoute(ctx context.Context, namespace, id string, route spec.RouteConfig) (spec.RouteConfig, error)
	DeleteRoute(ctx context.Context, namespace, id string) error
	CreateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error)
	UpdateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error)
	DeleteUpstreamPool(ctx context.Context, namespace, id string) error
}

func NewWithConfigState(store stateStore) Service {
	return newService(store)
}

func newService(store stateStore) Service {
	return &service{store: store}
}

func (s *service) ListNamespaces(ctx context.Context) ([]NamespaceView, error) {
	items, err := s.store.ListNamespaces(ctx)
	if err != nil {
		return nil, toAPIError(err)
	}

	views := make([]NamespaceView, 0, len(items))
	for _, item := range items {
		views = append(views, namespaceViewFromStore(item))
	}
	return views, nil
}

func (s *service) CreateNamespace(ctx context.Context, namespace string) (NamespaceView, error) {
	item, err := s.store.CreateNamespace(ctx, namespace)
	if err != nil {
		return NamespaceView{}, toAPIError(err)
	}
	return namespaceViewFromStore(item), nil
}

func (s *service) DeleteNamespace(ctx context.Context, namespace string) error {
	if err := s.store.DeleteNamespace(ctx, namespace); err != nil {
		return toAPIError(err)
	}
	return nil
}

func (s *service) GetNamespaceConfig(ctx context.Context, namespace string) (NamespaceConfigView, error) {
	item, err := s.store.GetNamespaceConfig(ctx, namespace)
	if err != nil {
		return NamespaceConfigView{}, toAPIError(err)
	}
	return namespaceConfigFromStore(item), nil
}

func (s *service) ReplaceNamespaceConfig(ctx context.Context, namespace string, cfg spec.Config) (NamespaceConfigView, error) {
	item, err := s.store.ReplaceNamespaceConfig(ctx, namespace, cfg)
	if err != nil {
		return NamespaceConfigView{}, toAPIError(err)
	}
	return namespaceConfigFromStore(item), nil
}

func (s *service) GetNamespaceRoutes(ctx context.Context, namespace string) ([]spec.RouteConfig, error) {
	view, err := s.GetNamespaceConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return view.Routes, nil
}

func (s *service) CreateRoute(ctx context.Context, namespace string, route spec.RouteConfig) (spec.RouteConfig, error) {
	item, err := s.store.CreateRoute(ctx, namespace, route)
	if err != nil {
		return spec.RouteConfig{}, toAPIError(err)
	}
	return item, nil
}

func (s *service) UpdateRoute(ctx context.Context, namespace, id string, route spec.RouteConfig) (spec.RouteConfig, error) {
	item, err := s.store.UpdateRoute(ctx, namespace, id, route)
	if err != nil {
		return spec.RouteConfig{}, toAPIError(err)
	}
	return item, nil
}

func (s *service) DeleteRoute(ctx context.Context, namespace, id string) error {
	if err := s.store.DeleteRoute(ctx, namespace, id); err != nil {
		return toAPIError(err)
	}
	return nil
}

func (s *service) GetNamespaceUpstreamPools(ctx context.Context, namespace string) (map[string]spec.UpstreamPool, error) {
	view, err := s.GetNamespaceConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return view.UpstreamPools, nil
}

func (s *service) CreateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error) {
	item, err := s.store.CreateUpstreamPool(ctx, namespace, id, pool)
	if err != nil {
		return spec.UpstreamPool{}, toAPIError(err)
	}
	return item, nil
}

func (s *service) UpdateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error) {
	item, err := s.store.UpdateUpstreamPool(ctx, namespace, id, pool)
	if err != nil {
		return spec.UpstreamPool{}, toAPIError(err)
	}
	return item, nil
}

func (s *service) DeleteUpstreamPool(ctx context.Context, namespace, id string) error {
	if err := s.store.DeleteUpstreamPool(ctx, namespace, id); err != nil {
		return toAPIError(err)
	}
	return nil
}

func namespaceViewFromStore(item state.NamespaceSummary) NamespaceView {
	return NamespaceView{
		Namespace:         item.Namespace,
		Path:              item.Path,
		Exists:            item.Exists,
		RouteCount:        item.RouteCount,
		UpstreamPoolCount: item.UpstreamPoolCount,
	}
}

func namespaceConfigFromStore(item state.NamespaceConfig) NamespaceConfigView {
	return NamespaceConfigView{
		Namespace:     item.Namespace,
		Exists:        item.Exists,
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
