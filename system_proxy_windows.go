//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var inet = syscall.NewLazyDLL("wininet.dll")

type inetOption struct {
	Kind  uint32
	_     uint32
	Value uintptr
}
type inetList struct {
	Size       uint32
	_          uint32
	Connection uintptr
	Count      uint32
	Error      uint32
	Options    uintptr
}
type proxySettings struct {
	Flags                   uint32
	Server, Bypass, AutoURL string
}
type proxyJournal struct{ Before, Applied proxySettings }
type SystemProxy struct {
	mu    sync.Mutex
	file  string
	relay *HTTPRelay
}

func newSystemProxy(dir string) *SystemProxy {
	return &SystemProxy{file: filepath.Join(dir, "system-proxy-restore.json")}
}
func inetString(p uintptr) string {
	if p == 0 {
		return ""
	}
	defer win(kernel, "GlobalFree", p)
	a := (*[32768]uint16)(unsafe.Pointer(p))
	n := 0
	for n < len(a) && a[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(a[:n])
}
func querySystemProxy() (proxySettings, error) {
	var s proxySettings
	a := []inetOption{{Kind: 1}, {Kind: 2}, {Kind: 3}, {Kind: 4}}
	l := inetList{Size: uint32(unsafe.Sizeof(inetList{})), Count: uint32(len(a)), Options: uintptr(unsafe.Pointer(&a[0]))}
	size := l.Size
	ret, _, e := inet.NewProc("InternetQueryOptionW").Call(0, 75, uintptr(unsafe.Pointer(&l)), uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(a)
	if ret == 0 {
		return s, fmt.Errorf("cannot read Windows proxy settings: %v", e)
	}
	s.Flags = uint32(a[0].Value)
	s.Server = inetString(a[1].Value)
	s.Bypass = inetString(a[2].Value)
	s.AutoURL = inetString(a[3].Value)
	return s, nil
}
func applySystemProxy(s proxySettings) error {
	server, bypass, auto := w(s.Server), w(s.Bypass), w(s.AutoURL)
	a := []inetOption{{Kind: 1, Value: uintptr(s.Flags)}, {Kind: 2, Value: uintptr(unsafe.Pointer(server))}, {Kind: 3, Value: uintptr(unsafe.Pointer(bypass))}, {Kind: 4, Value: uintptr(unsafe.Pointer(auto))}}
	l := inetList{Size: uint32(unsafe.Sizeof(inetList{})), Count: uint32(len(a)), Options: uintptr(unsafe.Pointer(&a[0]))}
	ret, _, e := inet.NewProc("InternetSetOptionW").Call(0, 75, uintptr(unsafe.Pointer(&l)), uintptr(l.Size))
	runtime.KeepAlive(a)
	runtime.KeepAlive(server)
	runtime.KeepAlive(bypass)
	runtime.KeepAlive(auto)
	if ret == 0 {
		return fmt.Errorf("cannot set Windows proxy: %v", e)
	}
	win(inet, "InternetSetOptionW", 0, 39, 0, 0)
	win(inet, "InternetSetOptionW", 0, 37, 0, 0)
	return nil
}
func (p *SystemProxy) restoreLocked() error {
	b, e := os.ReadFile(p.file)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var j proxyJournal
	if e = json.Unmarshal(b, &j); e != nil {
		return errors.New("invalid proxy recovery record; inspect Windows proxy settings manually")
	}
	if !loopbackAddr(j.Applied.Server) || j.Applied.Flags != 2 {
		return errors.New("invalid proxy recovery record")
	}
	cur, e := querySystemProxy()
	if e != nil {
		return e
	}
	if cur == j.Before {
		return os.Remove(p.file)
	}
	if !strings.EqualFold(cur.Server, j.Applied.Server) || (cur.Flags != 2 && cur.Flags != 3) || cur.Bypass != j.Applied.Bypass || cur.AutoURL != j.Applied.AutoURL {
		return errors.New("Windows proxy changed outside DarkusSurf; not overwriting it. Review Windows proxy settings.")
	}
	if e = applySystemProxy(j.Before); e != nil {
		return e
	}
	return os.Remove(p.file)
}
func (p *SystemProxy) Restore() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.restoreLocked()
	if p.relay != nil {
		p.relay.Close()
		p.relay = nil
	}
	return e
}
func (p *SystemProxy) Active() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.relay != nil }
func (p *SystemProxy) Enable(m *Manager) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.relay != nil {
		return errors.New("Windows proxy already enabled")
	}
	if e := p.restoreLocked(); e != nil {
		return e
	}
	before, e := querySystemProxy()
	if e != nil {
		return e
	}
	r, e := newHTTPRelay(m)
	if e != nil {
		return e
	}
	applied := proxySettings{Flags: 2, Server: r.l.Addr().String()}
	b, e := json.Marshal(proxyJournal{before, applied})
	if e != nil {
		r.Close()
		return e
	}
	f, e := os.OpenFile(p.file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		r.Close()
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		r.Close()
		_ = os.Remove(p.file)
		return e
	}
	if e = applySystemProxy(applied); e != nil {
		r.Close()
		_ = p.restoreLocked()
		return e
	}
	cur, e := querySystemProxy()
	if e != nil || cur.Server != applied.Server || cur.Flags&2 == 0 {
		r.Close()
		_ = p.restoreLocked()
		return errors.New("Windows proxy could not be verified")
	}
	p.relay = r
	return nil
}
