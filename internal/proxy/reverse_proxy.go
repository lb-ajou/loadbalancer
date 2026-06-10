package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"loadbalancer/internal/route"
	"loadbalancer/internal/runtime"
	"loadbalancer/internal/spec"
	"loadbalancer/internal/upstream"
)

const lbNodeHeader = "X-AjouLB-LB-Node"

type Handler struct {
	state     *runtime.State
	transport *http.Transport
	proxies   sync.Map
}

func NewHandler(state *runtime.State) http.Handler {
	return &Handler{
		state:     state,
		transport: newTransport(),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	matchedRoute, pool, ok := h.resolveRequest(w, r)
	if !ok {
		return
	}
	target, release, ok := h.selectTarget(w, r, matchedRoute, pool)
	if !ok {
		http.Error(w, "matched upstream pool has no healthy targets", http.StatusBadGateway)
		return
	}
	defer release()
	h.serveProxyToTarget(w, r, target)
}

func (h *Handler) resolveRequest(w http.ResponseWriter, r *http.Request) (route.Route, *upstream.Pool, bool) {
	snapshot, ok := h.snapshotOrError(w)
	if !ok {
		return route.Route{}, nil, false
	}
	matchedRoute, ok := h.resolveRoute(w, r, snapshot.RouteTable)
	if !ok {
		return route.Route{}, nil, false
	}
	return h.resolvePool(w, snapshot.Upstreams, matchedRoute)
}

func (h *Handler) snapshotOrError(w http.ResponseWriter) (runtime.Snapshot, bool) {
	if h.state == nil {
		http.Error(w, "proxy runtime state is not configured", http.StatusBadGateway)
		return runtime.Snapshot{}, false
	}
	return h.state.Snapshot(), true
}

func (h *Handler) resolveRoute(w http.ResponseWriter, r *http.Request, routes []route.Route) (route.Route, bool) {
	matchedRoute, ok := route.Resolve(routes, r.Host, r.URL.Path)
	if !ok {
		http.Error(w, "no matching route", http.StatusNotFound)
		return route.Route{}, false
	}
	return *matchedRoute, true
}

func (h *Handler) resolvePool(w http.ResponseWriter, registry *upstream.Registry, matchedRoute route.Route) (route.Route, *upstream.Pool, bool) {
	if registry == nil {
		http.Error(w, "upstream registry is not configured", http.StatusBadGateway)
		return route.Route{}, nil, false
	}
	pool, ok := registry.Get(matchedRoute.UpstreamPool)
	if !ok {
		http.Error(w, "matched upstream pool does not exist", http.StatusBadGateway)
		return route.Route{}, nil, false
	}
	return matchedRoute, pool, true
}

func (h *Handler) serveProxyToTarget(w http.ResponseWriter, r *http.Request, target upstream.Target) {
	proxy, err := h.proxyForTarget(target)
	if err != nil {
		http.Error(w, "invalid upstream target", http.StatusBadGateway)
		return
	}
	h.addLBNodeHeader(r)
	proxy.ServeHTTP(w, r)
}

func (h *Handler) addLBNodeHeader(r *http.Request) {
	if h.state == nil {
		return
	}
	if nodeID := h.state.Snapshot().RaftIdentity.NodeID; nodeID != "" {
		r.Header.Set(lbNodeHeader, nodeID)
	}
}

func (h *Handler) proxyForTarget(target upstream.Target) (*httputil.ReverseProxy, error) {
	if proxy, ok := h.cachedProxy(target.Raw); ok {
		return proxy, nil
	}
	targetURL, err := upstreamURL(target)
	if err != nil {
		return nil, err
	}
	return h.storeProxy(target.Raw, h.newReverseProxy(targetURL)), nil
}

func (h *Handler) cachedProxy(raw string) (*httputil.ReverseProxy, bool) {
	value, ok := h.proxies.Load(raw)
	if !ok {
		return nil, false
	}
	proxy, ok := value.(*httputil.ReverseProxy)
	return proxy, ok
}

func upstreamURL(target upstream.Target) (*url.URL, error) {
	if target.URL == nil {
		return nil, errors.New("missing target url")
	}
	return target.URL, nil
}

func (h *Handler) storeProxy(raw string, proxy *httputil.ReverseProxy) *httputil.ReverseProxy {
	actual, _ := h.proxies.LoadOrStore(raw, proxy)
	return actual.(*httputil.ReverseProxy)
}

func (h *Handler) newReverseProxy(targetURL *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = h.transport
	proxy.ErrorHandler = newErrorHandler()
	return proxy
}

func newErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(rw http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(rw, fmt.Sprintf("proxy upstream error: %v", proxyErr), http.StatusBadGateway)
	}
}

func (h *Handler) selectTarget(w http.ResponseWriter, r *http.Request, matchedRoute route.Route, pool *upstream.Pool) (upstream.Target, func(), bool) {
	if pool == nil {
		return upstream.Target{}, noopTargetRelease, false
	}
	if usesLeastConnection(matchedRoute) {
		return pool.LeastConnectionTarget()
	}
	return h.nonLeastConnectionTarget(w, r, matchedRoute, pool)
}

