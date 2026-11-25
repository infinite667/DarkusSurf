package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A scoped HTTP proxy. Every upstream TCP dial is through the session-bound
// Tor SOCKS listener; no direct fallback, UDP, or locally resolved DNS.
type HTTPRelay struct {
	m      *Manager
	g      uint64
	l      net.Listener
	srv    *http.Server
	tr     *http.Transport
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	slots  chan struct{}
}

func newHTTPRelay(m *Manager) (*HTTPRelay, error) {
	m.mu.Lock()
	g, p, state := m.generation, m.s.Proxy, m.s.State
	m.mu.Unlock()
	if state != "connected" || !loopbackAddr(p) {
		return nil, errors.New("connect to Tor first")
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	r := &HTTPRelay{m: m, g: g, l: l, conns: map[net.Conn]struct{}{}, slots: make(chan struct{}, 128)}
	r.tr = &http.Transport{DialContext: func(ctx context.Context, network, target string) (net.Conn, error) { return r.dial(ctx, target) }, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, ForceAttemptHTTP2: false}
	r.srv = &http.Server{Handler: r, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32768}
	go func() { _ = r.srv.Serve(l) }()
	return r, nil
}
func proxyTarget(target string) error {
	host, port, e := net.SplitHostPort(target)
	if e != nil || host == "" || len(host) > 253 {
		return errors.New("invalid proxy destination")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return errors.New("invalid destination port")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return errors.New("local/private or IPv6-literal destinations are blocked")
		}
	} else {
		h := strings.ToLower(strings.TrimSuffix(host, "."))
		if !strings.Contains(h, ".") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") {
			return errors.New("local names are blocked")
		}
	}
	return nil
}
func (r *HTTPRelay) dial(ctx context.Context, target string) (net.Conn, error) {
	if e := proxyTarget(target); e != nil {
		return nil, e
	}
	p := r.m.proxyFor(r.g)
	if !loopbackAddr(p) {
		return nil, errors.New("Tor session unavailable; no direct fallback")
	}
	c, e := socksDial(ctx, p, target)
	if e != nil {
		return nil, e
	}
	if r.m.proxyFor(r.g) != p {
		c.Close()
		return nil, errors.New("Tor session changed")
	}
	return c, nil
}
func stripHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, n := range strings.Split(v, ",") {
			h.Del(strings.TrimSpace(n))
		}
	}
	for _, n := range []string{"Connection", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(n)
	}
}
func (r *HTTPRelay) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		http.Error(w, "Proxy busy", 503)
		return
	}
	if r.m.proxyFor(r.g) == "" {
		http.Error(w, "Tor unavailable; reconnect or disable system proxy", 503)
		return
	}
	if q.Method == http.MethodConnect {
		r.connect(w, q)
		return
	}
	if q.URL.Scheme != "http" || q.URL.Host == "" || q.URL.User != nil {
		http.Error(w, "HTTP proxy requests only", 400)
		return
	}
	target := q.URL.Host
	if q.URL.Port() == "" {
		target = net.JoinHostPort(q.URL.Hostname(), "80")
	}
	if proxyTarget(target) != nil {
		http.Error(w, "Destination blocked", 403)
		return
	}
	out := q.Clone(q.Context())
	out.RequestURI = ""
	out.Host = q.URL.Host
	stripHop(out.Header)
	out.Header.Del("Forwarded")
	out.Header.Del("X-Forwarded-For")
	resp, e := r.tr.RoundTrip(out)
	if e != nil {
		http.Error(w, "Tor proxy request failed", 502)
		return
	}
	defer resp.Body.Close()
	stripHop(resp.Header)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
func (r *HTTPRelay) track(c net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.conns[c] = struct{}{}
	return true
}
func (r *HTTPRelay) untrack(c net.Conn) { r.mu.Lock(); delete(r.conns, c); r.mu.Unlock() }
func (r *HTTPRelay) connect(w http.ResponseWriter, q *http.Request) {
	up, e := r.dial(q.Context(), q.Host)
	if e != nil {
		http.Error(w, "Tor tunnel unavailable", 502)
		return
	}
	defer up.Close()
	h, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Tunnel unavailable", 500)
		return
	}
	c, b, e := h.Hijack()
	if e != nil {
		return
	}
	defer c.Close()
	if !r.track(c) {
		return
	}
	defer r.untrack(c)
	if !r.track(up) {
		return
	}
	defer r.untrack(up)
	if _, e = b.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); e != nil {
		return
	}
	if e = b.Flush(); e != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(up, b)
		if t, ok := up.(*net.TCPConn); ok {
			_ = t.CloseWrite()
		}
		close(done)
	}()
	_, _ = io.Copy(c, up)
	_ = c.Close()
	_ = up.Close()
	<-done
}
func (r *HTTPRelay) Close() {
	r.mu.Lock()
	r.closed = true
	for c := range r.conns {
		_ = c.Close()
	}
	r.mu.Unlock()
	_ = r.srv.Close()
	r.tr.CloseIdleConnections()
}
