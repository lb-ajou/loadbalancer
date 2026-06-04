package vip

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerAcquiresOnLeader(t *testing.T) {
	ctl, lead, manager, announcer := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runController(ctx, ctl)
	lead.send(true)
	waitSignal(t, manager.added)
	waitSignal(t, announcer.announced)
	cancel()
	waitSignal(t, done)
}

func TestControllerReleasesOnFollower(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	lead.send(true)
	waitSignal(t, manager.added)
	lead.send(false)
	waitSignal(t, manager.removed)
	cancel()
	waitSignal(t, done)
}

func TestControllerRemovesStaleVIPOnStartup(t *testing.T) {
	ctl, _, manager, _ := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	cancel()
	waitSignal(t, done)
}

func TestControllerStartupCleanupUsesReleaseTimeout(t *testing.T) {
	ctl, lead, manager, announcer := newTestController(t)
	ctl.cfg.ReleaseTimeout = 10 * time.Millisecond
	manager.setBlockRemove(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	go lead.send(true)
	waitSignal(t, manager.added)
	waitSignal(t, announcer.announced)
	cancel()
	waitSignal(t, done)
}

func TestControllerFollowerEventRemovesEvenWhenNotOwned(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	lead.send(false)
	waitSignal(t, manager.removed)
	cancel()
	waitSignal(t, done)
}

func TestControllerSkipsAcquireWhenVerifyFails(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	lead.verifyErr = errors.New("not leader")
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	lead.send(true)
	assertNoSignal(t, manager.added)
	cancel()
	waitSignal(t, done)
}

func TestControllerReleasesOnShutdown(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	lead.send(true)
	waitSignal(t, manager.added)
	cancel()
	waitSignal(t, manager.removed)
	waitSignal(t, done)
}

func TestControllerReacquireReleasesBeforeAdd(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	lead.send(true)
	waitSignal(t, manager.added)
	lead.send(true)
	waitSignal(t, manager.removed)
	waitSignal(t, manager.added)
	cancel()
	waitSignal(t, done)
}

func TestControllerStatusReportsOwnershipSnapshot(t *testing.T) {
	ctl, _, _, _ := newTestController(t)
	if got := ctl.Status(); got.Owned || got.LastError != "" {
		t.Fatalf("initial status = %+v, want not owned with no error", got)
	}
	ctl.setOwned(true)
	if got := ctl.Status(); !got.Owned || got.LastError != "" {
		t.Fatalf("owned status = %+v, want owned with no error", got)
	}
}

func TestControllerStatusRecordsVerifyFailure(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	lead.verifyErr = errors.New("not leader")
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	lead.send(true)
	assertNoSignal(t, manager.added)
	assertLastError(t, ctl, "vip verify leader failed: not leader")
	cancel()
	waitSignal(t, done)
}

func TestControllerStatusRecordsAddFailure(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	manager.addErr = errors.New("address busy")
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	lead.send(true)
	waitSignal(t, manager.added)
	assertLastError(t, ctl, "vip add failed: address busy")
	cancel()
	waitSignal(t, done)
}

func TestControllerStatusRecordsAnnounceFailure(t *testing.T) {
	ctl, lead, manager, announcer := newTestController(t)
	announcer.err = errors.New("send arp")
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	lead.send(true)
	waitSignal(t, manager.added)
	waitSignal(t, announcer.announced)
	got := ctl.Status()
	if !got.Owned {
		t.Fatalf("status owned = false, want true")
	}
	assertLastError(t, ctl, "vip garp announce failed: send arp")
	cancel()
	waitSignal(t, done)
}

func TestControllerStatusRecordsRemoveFailure(t *testing.T) {
	ctl, _, manager, _ := newTestController(t)
	manager.removeErr = errors.New("no permission")
	ctl.removeLocal(context.Background())
	assertLastError(t, ctl, "vip remove failed: no permission")
}

func TestControllerCleanupUsesReleaseTimeout(t *testing.T) {
	ctl, lead, manager, _ := newTestController(t)
	ctl.cfg.ReleaseTimeout = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := runController(ctx, ctl)
	waitSignal(t, manager.removed)
	manager.setBlockRemove(true)
	lead.send(true)
	waitSignal(t, manager.added)
	cancel()
	waitSignal(t, manager.removed)
	waitSignal(t, done)
}

func newTestController(t *testing.T) (*Controller, *fakeLeadership, *fakeManager, *fakeAnnouncer) {
	t.Helper()
	lead := &fakeLeadership{ch: make(chan bool)}
	manager := newFakeManager()
	announcer := &fakeAnnouncer{announced: make(chan struct{}, 10)}
	cfg := Config{ReleaseOnShutdown: true}
	return NewController(cfg, lead, manager, announcer, testLogger{t}), lead, manager, announcer
}

func runController(ctx context.Context, ctl *Controller) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctl.Run(ctx)
	}()
	return done
}

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for signal")
	}
}

func assertNoSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("unexpected signal")
	case <-time.After(50 * time.Millisecond):
	}
}

func assertLastError(t *testing.T, ctl *Controller, want string) {
	t.Helper()
	if got := ctl.Status().LastError; got != want {
		t.Fatalf("last error = %q, want %q", got, want)
	}
}

type fakeLeadership struct {
	ch        chan bool
	verifyErr error
}

func (l *fakeLeadership) LeaderCh() <-chan bool { return l.ch }

func (l *fakeLeadership) VerifyLeader(context.Context) error { return l.verifyErr }

func (l *fakeLeadership) send(v bool) { l.ch <- v }

type fakeManager struct {
	added       chan struct{}
	removed     chan struct{}
	blockRemove atomic.Bool
	addErr      error
	removeErr   error
}

func newFakeManager() *fakeManager {
	return &fakeManager{added: make(chan struct{}, 1), removed: make(chan struct{}, 10)}
}

func (m *fakeManager) Add(context.Context) error {
	m.added <- struct{}{}
	return m.addErr
}

func (m *fakeManager) setBlockRemove(block bool) {
	m.blockRemove.Store(block)
}

func (m *fakeManager) Remove(ctx context.Context) error {
	m.removed <- struct{}{}
	if m.removeErr != nil {
		return m.removeErr
	}
	if m.blockRemove.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

type fakeAnnouncer struct {
	announced chan struct{}
	err       error
}

func (a *fakeAnnouncer) Announce(context.Context) error {
	a.announced <- struct{}{}
	return a.err
}

type testLogger struct {
	t *testing.T
}

func (l testLogger) Printf(format string, args ...any) {
	l.t.Logf(format, args...)
}
