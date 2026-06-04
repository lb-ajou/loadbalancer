package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/raft"

	"reverseproxy-poc/internal/vip"
	vipruntime "reverseproxy-poc/internal/vip/runtime"
)

type vipRunner interface {
	Run(context.Context)
}

type raftLeadership struct {
	raft *raft.Raft
}

func (l raftLeadership) LeaderCh() <-chan bool {
	return l.raft.LeaderCh()
}

func (l raftLeadership) VerifyLeader(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- l.raft.VerifyLeader().Error()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) configureVIP(cfg vipruntime.Config, node *raft.Raft) error {
	runner, err := newVIPController(cfg, node, a.logger)
	if err != nil {
		return err
	}
	a.vipController = runner
	return nil
}

func (a *App) reconfigureVIP(cfg vipruntime.Config, node *raft.Raft) {
	a.stopVIPController()
	runner, err := newVIPController(cfg, node, a.logger)
	if err != nil {
		a.logger.Printf("configure vip failed: %v", err)
		return
	}
	a.mu.Lock()
	a.vipController = runner
	runCtx := a.runCtx
	a.mu.Unlock()
	if runCtx != nil {
		a.startVIPController(runCtx)
	}
}

func newVIPController(cfg vipruntime.Config, node *raft.Raft, logger *log.Logger) (vipRunner, error) {
	if !cfg.Active() {
		return nil, nil
	}
	if node == nil {
		return nil, fmt.Errorf("raft node is required for vip")
	}
	manager, announcer, err := newVIPNetwork(cfg)
	if err != nil {
		return nil, err
	}
	return vip.NewController(vipConfig(cfg), raftLeadership{node}, manager, announcer, logger), nil
}

func newVIPNetwork(cfg vipruntime.Config) (vip.Manager, vip.Announcer, error) {
	garpInterval, err := time.ParseDuration(cfg.GARPInterval)
	if err != nil {
		return nil, nil, fmt.Errorf("parse vip garp interval: %w", err)
	}
	manager, err := vip.NewNetlinkManager(cfg.Interface, cfg.Address)
	if err != nil {
		return nil, nil, err
	}
	return newVIPAnnouncer(cfg, garpInterval, manager)
}

func newVIPAnnouncer(cfg vipruntime.Config, interval time.Duration, manager vip.Manager) (vip.Manager, vip.Announcer, error) {
	announcer, err := vip.NewARPAnnouncer(cfg.Interface, cfg.Address, cfg.GARPCount, interval)
	if err != nil {
		return nil, nil, err
	}
	return manager, announcer, nil
}

func vipConfig(cfg vipruntime.Config) vip.Config {
	delay, _ := time.ParseDuration(cfg.AcquireDelay)
	return vip.Config{
		AcquireDelay:      delay,
		VerifyTimeout:     5 * time.Second,
		ReleaseTimeout:    5 * time.Second,
		ReleaseOnShutdown: cfg.ReleaseOnShutdown,
	}
}