func (h *Handler) nonLeastConnectionTarget(w http.ResponseWriter, r *http.Request, matchedRoute route.Route, pool *upstream.Pool) (upstream.Target, func(), bool) {
	if usesTupleHash(matchedRoute) {
		target, ok := h.selectTupleHashTarget(r, pool)
		return target, noopTargetRelease, ok
	}
	if !usesStickyCookie(matchedRoute) {
		target, ok := pool.NextTarget()
		return target, noopTargetRelease, ok
	}
	target, ok := h.selectStickyTarget(w, r, matchedRoute, pool)
	return target, noopTargetRelease, ok
}

func stickyCookieName(matchedRoute route.Route) string {
	name := "rp_sticky_" + matchedRoute.ID
	return strings.NewReplacer(":", "_", "/", "_", " ", "_").Replace(name)
}

func usesStickyCookie(matchedRoute route.Route) bool {
	return matchedRoute.Algorithm == string(spec.RouteAlgorithmStickyCookie)
}

func usesTupleHash(matchedRoute route.Route) bool {
	return matchedRoute.Algorithm == string(spec.RouteAlgorithmFiveTupleHash)
}

func usesLeastConnection(matchedRoute route.Route) bool {
	return matchedRoute.Algorithm == string(spec.RouteAlgorithmLeastConnection)
}

func (h *Handler) selectTupleHashTarget(r *http.Request, pool *upstream.Pool) (upstream.Target, bool) {
	return pool.HashTarget(tupleHashKey(r))
}

func tupleHashKey(r *http.Request) string {
	clientHost, clientPort := trustedClientAddress(r)
	dstHost, dstPort := requestDestination(r)
	return strings.Join([]string{r.Proto, clientHost, clientPort, dstHost, dstPort}, "|")
}

func trustedClientAddress(r *http.Request) (string, string) {
	if host := forwardedClientAddress(r); host != "" {
		return host, ""
	}
	return splitHostPort(r.RemoteAddr)
}

func forwardedClientAddress(r *http.Request) string {
	if host := forwardedHeaderAddress(r.Header.Get("Forwarded")); host != "" {
		return host
	}
	return firstForwardedFor(r.Header.Get("X-Forwarded-For"))
}

func forwardedHeaderAddress(value string) string {
	for _, part := range strings.Split(value, ",") {
		if host := forwardedPairHost(part); host != "" {
			return host
		}
	}
	return ""
}

func forwardedPairHost(value string) string {
	for _, item := range strings.Split(value, ";") {
		name, raw, ok := strings.Cut(strings.TrimSpace(item), "=")
		if ok && strings.EqualFold(name, "for") {
			return cleanForwardedHost(raw)
		}
	}
	return ""
}

func cleanForwardedHost(value string) string {
	host := strings.Trim(strings.TrimSpace(value), "\"")
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if strings.HasPrefix(strings.ToLower(host), "_") {
		return ""
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return parsedHost
	}
	return strings.TrimPrefix(host, ":")
}

func firstForwardedFor(value string) string {
	host := strings.TrimSpace(strings.Split(value, ",")[0])
	if host == "" {
		return ""
	}
	return cleanForwardedHost(host)
}

func requestDestination(r *http.Request) (string, string) {
	if host, port, err := net.SplitHostPort(r.Host); err == nil {
		return host, port
	}
	return r.Host, defaultPort(r)
}

func defaultPort(r *http.Request) string {
	if strings.HasPrefix(strings.ToUpper(r.Proto), "HTTP/") {
		return "80"
	}
	return ""
}

func splitHostPort(value string) (string, string) {
	if host, port, err := net.SplitHostPort(value); err == nil {
		return host, port
	}
	return value, ""
}

func (h *Handler) selectStickyTarget(w http.ResponseWriter, r *http.Request, matchedRoute route.Route, pool *upstream.Pool) (upstream.Target, bool) {
	target, ok, err := stickyCookieTarget(r, matchedRoute, pool)
	if err != nil || ok {
		return target, ok
	}
	target, ok = pool.NextTarget()
	if !ok {
		return upstream.Target{}, false
	}
	setStickyCookie(w, matchedRoute, target)
	return target, true
}

func stickyCookieTarget(r *http.Request, matchedRoute route.Route, pool *upstream.Pool) (upstream.Target, bool, error) {
	cookie, err := r.Cookie(stickyCookieName(matchedRoute))
	if err == nil {
		target, ok := findHealthyTarget(pool, cookie.Value)
		return target, ok, nil
	}
	if errors.Is(err, http.ErrNoCookie) {
		return upstream.Target{}, false, nil
	}
	return upstream.Target{}, false, err
}

func setStickyCookie(w http.ResponseWriter, matchedRoute route.Route, target upstream.Target) {
	http.SetCookie(w, &http.Cookie{
		Name:     stickyCookieName(matchedRoute),
		Value:    target.Raw,
		Path:     "/",
		HttpOnly: true,
	})
}

func findHealthyTarget(pool *upstream.Pool, raw string) (upstream.Target, bool) {
	if pool == nil {
		return upstream.Target{}, false
	}
	index := findTargetIndex(pool.Targets, raw)
	if index < 0 {
		return upstream.Target{}, false
	}
	return targetState(pool, index)
}

func findTargetIndex(targets []upstream.Target, raw string) int {
	for i, target := range targets {
		if target.Raw == raw {
			return i
		}
	}
	return -1
}

func targetState(pool *upstream.Pool, index int) (upstream.Target, bool) {
	states := pool.SnapshotStates()
	if index < len(states) && !states[index].Healthy {
		return upstream.Target{}, false
	}
	return pool.Targets[index], true
}

func noopTargetRelease() {}
