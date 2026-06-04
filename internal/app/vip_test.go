package app

import (
	"context"
	"testing"
	"time"
)

func TestAppVIPControllerCancelCancelsContext(t *testing.T) {
	runner := newFakeVIPRunner()
	app := &App{vipController: runner}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.startVIPController(ctx)
	waitClosed(t, runner.started)
	app.stopVIPController()
	waitClosed(t, runner.stopped)
}

type fakeVIPRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func newFakeVIPRunner() *fakeVIPRunner {
	return &fakeVIPRunner{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (r *fakeVIPRunner) Run(ctx context.Context) {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
}

func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for close")
	}
}
