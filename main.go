package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed ui/*
var webAssets embed.FS

type Status struct {
	State    string   `json:"state"`
	Progress int      `json:"progress"`
	Detail   string   `json:"detail"`
	Proxy    string   `json:"proxy"`
	IP       string   `json:"ip"`
	Verified bool     `json:"verified"`
	Started  int64    `json:"started"`
	Logs     []string `json:"logs"`
	Checking bool     `json:"checking"`
	Monitor  Monitor  `json:"monitor"`
}
type Manager struct {
	mu              sync.Mutex
	s               Status
	cancel          context.CancelFunc
	generation      uint64
	control, cookie string
	nextIdentity    time.Time
	bridges         []string
	closed          bool
	wg              sync.WaitGroup
	root            string
	router          *Router
}

func NewManager(root string) *Manager {
	return &Manager{root: root, s: Status{State: "disconnected", Detail: "Tor is not running"}}
}
func (m *Manager) logLocked(s string) {
	if len(s) > 400 {
		s = s[:400]
	}
	m.s.Logs = append(m.s.Logs, time.Now().Format("15:04:05")+"  "+s)
	if len(m.s.Logs) > 80 {
		m.s.Logs = m.s.Logs[len(m.s.Logs)-80:]
	}
}
func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.s
	s.Logs = append([]string{}, s.Logs...)
	return s
}
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("application closing")
	}
	if m.s.State != "disconnected" && m.s.State != "error" {
		return errors.New("stop the existing session first")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.generation++
	g := m.generation
	m.s = Status{State: "starting", Detail: "Preparing Tor engine"}
	m.logLocked("Starting isolated Tor session")
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.run(ctx, g) }()
	return nil
}
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.s.State = "stopping"
		m.s.Detail = "Stopping Tor"
		m.s.Proxy = ""
		m.s.Verified = false
		m.s.IP = ""
		m.s.Started = 0
	}
}
func (m *Manager) Close() { m.mu.Lock(); m.closed = true; m.mu.Unlock(); m.Stop(); m.wg.Wait() }
func (m *Manager) run(ctx context.Context, g uint64) {
	var failure error
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.generation != g {
			return
		}
		m.cancel = nil
		m.control = ""
		m.cookie = ""
		m.s.Proxy = ""
		m.s.Verified = false
		m.s.IP = ""
		m.s.Started = 0
		m.s.Checking = false
		m.s.Progress = 0
		m.s.Monitor = Monitor{}
		if failure != nil && ctx.Err() == nil {
			m.s.State = "error"
			m.s.Detail = failure.Error()
			m.logLocked("Error: " + failure.Error())
		} else {
			m.s.State = "disconnected"
			m.s.Detail = "Tor is not running"
			m.logLocked("Session stopped")
		}
	}()
	dir, err := os.MkdirTemp(m.root, "session-")
	if err != nil {
		failure = err
		return
	}
	defer os.RemoveAll(dir)
	executable, extra, err := prepareTor(dir)
	if err != nil {
		failure = fmt.Errorf("Tor engine unavailable: %w", err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	dat := filepath.Join(dir, "state")
	if err = os.MkdirAll(dat, 0700); err != nil {
		failure = err
		return
	}
	cp := filepath.Join(dir, "control-port")
	cookie := filepath.Join(dat, "control_auth_cookie")
	empty := filepath.Join(dir, "torrc")
	if err = os.WriteFile(empty, nil, 0600); err != nil {
		failure = err
		return
	}
	args := []string{"-f", empty, "--DataDirectory", dat, "--SocksPort", "auto NoIPv6Traffic", "--SafeSocks", "1", "--ControlPort", "auto", "--ControlPortWriteToFile", cp, "--CookieAuthentication", "1", "--CookieAuthFile", cookie, "--ClientOnly", "1", "--SafeLogging", "1", "--Log", "notice stdout", "--AvoidDiskWrites", "1", "--__OwningControllerProcess", strconv.Itoa(os.Getpid())}
	args = append(args, extra...)
	m.mu.Lock()
	bridges := append([]string(nil), m.bridges...)
	m.mu.Unlock()
	ba, e := bridgeArgs(executable, bridges)
	if e != nil {
		failure = e
		return
	}
	args = append(args, ba...)
	c := exec.CommandContext(ctx, executable, args...)
	hideProcess(c)
	out, err := c.StdoutPipe()
	if err != nil {
		failure = err
		return
	}
	c.Stderr = c.Stdout
	if err = c.Start(); err != nil {
		failure = err
		return
	}
	m.mu.Lock()
	m.s.Detail = "Connecting to Tor network"
	m.mu.Unlock()
	// Drain output without retaining addresses or raw logs.
	go func() { _, _ = io.Copy(io.Discard, out) }()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = c.Process.Kill()
			<-done
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()
	ready := false
	misses := 0
	var lastSample Monitor
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-done:
			waited = true
			if e != nil {
				failure = fmt.Errorf("Tor process exited: %w", e)
			} else {
				failure = errors.New("Tor process exited")
			}
			return
		case <-timeout.C:
			if !ready {
				failure = errors.New("Tor bootstrap timed out; check network access")
				return
			}
		case <-ticker.C:
			if ready {
				misses++
				if misses >= 4 {
					failure = errors.New("Tor controller stopped responding; session closed")
					return
				}
			}
			b, e := os.ReadFile(cp)
			if e != nil {
				continue
			}
			addr := ""
			for _, line := range strings.Split(string(b), "\n") {
				candidate := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "PORT="))
				if loopbackAddr(candidate) {
					addr = candidate
					break
				}
			}
			if !loopbackAddr(addr) {
				continue
			}
			auth, e := os.ReadFile(cookie)
			if e != nil || len(auth) != 32 {
				continue
			}
			lines, e := controlCommand(addr, auth, "GETINFO status/bootstrap-phase")
			if e != nil {
				continue
			}
			misses = 0
			progress, detail := bootstrap(lines)
			m.mu.Lock()
			if m.generation != g || ctx.Err() != nil {
				m.mu.Unlock()
				return
			}
			m.s.Progress = progress
			if !ready {
				m.s.Detail = detail
			}
			m.mu.Unlock()
			if progress == 100 && !ready {
				ls, e := controlCommand(addr, auth, "GETINFO net/listeners/socks")
				if e != nil {
					continue
				}
				proxy := socksAddress(ls)
				if proxy == "" {
					continue
				}
				m.mu.Lock()
				if ctx.Err() != nil {
					m.mu.Unlock()
					return
				}
				m.control = addr
				m.cookie = cookie
				m.s.Proxy = proxy
				m.s.State = "connected"
				m.s.Started = time.Now().Unix()
				m.s.Detail = "Tor SOCKS proxy ready"
				m.nextIdentity = time.Time{}
				m.logLocked("Tor bootstrap complete; application proxy available")
				m.mu.Unlock()
				ready = true
				timeout.Stop()
			}
			if ready && (lastSample.Updated == 0 || time.Now().UnixMilli()-lastSample.Updated >= 2000) {
				lastSample = m.sample(g, addr, auth, lastSample)
			}
		}
	}
}
func loopbackAddr(a string) bool {
	h, p, e := net.SplitHostPort(a)
	n, ne := strconv.Atoi(p)
	return e == nil && ne == nil && n > 0 && n <= 65535 && h == "127.0.0.1"
}

var progressRE = regexp.MustCompile(`PROGRESS=([0-9]+)`)
var summaryRE = regexp.MustCompile(`SUMMARY="([^"\r\n]*)"`)

func bootstrap(lines []string) (int, string) {
	s := strings.Join(lines, " ")
	n := 0
	if a := progressRE.FindStringSubmatch(s); len(a) > 1 {
		n, _ = strconv.Atoi(a[1])
		if n > 100 {
			n = 100
		}
	}
	d := "Connecting to Tor"
	if a := summaryRE.FindStringSubmatch(s); len(a) > 1 {
		d = a[1]
	}
	return n, d
}

var socksRE = regexp.MustCompile(`127\.0\.0\.1:[0-9]+`)

func socksAddress(lines []string) string {
	for _, s := range lines {
		a := socksRE.FindString(s)
		if loopbackAddr(a) {
			return a
		}
	}
	return ""
}
func readReply(r *bufio.Reader) ([]string, error) {
	var lines []string
	for i := 0; i < 512; i++ {
		line, e := r.ReadString('\n')
		if e != nil {
			return nil, e
		}
		if len(line) > 8192 {
			return nil, errors.New("oversized control reply")
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 4 || line[:3] != "250" {
			return nil, errors.New("Tor rejected control command")
		}
		lines = append(lines, line[4:])
		switch line[3] {
		case ' ':
			return lines, nil
		case '-':
			continue
		case '+':
			for j := 0; j < 512; j++ {
				data, e := r.ReadString('\n')
				if e != nil {
					return nil, e
				}
				if len(data) > 8192 {
					return nil, errors.New("oversized control data")
				}
				data = strings.TrimRight(data, "\r\n")
				if data == "." {
					break
				}
				if j == 511 {
					return nil, errors.New("control data too long")
				}
				lines = append(lines, strings.TrimPrefix(data, "."))
			}
		default:
			return nil, errors.New("unexpected control response")
		}
	}
	return nil, errors.New("control response too long")
}
func controlCommand(addr string, cookie []byte, cmd string) ([]string, error) {
	if !loopbackAddr(addr) || len(cookie) != 32 {
		return nil, errors.New("invalid local Tor controller")
	}
	c, e := net.DialTimeout("tcp", addr, 2*time.Second)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	if _, e = fmt.Fprintf(c, "AUTHENTICATE %s\r\n", hex.EncodeToString(cookie)); e != nil {
		return nil, e
	}
	if _, e = readReply(r); e != nil {
		return nil, e
	}
	if _, e = fmt.Fprintf(c, "%s\r\n", cmd); e != nil {
		return nil, e
	}
	return readReply(r)
}
func (m *Manager) NewCircuit() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.s.State != "connected" {
		return errors.New("connect to Tor first")
	}
	if m.s.Checking {
		return errors.New("wait for the verification to finish")
	}
	if time.Now().Before(m.nextIdentity) {
		return errors.New("wait at least 10 seconds between circuit requests")
	}
	b, e := os.ReadFile(m.cookie)
	if e != nil {
		return e
	}
	if _, e = controlCommand(m.control, b, "SIGNAL NEWNYM"); e != nil {
		return e
	}
	m.nextIdentity = time.Now().Add(10 * time.Second)
	m.s.Verified = false
	m.s.IP = ""
	m.logLocked("Requested new circuits for future connections; exit IP may remain unchanged")
	return nil
}

// SOCKS5 domain-name requests leave DNS resolution to Tor. No direct fallback.
func socksDial(ctx context.Context, proxy, target string) (net.Conn, error) {
	if !loopbackAddr(proxy) {
		return nil, errors.New("invalid proxy")
	}
	host, port, e := net.SplitHostPort(target)
	if e != nil {
		return nil, e
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 || len(host) == 0 || len(host) > 255 {
		return nil, errors.New("invalid target")
	}
	d := net.Dialer{Timeout: 8 * time.Second}
	c, e := d.DialContext(ctx, "tcp", proxy)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if deadline, has := ctx.Deadline(); has && deadline.Before(time.Now().Add(15*time.Second)) {
		_ = c.SetDeadline(deadline)
	}
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return nil, e
	}
	b := make([]byte, 2)
	if _, e = io.ReadFull(c, b); e != nil {
		return nil, e
	}
	if b[0] != 5 || b[1] != 0 {
		return nil, errors.New("SOCKS negotiation failed")
	}
	req := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	req = append(req, byte(n>>8), byte(n))
	if _, e = c.Write(req); e != nil {
		return nil, e
	}
	head := make([]byte, 4)
	if _, e = io.ReadFull(c, head); e != nil {
		return nil, e
	}
	if head[0] != 5 || head[1] != 0 {
		return nil, errors.New("SOCKS connection rejected")
	}
	size := 0
	switch head[3] {
	case 1:
		size = 4
	case 4:
		size = 16
	case 3:
		if _, e = io.ReadFull(c, b[:1]); e != nil {
			return nil, e
		}
		size = int(b[0])
	default:
		return nil, errors.New("invalid SOCKS response")
	}
	if _, e = io.ReadFull(c, make([]byte, size+2)); e != nil {
		return nil, e
	}
	_ = c.SetDeadline(time.Time{})
	ok = true
	return c, nil
}
func (m *Manager) Verify() error {
	m.mu.Lock()
	if m.s.State != "connected" || m.s.Checking {
		m.mu.Unlock()
		return errors.New("Tor must be connected and idle")
	}
	g := m.generation
	proxy := m.s.Proxy
	m.s.Checking = true
	m.s.Verified = false
	m.s.IP = ""
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		tr := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return socksDial(ctx, proxy, address)
		}, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second}
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect blocked") }}
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://check.torproject.org/api/ip", nil)
		resp, e := client.Do(req)
		var result struct {
			IsTor bool
			IP    string
		}
		if e == nil {
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				e = errors.New("verification service returned an error")
			} else {
				e = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result)
			}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.generation != g || m.s.State != "connected" {
			return
		}
		m.s.Checking = false
		if e != nil {
			m.logLocked("Tor verification failed; no direct fallback was used")
			return
		}
		if !result.IsTor || net.ParseIP(result.IP) == nil {
			m.logLocked("Verification did not confirm a Tor exit")
			return
		}
		m.s.Verified = true
		m.s.IP = result.IP
		m.logLocked("Tor Project verified this proxied request")
	}()
	return nil
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func apiHandler(m *Manager, secret, host string, quit func(), touch func()) http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webAssets, "ui")
	static := http.FileServer(http.FS(sub))
	mux.Handle("/", static)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "origin rejected", 403)
			return
		}
		touch()
		if r.URL.Path == "/api/status" && r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(m.Snapshot())
			return
		}
		if m.router != nil && r.Method == "GET" {
			switch r.URL.Path {
			case "/api/browsers":
				_ = json.NewEncoder(w).Encode(m.router.browsers())
				return
			case "/api/preferences":
				_ = json.NewEncoder(w).Encode(m.router.getPrefs())
				return
			}
		}
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		var e error
		switch r.URL.Path {
		case "/api/preferences":
			if m.router == nil {
				e = errors.New("routing unavailable")
				break
			}
			var p Preferences
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
			d.DisallowUnknownFields()
			if e = d.Decode(&p); e == nil {
				e = m.router.save(p)
			}
		case "/api/launch-browser":
			if m.router == nil {
				e = errors.New("routing unavailable")
				break
			}
			var b struct {
				ID string `json:"id"`
			}
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
			d.DisallowUnknownFields()
			if e = d.Decode(&b); e == nil {
				e = m.router.launch(b.ID)
			}
		case "/api/connect":
			e = m.Start()
		case "/api/disconnect":
			m.Stop()
		case "/api/verify":
			e = m.Verify()
		case "/api/circuit":
			e = m.NewCircuit()
		case "/api/quit":
			defer quit()
		default:
			http.NotFound(w, r)
			return
		}
		if e != nil {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": e.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "invalid host", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}
func legacyMain() {
	root, e := os.MkdirTemp("", "DarkusSurf-")
	if e != nil {
		fatal(e)
		return
	}
	defer os.RemoveAll(root)
	m := NewManager(root)
	defer m.Close()
	config, e := os.UserConfigDir()
	if e != nil {
		fatal(e)
		return
	}
	router, e := newRouter(root, filepath.Join(config, "DarkusSurf"), m)
	if e != nil {
		fatal(e)
		return
	}
	m.router = router
	defer router.Close()
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		fatal(e)
		return
	}
	defer listener.Close()
	host := listener.Addr().String()
	secret := token()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var lastMu sync.Mutex
	last := time.Now()
	touch := func() { lastMu.Lock(); last = time.Now(); lastMu.Unlock() }
	srv := &http.Server{Handler: apiHandler(m, secret, host, stop, touch), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	url := "http://" + host + "/#" + secret
	if e = openUI(url); e != nil {
		fatal(fmt.Errorf("could not open interface: %w", e))
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lastMu.Lock()
			idle := time.Since(last)
			lastMu.Unlock()
			if idle > 5*time.Minute {
				return
			}
		}
	}
}
