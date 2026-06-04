package raftstore

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/spec"
	control "reverseproxy-poc/internal/state"
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

func (s *Store) DesiredState(_ context.Context) (control.DesiredState, error) {
	return s.fsm.DesiredState(), nil
}

func (s *Store) ListNamespaces(_ context.Context) ([]control.NamespaceSummary, error) {
	state := s.fsm.DesiredState()
	return namespaceSummaries(state), nil
}

func namespaceSummaries(state control.DesiredState) []control.NamespaceSummary {
	namespaces := stateNamespaces(state)
	items := make([]control.NamespaceSummary, 0, len(namespaces))
	for _, namespace := range namespaces {
		cfg, exists := state.Namespaces[namespace]
		items = append(items, namespaceSummary(namespace, cfg, exists))
	}
	return items
}

func stateNamespaces(state control.DesiredState) []string {
	namespaces := make([]string, 0, len(state.Namespaces)+1)
	hasDefault := false
	for namespace := range state.Namespaces {
		if namespace == control.DefaultNamespace {
			hasDefault = true
		}
		namespaces = append(namespaces, namespace)
	}
	if !hasDefault {
		namespaces = append(namespaces, control.DefaultNamespace)
	}
	sort.Strings(namespaces)
	return namespaces
}

func namespaceSummary(namespace string, cfg spec.Config, exists bool) control.NamespaceSummary {
	cfg = cloneConfig(cfg)
	return control.NamespaceSummary{
		Namespace:         namespace,
		Path:              control.DesiredStatePath(namespace),
		Exists:            exists,
		RouteCount:        len(cfg.Routes),
		UpstreamPoolCount: len(cfg.UpstreamPools),
	}
}

func (s *Store) GetNamespaceConfig(_ context.Context, namespace string) (control.NamespaceConfig, error) {
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return control.NamespaceConfig{}, err
	}
	state := s.fsm.DesiredState()
	cfg, exists := state.Namespaces[namespace]
	cfg = cloneConfig(cfg)
	return control.NamespaceConfig{
		Namespace:     namespace,
		Exists:        exists,
		Routes:        cfg.Routes,
		UpstreamPools: cfg.UpstreamPools,
		AppliedAt:     state.AppliedAt,
	}, nil
}

func (s *Store) ReplaceNamespaceConfig(ctx context.Context, namespace string, cfg spec.Config) (control.NamespaceConfig, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return control.NamespaceConfig{}, err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return control.NamespaceConfig{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandReplaceNamespaceConfig, Namespace: namespace, Config: cfg}); err != nil {
		return control.NamespaceConfig{}, err
	}
	return s.GetNamespaceConfig(ctx, namespace)
}

func (s *Store) CreateNamespace(ctx context.Context, namespace string) (control.NamespaceSummary, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return control.NamespaceSummary{}, err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return control.NamespaceSummary{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandCreateNamespace, Namespace: namespace}); err != nil {
		return control.NamespaceSummary{}, err
	}
	return control.NamespaceSummary{
		Namespace: namespace,
		Exists:    true,
	}, nil
}

func (s *Store) DeleteNamespace(ctx context.Context, namespace string) error {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return err
	}
	return s.apply(ctx, Command{Type: CommandDeleteNamespace, Namespace: namespace})
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

func (s *Store) CreateRoute(ctx context.Context, namespace string, route spec.RouteConfig) (spec.RouteConfig, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return spec.RouteConfig{}, err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return spec.RouteConfig{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandCreateRoute, Namespace: namespace, Route: route}); err != nil {
		return spec.RouteConfig{}, err
	}
	return cloneRoute(route), nil
}

func (s *Store) UpdateRoute(ctx context.Context, namespace, id string, route spec.RouteConfig) (spec.RouteConfig, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return spec.RouteConfig{}, err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return spec.RouteConfig{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandUpdateRoute, Namespace: namespace, RouteID: id, Route: route}); err != nil {
		return spec.RouteConfig{}, err
	}
	return cloneRoute(route), nil
}

func (s *Store) DeleteRoute(ctx context.Context, namespace, id string) error {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return err
	}
	return s.apply(ctx, Command{Type: CommandDeleteRoute, Namespace: namespace, RouteID: id})
}

func (s *Store) CreateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return spec.UpstreamPool{}, err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return spec.UpstreamPool{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandCreateUpstreamPool, Namespace: namespace, PoolID: id, Pool: pool}); err != nil {
		return spec.UpstreamPool{}, err
	}
	return cloneUpstreamPool(pool), nil
}

func (s *Store) UpdateUpstreamPool(ctx context.Context, namespace, id string, pool spec.UpstreamPool) (spec.UpstreamPool, error) {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return spec.UpstreamPool{}, err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return spec.UpstreamPool{}, err
	}
	if err := s.apply(ctx, Command{Type: CommandUpdateUpstreamPool, Namespace: namespace, PoolID: id, Pool: pool}); err != nil {
		return spec.UpstreamPool{}, err
	}
	return cloneUpstreamPool(pool), nil
}

func (s *Store) DeleteUpstreamPool(ctx context.Context, namespace, id string) error {
	if err := s.ensureLeaderWrite(ctx); err != nil {
		return err
	}
	if err := control.ValidateNamespaceName(namespace); err != nil {
		return err
	}
	return s.apply(ctx, Command{Type: CommandDeleteUpstreamPool, Namespace: namespace, PoolID: id})
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
		return &control.StateError{
			StatusCode: statusCode,
			Code:       code,
			Message:    resp.Error,
		}
	}
	return nil
}

func isRaftLeadershipError(err error) bool {
	return errors.Is(err, raft.ErrNotLeader) ||
		errors.Is(err, raft.ErrLeadershipLost) ||
		errors.Is(err, raft.ErrLeadershipTransferInProgress)
}
