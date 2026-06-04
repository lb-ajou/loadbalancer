package vip

import (
	"context"
	"io"
	"log"
	"sync"
	"time"
)

type Config struct {
	AcquireDelay      time.Duration
	VerifyTimeout     time.Duration
	ReleaseTimeout    time.Duration
	ReleaseOnShutdown bool
}

type Manager interface {
	Add(context.Context) error
	Remove(context.Context) error
}

type Announcer interface {
	Announce(context.Context) error
}

type Leadership interface {
	LeaderCh() <-chan bool
	VerifyLeader(context.Context) error
}

type Status struct {
	Owned     bool   `json:"owned"`
	LastError string `json:"last_error"`
}

type Logger interface {
	Printf(string, ...any)
}

type Controller struct {
	cfg        Config
	leadership Leadership
	manager    Manager
	announcer  Announcer
	logger     Logger
	mu         sync.Mutex
	owned      bool
	lastError  string
}

func NewController(cfg Config, leadership Leadership, manager Manager, announcer Announcer, logger Logger) *Controller {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Controller{cfg: cfg, leadership: leadership, manager: manager, announcer: announcer, logger: logger}
}

func (c *Controller) Run(ctx context.Context) {
	leaderCh := c.leadership.LeaderCh()
	c.cleanupStale(ctx)
	defer c.cleanup()
	for c.handleNext(ctx, leaderCh) {
	}
}

func (c *Controller) cleanupStale(ctx context.Context) {
	cleanupCtx, cancel := c.timeoutContext(ctx, c.cfg.ReleaseTimeout)
	defer cancel()
	c.removeLocal(cleanupCtx)
}

func (c *Controller) handleNext(ctx context.Context, leaderCh <-chan bool) bool {
	select {
	case <-ctx.Done():
		return false
	case isLeader, ok := <-leaderCh:
		if !ok {
			return false
		}
		c.handleLeadership(ctx, isLeader)
		return true
	}
}

func (c *Controller) handleLeadership(ctx context.Context, isLeader bool) {
	if isLeader {
		c.reacquire(ctx)
		return
	}
	c.release(ctx)
}

func (c *Controller) reacquire(ctx context.Context) {
	if c.isOwned() {
		c.release(ctx)
	}
	if !c.isOwned() {
		c.acquire(ctx)
	}
}

func (c *Controller) acquire(ctx context.Context) {
	if !c.waitAcquireDelay(ctx) {
		return
	}
	verifyCtx, cancel := c.timeoutContext(ctx, c.cfg.VerifyTimeout)
	defer cancel()
	if err := c.leadership.VerifyLeader(verifyCtx); err != nil {
		c.recordError("verify leader", err)
		return
	}
	c.addAndAnnounce(ctx)
}

func (c *Controller) waitAcquireDelay(ctx context.Context) bool {
	if c.cfg.AcquireDelay <= 0 {
		return true
	}
	timer := time.NewTimer(c.cfg.AcquireDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Controller) addAndAnnounce(ctx context.Context) {
	if err := c.manager.Add(ctx); err != nil {
		c.recordError("add", err)
		return
	}
	c.setOwned(true)
	if err := c.announcer.Announce(ctx); err != nil {
		c.recordError("garp announce", err)
		return
	}
	c.clearLastError()
}

func (c *Controller) release(ctx context.Context) {
	c.removeLocal(ctx)
}

func (c *Controller) removeLocal(ctx context.Context) {
	if err := c.manager.Remove(ctx); err != nil {
		c.recordError("remove", err)
		return
	}
	c.setOwned(false)
	c.clearLastError()
}

func (c *Controller) cleanup() {
	if c.cfg.ReleaseOnShutdown {
		ctx, cancel := c.timeoutContext(context.Background(), c.cfg.ReleaseTimeout)
		defer cancel()
		c.release(ctx)
	}
}

func (c *Controller) timeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(parent, timeout)
}

func (c *Controller) isOwned() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.owned
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{Owned: c.owned, LastError: c.lastError}
}

func (c *Controller) setOwned(owned bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.owned = owned
}

func (c *Controller) recordError(action string, err error) {
	message := "vip " + action + " failed: " + err.Error()
	c.mu.Lock()
	c.lastError = message
	c.mu.Unlock()
	c.logger.Printf("%s", message)
}

func (c *Controller) clearLastError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastError = ""
}
