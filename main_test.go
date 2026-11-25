package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBootstrapAndListener(t *testing.T) {
	p, d := bootstrap([]string{`status/bootstrap-phase=NOTICE BOOTSTRAP PROGRESS=80 TAG=ap_conn SUMMARY="Connecting to relay"`})
	if p != 80 || d != "Connecting to relay" {
		t.Fatalf("%d %s", p, d)
	}
	if socksAddress([]string{`net/listeners/socks="127.0.0.1:12345"`}) != "127.0.0.1:12345" {
		t.Fatal("bad listener")
	}
	for _, a := range []string{"0.0.0.0:9050", "8.8.8.8:9050", "127.0.0.1:0", "127.0.0.1:99999"} {
		if loopbackAddr(a) {
			t.Fatalf("accepted %s", a)
		}
	}
}
func TestControlProtocol(t *testing.T) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	cookie := bytes.Repeat([]byte{0x42}, 32)
	errs := make(chan string, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			errs <- e.Error()
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		line, _ := r.ReadString('\n')
		if line != "AUTHENTICATE "+hex.EncodeToString(cookie)+"\r\n" {
			errs <- "wrong auth"
			return
		}
		io.WriteString(c, "250 OK\r\n")
		line, _ = r.ReadString('\n')
		if line != "GETINFO status/bootstrap-phase\r\n" {
			errs <- "wrong command"
			return
		}
		io.WriteString(c, "250-status/bootstrap-phase=NOTICE BOOTSTRAP PROGRESS=100 SUMMARY=\"Done\"\r\n250 OK\r\n")
		errs <- ""
	}()
	lines, e := controlCommand(l.Addr().String(), cookie, "GETINFO status/bootstrap-phase")
	if e != nil {
		t.Fatal(e)
	}
	if p, _ := bootstrap(lines); p != 100 {
		t.Fatal(lines)
	}
	if s := <-errs; s != "" {
		t.Fatal(s)
	}
	if _, e := readReply(bufio.NewReader(strings.NewReader("515 Bad authentication\r\n"))); e == nil {
		t.Fatal("accepted failure")
	}
}
func TestSOCKSRemoteDNS(t *testing.T) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	got := make(chan []byte, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 3)
		io.ReadFull(c, b)
		c.Write([]byte{5, 0})
		h := make([]byte, 5)
		io.ReadFull(c, h)
		tail := make([]byte, int(h[4])+2)
		io.ReadFull(c, tail)
		got <- append(h, tail...)
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, e := socksDial(ctx, l.Addr().String(), "check.torproject.org:443")
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	want := append([]byte{5, 1, 0, 3, byte(len("check.torproject.org"))}, []byte("check.torproject.org")...)
	want = append(want, 1, 187)
	if b := <-got; !bytes.Equal(want, b) {
		t.Fatalf("want %v got %v", want, b)
	}
	if _, e = socksDial(ctx, "8.8.8.8:9050", "example.com:443"); e == nil {
		t.Fatal("nonlocal proxy accepted")
	}
}
func TestAPIAccess(t *testing.T) {
	m := NewManager(t.TempDir())
	defer m.Close()
	host := "127.0.0.1:23456"
	h := apiHandler(m, "secret", host, func() {}, func() {})
	for _, tc := range []struct {
		host, origin, token, method, path string
		want                              int
	}{{host, "", "", "GET", "/api/status", 401}, {"evil.test", "", "secret", "GET", "/api/status", 403}, {host, "https://evil.test", "secret", "POST", "/api/connect", 403}, {host, "", "secret", "GET", "/api/connect", 405}, {host, "", "secret", "GET", "/api/status", 200}, {host, "", "secret", "POST", "/api/verify", 409}, {host, "", "secret", "POST", "/api/circuit", 409}, {host, "", "", "GET", "/", 200}} {
		r := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if tc.path == "/" && w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
}
func TestStopWithoutStart(t *testing.T) {
	m := NewManager(t.TempDir())
	m.Stop()
	if m.Snapshot().State != "disconnected" {
		t.Fatal("bad initial state")
	}
	m.Close()
	if e := m.Start(); e == nil {
		t.Fatal("closed manager restarted")
	}
}
