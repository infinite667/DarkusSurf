package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const browserCheckURL = "https://check.torproject.org/"

type Browser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Selected bool   `json:"selected"`
}
type Preferences struct {
	Selected       map[string]bool `json:"selected"`
	Compact        bool            `json:"compact"`
	AutoVerify     bool            `json:"autoVerify"`
	AutoReconnect  bool            `json:"autoReconnect"`
	MinimizeToTray bool            `json:"minimizeToTray"`
}
type Router struct {
	mu        sync.Mutex
	prefs     Preferences
	file      string
	root      string
	manager   *Manager
	listeners []net.Listener
	closed    bool
}

var browserNames = map[string]string{"firefox": "Mozilla Firefox", "chrome": "Google Chrome", "edge": "Microsoft Edge", "chromium": "Chromium", "brave": "Brave"}
var browserOrder = []string{"firefox", "chrome", "edge", "chromium", "brave"}

func defaultPrefs() Preferences {
	return Preferences{Selected: map[string]bool{}, MinimizeToTray: true}
}
func newRouter(root, config string, m *Manager) (*Router, error) {
	r := &Router{root: root, file: filepath.Join(config, "preferences.json"), manager: m, prefs: defaultPrefs()}
	if err := os.MkdirAll(config, 0700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(r.file)
	if err == nil {
		if len(b) > 65536 {
			return nil, errors.New("preferences file too large")
		}
		if err = json.Unmarshal(b, &r.prefs); err != nil {
			return nil, fmt.Errorf("invalid preferences: %w", err)
		}
		if r.prefs.Selected == nil {
			r.prefs.Selected = map[string]bool{}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return r, nil
}
func (r *Router) getPrefs() Preferences {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.prefs
	p.Selected = map[string]bool{}
	for k, v := range r.prefs.Selected {
		p.Selected[k] = v
	}
	return p
}
func (r *Router) save(p Preferences) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("application closing")
	}
	for id := range p.Selected {
		if _, ok := browserNames[id]; !ok {
			return errors.New("unknown browser")
		}
	}
	if p.Selected == nil {
		p.Selected = map[string]bool{}
	}
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(r.file), "prefs-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), r.file); e != nil {
		return e
	}
	r.prefs = p
	return nil
}
func (r *Router) browsers() []Browser {
	p := r.getPrefs()
	out := []Browser{}
	for _, id := range browserOrder {
		path := browserPath(id)
		out = append(out, Browser{id, browserNames[id], path, p.Selected[id]})
	}
	return out
}
func browserArguments(id, profile, proxy string) ([]string, error) {
	if !loopbackAddr(proxy) {
		return nil, errors.New("invalid proxy address")
	}
	if _, ok := browserNames[id]; !ok {
		return nil, errors.New("unsupported browser")
	}
	if id == "firefox" {
		_, port, _ := net.SplitHostPort(proxy)
		n, _ := strconv.Atoi(port)
		prefs := fmt.Sprintf(`user_pref("network.proxy.type", 1);
user_pref("network.proxy.socks", "127.0.0.1");
user_pref("network.proxy.socks_port", %d);
user_pref("network.proxy.socks_version", 5);
user_pref("network.proxy.socks_remote_dns", true);
user_pref("network.proxy.socks5_remote_dns", true);
user_pref("network.proxy.no_proxies_on", "");
user_pref("network.proxy.failover_direct", false);
user_pref("network.trr.mode", 5);
user_pref("network.dns.disablePrefetch", true);
user_pref("network.dns.disableIPv6", true);
user_pref("network.prefetch-next", false);
user_pref("network.http.speculative-parallel-limit", 0);
user_pref("media.peerconnection.enabled", false);
user_pref("network.http.http3.enable", false);
user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("browser.aboutwelcome.enabled", false);
user_pref("browser.startup.homepage_override.mstone", "ignore");
`, n)
		if e := os.WriteFile(filepath.Join(profile, "user.js"), []byte(prefs), 0600); e != nil {
			return nil, e
		}
		return []string{"-no-remote", "-new-instance", "-profile", profile, browserCheckURL}, nil
	}
	return []string{"--user-data-dir=" + profile, "--proxy-server=socks5://" + proxy, "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1", "--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--no-first-run", "--no-default-browser-check", "--new-window", browserCheckURL}, nil
}

// Relay has no direct-network fallback. It forwards only to the Tor SOCKS listener
// belonging to the captured session, and remains bound after disconnect to avoid
// reuse during this controller's lifetime. It is not an OS firewall.
func (m *Manager) proxyFor(g uint64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation != g || m.s.State != "connected" {
		return ""
	}
	return m.s.Proxy
}
func (r *Router) relay(l net.Listener, g uint64) {
	for {
		c, e := l.Accept()
		if e != nil {
			return
		}
		go func() {
			defer c.Close()
			p := r.manager.proxyFor(g)
			if !loopbackAddr(p) {
				return
			}
			up, e := net.DialTimeout("tcp", p, 3*time.Second)
			if e != nil {
				return
			}
			defer up.Close()
			if r.manager.proxyFor(g) != p {
				return
			}
			done := make(chan struct{})
			go func() {
				_, _ = io.Copy(up, c)
				if t, ok := up.(*net.TCPConn); ok {
					_ = t.CloseWrite()
				}
				close(done)
			}()
			_, _ = io.Copy(c, up)
			_ = c.Close()
			<-done
		}()
	}
}
func (r *Router) launch(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("application closing")
	}
	if !r.prefs.Selected[id] {
		return errors.New("select and save this browser first")
	}
	if _, ok := browserNames[id]; !ok {
		return errors.New("unsupported browser")
	}
	path := browserPath(id)
	if path == "" {
		return errors.New("browser not found")
	}
	if len(r.listeners) >= 64 {
		return errors.New("launch limit reached; restart DarkusSurf")
	}
	r.manager.mu.Lock()
	g := r.manager.generation
	p := r.manager.s.Proxy
	state := r.manager.s.State
	r.manager.mu.Unlock()
	if state != "connected" || !loopbackAddr(p) {
		return errors.New("connect to Tor first")
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return e
	}
	profile, e := os.MkdirTemp(r.root, "browser-"+id+"-")
	if e != nil {
		l.Close()
		return e
	}
	args, e := browserArguments(id, profile, l.Addr().String())
	if e != nil {
		l.Close()
		os.RemoveAll(profile)
		return e
	}
	if r.manager.proxyFor(g) != p {
		l.Close()
		os.RemoveAll(profile)
		return errors.New("Tor session changed; retry")
	}
	go r.relay(l, g)
	cmd := exec.Command(path, args...)
	if e = cmd.Start(); e != nil {
		l.Close()
		os.RemoveAll(profile)
		return e
	}
	r.listeners = append(r.listeners, l)
	go func() { _ = cmd.Wait() }()
	return nil
}
func (r *Router) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for _, l := range r.listeners {
		_ = l.Close()
	}
}
