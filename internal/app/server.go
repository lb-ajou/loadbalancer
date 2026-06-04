package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	a.startHealthChecker(ctx)
	a.startVIPController(ctx)
	defer a.stopVIPController()
	defer a.stopHealthChecker()

	go a.serve("proxy", a.proxyServer, errCh)
	go a.serve("dashboard", a.dashboardServer, errCh)

	select {
	case <-ctx.Done():
		return a.shutdownWithTimeout(context.Background())
	case err := <-errCh:
		_ = a.shutdownWithTimeout(context.Background())
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	var errs []error

	if a.proxyServer != nil {
		if err := a.proxyServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown proxy server: %w", err))
		}
	}
	if a.dashboardServer != nil {
		if err := a.dashboardServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown dashboard server: %w", err))
		}
	}
	a.stopVIPController()
	if a.raftNode != nil {
		if err := a.raftNode.Shutdown(); err != nil {
			errs = append(errs, fmt.Errorf("shutdown raft node: %w", err))
		}
	}

	return errors.Join(errs...)
}

func (a *App) shutdownWithTimeout(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return a.Shutdown(shutdownCtx)
}

func (a *App) serve(name string, srv *http.Server, errCh chan<- error) {
	a.logger.Printf("%s server listening on %s", name, srv.Addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- fmt.Errorf("%s server failed: %w", name, err)
	}
}

func (a *App) startVIPController(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vipController == nil || a.vipCancel != nil {
		return
	}
	vipCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.vipCancel = cancel
	a.vipDone = done
	go func() {
		defer close(done)
		a.vipController.Run(vipCtx)
	}()
}

func (a *App) stopVIPController() {
	a.mu.Lock()
	cancel, done := a.vipCancel, a.vipDone
	a.vipCancel, a.vipDone = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
