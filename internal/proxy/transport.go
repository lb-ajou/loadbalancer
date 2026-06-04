package proxy

import (
	"net"
	"net/http"
	"time"
)

type transportConfig struct {
	maxIdleConns        int
	maxIdleConnsPerHost int
	maxConnsPerHost     int
	idleConnTimeout     time.Duration
	responseHeaderWait  time.Duration
}

func newTransport() *http.Transport {
	return applyTransportDefaults(cloneDefaultTransport(), defaultTransportConfig())
}

func cloneDefaultTransport() *http.Transport {
	return http.DefaultTransport.(*http.Transport).Clone()
}

func applyTransportDefaults(t *http.Transport, cfg transportConfig) *http.Transport {
	t.MaxIdleConns = cfg.maxIdleConns
	t.MaxIdleConnsPerHost = cfg.maxIdleConnsPerHost
	t.MaxConnsPerHost = cfg.maxConnsPerHost
	t.IdleConnTimeout = cfg.idleConnTimeout
	t.ResponseHeaderTimeout = cfg.responseHeaderWait
	t.ExpectContinueTimeout = time.Second
	t.DialContext = (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return t
}

func defaultTransportConfig() transportConfig {
	return transportConfig{
		maxIdleConns:        512,
		maxIdleConnsPerHost: 128,
		maxConnsPerHost:     256,
		idleConnTimeout:     90 * time.Second,
		responseHeaderWait:  2 * time.Second,
	}
}
