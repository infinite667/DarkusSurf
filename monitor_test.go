package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMonitorReply(t *testing.T) {
	line := "250-traffic/read=12345\r\n250-traffic/written=456\r\n250+circuit-status=\r\n12 BUILT $" + strings.Repeat("A", 40) + "~Guard,$" + strings.Repeat("B", 40) + "~Middle,$" + strings.Repeat("C", 40) + "~Exit PURPOSE=GENERAL\r\n.\r\n250 OK\r\n"
	ls, e := readReply(bufio.NewReader(strings.NewReader(line)))
	if e != nil {
		t.Fatal(e)
	}
	s, e := parseMonitor(ls)
	if e != nil || s.Read != 12345 || s.Written != 456 || len(s.Circuits) != 1 || strings.Join(s.Circuits[0].Hops, ",") != "Guard,Middle,Exit" {
		t.Fatalf("%+v %v", s, e)
	}
}
func TestBridgeInput(t *testing.T) {
	fp := strings.Repeat("A", 40)
	for _, raw := range []string{"Bridge 192.0.2.1:443 " + fp, "obfs4 192.0.2.2:443 " + fp + " cert=abc123+/ iat-mode=0"} {
		if _, e := parseBridges(raw); e != nil {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{"UseBridges 1", "obfs4 192.0.2.1:443 " + fp + " cert=abc iat-mode=0 extra=bad", "192.0.2.1:443 " + fp + "\nControlPort 0", "192.0.2.1:0 " + fp} {
		if _, e := parseBridges(raw); e == nil {
			t.Fatalf("accepted invalid bridge %q", raw)
		}
	}
}
func TestHTTPRelayRemoteDNSAndDisconnect(t *testing.T) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	seen := make(chan string, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		b := make([]byte, 3)
		io.ReadFull(c, b)
		c.Write([]byte{5, 0})
		h := make([]byte, 5)
		io.ReadFull(c, h)
		if h[3] != 3 {
			return
		}
		tail := make([]byte, int(h[4])+2)
		io.ReadFull(c, tail)
		seen <- string(tail[:len(tail)-2])
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		req, e := http.ReadRequest(bufio.NewReader(c))
		if e != nil {
			return
		}
		req.Body.Close()
		io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK")
	}()
	m := NewManager(t.TempDir())
	m.mu.Lock()
	m.generation = 1
	m.s.State = "connected"
	m.s.Proxy = l.Addr().String()
	m.mu.Unlock()
	r, e := newHTTPRelay(m)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	u, _ := url.Parse("http://" + r.l.Addr().String())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 4 * time.Second}
	resp, e := client.Get("http://example.com/")
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "OK" || resp.StatusCode != 200 {
		t.Fatalf("%s %d", body, resp.StatusCode)
	}
	if <-seen != "example.com" {
		t.Fatal("target not forwarded as hostname")
	}
	m.mu.Lock()
	m.s.State = "disconnected"
	m.mu.Unlock()
	resp, e = client.Get("http://example.com/")
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatal(resp.StatusCode)
	}
}
func TestProxyTarget(t *testing.T) {
	for _, s := range []string{"127.0.0.1:80", "[::1]:443", "[2001:db8::1]:443", "192.168.1.1:80", "localhost:80", "foo.local:80"} {
		if proxyTarget(s) == nil {
			t.Fatal(s)
		}
	}
	if e := proxyTarget("example.com:443"); e != nil {
		t.Fatal(e)
	}
}
