package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowserArgs(t *testing.T) {
	p := t.TempDir()
	a, e := browserArguments("firefox", p, "127.0.0.1:12345")
	if e != nil || !strings.Contains(strings.Join(a, " "), "-no-remote") {
		t.Fatal(a, e)
	}
	b, e := os.ReadFile(filepath.Join(p, "user.js"))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{`"network.proxy.socks_port", 12345`, `"network.proxy.socks_remote_dns", true`, `"network.proxy.failover_direct", false`} {
		if !strings.Contains(string(b), v) {
			t.Fatal(v)
		}
	}
	for _, id := range []string{"chrome", "edge", "brave", "chromium"} {
		a, e = browserArguments(id, p, "127.0.0.1:12345")
		if e != nil || !strings.Contains(strings.Join(a, " "), "--proxy-server=socks5://127.0.0.1:12345") {
			t.Fatal(a, e)
		}
	}
	if _, e = browserArguments("chrome", p, "8.8.8.8:12345"); e == nil {
		t.Fatal("accepted nonlocal proxy")
	}
}
func TestPreferences(t *testing.T) {
	root := t.TempDir()
	r, e := newRouter(root, root, NewManager(root))
	if e != nil {
		t.Fatal(e)
	}
	p := defaultPrefs()
	p.Selected["firefox"] = true
	p.Compact = true
	p.AutoReconnect = true
	p.MinimizeToTray = false
	if e = r.save(p); e != nil {
		t.Fatal(e)
	}
	r2, e := newRouter(root, root, NewManager(root))
	if e != nil || !r2.getPrefs().Selected["firefox"] || !r2.getPrefs().Compact || !r2.getPrefs().AutoReconnect || r2.getPrefs().MinimizeToTray {
		t.Fatal(e)
	}
	p.Selected["arbitrary.exe"] = true
	if e = r.save(p); e == nil {
		t.Fatal("accepted unknown application")
	}
	if e = r.launch("chrome"); e == nil {
		t.Fatal("launched unselected browser")
	}
}
func TestRelaySessionIsolation(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	r, e := newRouter(root, root, m)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	up, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer up.Close()
	go func() {
		for {
			c, e := up.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	m.mu.Lock()
	m.generation = 1
	m.s.State = "connected"
	m.s.Proxy = up.Addr().String()
	m.mu.Unlock()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go r.relay(l, 1)
	c, e := net.Dial("tcp", l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte("test"))
	b := make([]byte, 4)
	if _, e = io.ReadFull(c, b); e != nil || string(b) != "test" {
		t.Fatal(e, string(b))
	}
	c.Close()
	m.mu.Lock()
	m.s.State = "disconnected"
	m.mu.Unlock()
	checkClosed := func() {
		t.Helper()
		c, e := net.Dial("tcp", l.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write([]byte("test"))
		if _, e = c.Read(b); e == nil {
			t.Fatal("relay forwarded after disconnect")
		}
	}
	checkClosed()
	m.mu.Lock()
	m.generation = 2
	m.s.State = "connected"
	m.mu.Unlock()
	checkClosed()
}
