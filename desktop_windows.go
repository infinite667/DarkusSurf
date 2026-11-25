//go:build windows

package main

// Native Windows UI using Win32/GDI. No web server, browser, WebView or CGo.
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var user = syscall.NewLazyDLL("user32.dll")
var gdi = syscall.NewLazyDLL("gdi32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var shell = syscall.NewLazyDLL("shell32.dll")

//go:uintptrescapes
func win(d *syscall.LazyDLL, name string, args ...uintptr) uintptr {
	r, _, _ := d.NewProc(name).Call(args...)
	return r
}
func w(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return p
}
func arg(n int32) uintptr        { return uintptr(n) }
func rgb(r, g, b uint32) uintptr { return uintptr(r | g<<8 | b<<16) }

var bg = rgb(12, 10, 18)
var panel = rgb(23, 19, 32)
var line = rgb(48, 36, 65)
var accent = rgb(124, 58, 237)
var violet = rgb(198, 165, 255)
var ink = rgb(243, 239, 250)
var muted = rgb(172, 160, 193)
var green = rgb(125, 224, 174)

type rect struct{ L, T, R, B int32 }
type point struct{ X, Y int32 }
type message struct {
	HWND           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type paintStruct struct {
	DC              uintptr
	Erase           int32
	Paint           rect
	Restore, Update int32
	Reserved        [32]byte
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Class                        *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	HWND                uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Timeout             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	Balloon             uintptr
}

const trayMessage = 0x8001
const restoreMessage = 0x8002
const className = "DarkusSurfNativeWindowV03"

type hit struct {
	r       rect
	label   string
	run     func()
	enabled bool
}
type desktop struct {
	m                          *Manager
	router                     *Router
	hwnd, icon, instance       uintptr
	fonts                      [4]uintptr
	tray, closing, busy, dirty bool
	page, notice               string
	prefs                      Preferences
	browsers                   []Browser
	hits                       []hit
	focus                      int
	events                     chan func()
	workers                    sync.WaitGroup
	retry                      retryPolicy
	lastVerified               int64
	taskbar                    uint32
	sys                        *SystemProxy
	rotation                   bool
	interval                   int
	nextRefresh                time.Time
}

var desk *desktop

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mutex, _, e := kernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(w(`Local\DarkusSurf.Native.v03`))))
	if mutex == 0 {
		fatal(fmt.Errorf("single-instance check failed: %v", e))
		return
	}
	defer win(kernel, "CloseHandle", mutex)
	if e == syscall.Errno(183) {
		h := win(user, "FindWindowW", uintptr(unsafe.Pointer(w(className))), 0)
		if h != 0 {
			win(user, "PostMessageW", h, restoreMessage, 0, 0)
		} else {
			fatal(fmt.Errorf("DarkusSurf is already starting or running"))
		}
		return
	}
	root, err := os.MkdirTemp("", "DarkusSurf-")
	if err != nil {
		fatal(err)
		return
	}
	defer os.RemoveAll(root)
	m := NewManager(root)
	defer m.Close()
	cfg, err := os.UserConfigDir()
	if err != nil {
		fatal(err)
		return
	}
	router, err := newRouter(root, filepath.Join(cfg, "DarkusSurf"), m)
	if err != nil {
		fatal(err)
		return
	}
	defer router.Close()
	m.router = router
	d := &desktop{m: m, router: router, page: "Dashboard", prefs: router.getPrefs(), focus: -1, events: make(chan func(), 32)}
	d.sys = newSystemProxy(filepath.Join(cfg, "DarkusSurf"))
	d.interval = 30
	if e := d.sys.Restore(); e != nil {
		d.notice = "Proxy recovery: " + e.Error()
		win(user, "MessageBoxW", 0, uintptr(unsafe.Pointer(w(e.Error()))), uintptr(unsafe.Pointer(w("DarkusSurf proxy recovery"))), 0x30)
	}
	defer d.sys.Restore()
	desk = d
	d.retry.enabled = d.prefs.AutoReconnect
	d.instance = win(kernel, "GetModuleHandleW", 0)
	d.icon = win(user, "LoadIconW", d.instance, 1)
	if d.icon == 0 {
		d.icon = win(user, "LoadIconW", 0, 32512)
	}
	d.taskbar = uint32(win(user, "RegisterWindowMessageW", uintptr(unsafe.Pointer(w("TaskbarCreated")))))
	wc := windowClass{Style: 3, Proc: syscall.NewCallback(wndProc), Instance: d.instance, Icon: d.icon, SmallIcon: d.icon, Cursor: win(user, "LoadCursorW", 0, 32512), Class: w(className)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if win(user, "RegisterClassExW", uintptr(unsafe.Pointer(&wc))) == 0 {
		fatal(fmt.Errorf("could not register native window"))
		return
	}
	// OS title bar is intentionally retained for keyboard accessibility and predictable window behavior.
	const style = 0x00CA0000 // caption, system menu, minimize box, fixed size
	r := rect{0, 0, 980, 650}
	win(user, "AdjustWindowRectEx", uintptr(unsafe.Pointer(&r)), style, 0, 0)
	outerW, outerH := r.R-r.L, r.B-r.T
	if d.prefs.Compact {
		outerW = outerW * 85 / 100
		outerH = outerH * 85 / 100
	}
	var area rect
	if win(user, "SystemParametersInfoW", 0x0030, 0, uintptr(unsafe.Pointer(&area)), 0) != 0 {
		aw, ah := area.R-area.L-16, area.B-area.T-16
		if aw > 0 && ah > 0 {
			if outerW > aw {
				outerH = outerH * aw / outerW
				outerW = aw
			}
			if outerH > ah {
				outerW = outerW * ah / outerH
				outerH = ah
			}
		}
	}
	for i, sz := range []int32{15, 13, 23, 32} {
		weight := uintptr(400)
		if i >= 2 {
			weight = 650
		}
		d.fonts[i] = win(gdi, "CreateFontW", arg(-sz), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(w("Segoe UI"))))
	}
	defer func() {
		for _, f := range d.fonts {
			if f != 0 {
				win(gdi, "DeleteObject", f)
			}
		}
	}()
	h := win(user, "CreateWindowExW", 0, uintptr(unsafe.Pointer(w(className))), uintptr(unsafe.Pointer(w("DarkusSurf — Privacy. Control. Anonymity."))), style, 0x80000000, 0x80000000, arg(outerW), arg(outerH), 0, 0, d.instance, 0)
	if h == 0 {
		fatal(fmt.Errorf("could not create native window"))
		return
	}
	d.hwnd = h
	// Windows 10/11 dark title bar where supported; OS title bar remains standard.
	dwm := syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	if dwm.Find() == nil {
		on := int32(1)
		dwm.Call(h, 20, uintptr(unsafe.Pointer(&on)), 4)
	}
	d.addTray()
	win(user, "ShowWindow", h, 5)
	win(user, "UpdateWindow", h)
	if win(user, "SetTimer", h, 1, 500, 0) == 0 {
		d.removeTray()
		win(user, "DestroyWindow", h)
		fatal(fmt.Errorf("could not start UI timer"))
		return
	}
	d.scan()
	var msg message
	for {
		ret := int32(win(user, "GetMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0))
		if ret <= 0 {
			if ret < 0 {
				fatal(fmt.Errorf("native window message loop failed"))
			}
			break
		}
		win(user, "TranslateMessage", uintptr(unsafe.Pointer(&msg)))
		win(user, "DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
	}
	d.closing = true
	d.removeTray()
	d.retry.stop()
	d.rotation = false
	m.Stop()
	d.workers.Wait()
}
func (d *desktop) addTray() {
	if d.hwnd == 0 {
		return
	}
	n := notifyIcon{HWND: d.hwnd, ID: 1, Flags: 1 | 2 | 4, Callback: trayMessage, Icon: d.icon}
	n.Size = uint32(unsafe.Sizeof(n))
	copy(n.Tip[:], syscall.StringToUTF16("DarkusSurf — click to restore"))
	d.tray = win(shell, "Shell_NotifyIconW", 0, uintptr(unsafe.Pointer(&n))) != 0
}
func (d *desktop) removeTray() {
	if !d.tray {
		return
	}
	n := notifyIcon{HWND: d.hwnd, ID: 1}
	n.Size = uint32(unsafe.Sizeof(n))
	win(shell, "Shell_NotifyIconW", 2, uintptr(unsafe.Pointer(&n)))
	d.tray = false
}
func (d *desktop) copyText(s string) {
	if s == "" {
		return
	}
	data := syscall.StringToUTF16(strings.ReplaceAll(s, "\x00", ""))
	mem := win(kernel, "GlobalAlloc", 2, uintptr(len(data)*2))
	if mem == 0 {
		d.notice = "Clipboard allocation failed."
		return
	}
	keep := false
	defer func() {
		if !keep {
			win(kernel, "GlobalFree", mem)
		}
	}()
	dest := win(kernel, "GlobalLock", mem)
	if dest == 0 {
		d.notice = "Clipboard unavailable."
		return
	}
	// RtlMoveMemory copies UTF-16 including the terminating NUL to the movable block.
	move := kernel.NewProc("RtlMoveMemory")
	if move.Find() != nil {
		win(kernel, "GlobalUnlock", mem)
		d.notice = "Clipboard copy unavailable."
		return
	}
	move.Call(dest, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	win(kernel, "GlobalUnlock", mem)
	if win(user, "OpenClipboard", d.hwnd) == 0 {
		d.notice = "Clipboard is busy; try again."
		return
	}
	defer win(user, "CloseClipboard")
	if win(user, "EmptyClipboard") == 0 || win(user, "SetClipboardData", 13, mem) == 0 {
		d.notice = "Clipboard copy failed."
		return
	}
	keep = true
	d.notice = "Copied to clipboard."
}
func (d *desktop) restore() {
	win(user, "ShowWindow", d.hwnd, 9)
	win(user, "SetForegroundWindow", d.hwnd)
	d.redraw()
}
func (d *desktop) redraw() {
	if d.hwnd != 0 {
		win(user, "InvalidateRect", d.hwnd, 0, 0)
	}
}
func (d *desktop) exit() {
	if win(user, "MessageBoxW", d.hwnd, uintptr(unsafe.Pointer(w("Close Tor-configured browser windows first. Stop Tor and exit DarkusSurf?"))), uintptr(unsafe.Pointer(w("Exit DarkusSurf"))), 0x24) != 6 {
		return
	}
	d.closing = true
	d.retry.stop()
	d.rotation = false
	if e := d.sys.Restore(); e != nil {
		win(user, "MessageBoxW", d.hwnd, uintptr(unsafe.Pointer(w(e.Error()))), uintptr(unsafe.Pointer(w("Proxy restoration warning"))), 0x30)
	}
	d.m.Stop()
	d.removeTray()
	win(user, "DestroyWindow", d.hwnd)
}
func (d *desktop) async(task func() func()) {
	if d.busy || d.closing {
		return
	}
	d.busy = true
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		done := task()
		d.events <- func() {
			d.busy = false
			if done != nil {
				done()
			}
		}
	}()
	d.redraw()
}
func (d *desktop) action(label string, fn func() error) {
	d.async(func() func() {
		e := fn()
		return func() {
			if e != nil {
				d.notice = e.Error()
			} else {
				d.notice = label
			}
		}
	})
}
func (d *desktop) scan() {
	d.async(func() func() {
		bs := d.router.browsers()
		return func() { d.browsers = bs; d.notice = "Browser detection refreshed." }
	})
}
func (d *desktop) save() {
	p := d.prefs
	p.Selected = map[string]bool{}
	for k, v := range d.prefs.Selected {
		p.Selected[k] = v
	}
	d.async(func() func() {
		e := d.router.save(p)
		return func() {
			if e != nil {
				d.notice = e.Error()
				return
			}
			d.dirty = false
			d.retry.enabled = p.AutoReconnect
			d.notice = "Preferences saved locally."
		}
	})
}
func (d *desktop) connect() { d.retry.request(); d.action("Tor startup requested.", d.m.Start) }
func (d *desktop) disconnect() {
	d.retry.stop()
	d.rotation = false
	d.m.Stop()
	d.notice = "Tor disconnect requested. Close routed browser windows before reconnecting."
	if e := d.sys.Restore(); e != nil {
		d.notice = "Proxy restoration: " + e.Error()
	}
	d.redraw()
}
func (d *desktop) tick() {
	for {
		select {
		case f := <-d.events:
			f()
		default:
			goto done
		}
	}
done:
	if d.closing {
		return
	}
	s := d.m.Snapshot()
	if d.rotation && s.State == "connected" && !d.busy && !s.Checking && !time.Now().Before(d.nextRefresh) {
		d.nextRefresh = time.Now().Add(time.Duration(max(d.interval, 10)) * time.Second)
		d.action("Circuit refresh requested; exit IP may not change.", d.m.NewCircuit)
	}
	if s.State != "connected" {
		d.rotation = false
	}
	if !d.busy && d.retry.tick(s.State, time.Now()) {
		d.action("Reconnecting Tor; launch a fresh browser profile afterward.", d.m.Start)
	}
	if !d.busy && s.State == "connected" && d.router.getPrefs().AutoVerify && !s.Checking && s.Started != d.lastVerified {
		d.lastVerified = s.Started
		d.action("Tor exit verification requested.", d.m.Verify)
	}
	d.redraw()
}
func wndProc(h uintptr, msg uint32, wp, lp uintptr) uintptr {
	d := desk
	if d == nil {
		return win(user, "DefWindowProcW", h, uintptr(msg), wp, lp)
	}
	if d.hwnd == 0 {
		d.hwnd = h
	}
	switch msg {
	case 0x000F:
		d.paint(h)
		return 0 // WM_PAINT
	case 0x0014:
		return 1 // WM_ERASEBKGND
	case 0x0113:
		d.tick()
		return 0
	case 0x0202:
		x, y := int32(int16(lp&65535)), int32(int16((lp>>16)&65535))
		var rc rect
		win(user, "GetClientRect", h, uintptr(unsafe.Pointer(&rc)))
		if rc.R > 0 && rc.B > 0 {
			x = x * 980 / rc.R
			y = y * 650 / rc.B
		}
		for i, b := range d.hits {
			if b.enabled && x >= b.r.L && x < b.r.R && y >= b.r.T && y < b.r.B {
				d.focus = i
				b.run()
				d.redraw()
				break
			}
		}
		return 0
	case 0x0100:
		if wp == 9 {
			back := win(user, "GetKeyState", 16)&0x8000 != 0
			step := 1
			if back {
				step = -1
				if d.focus < 0 {
					d.focus = 0
				}
			}
			if len(d.hits) > 0 {
				for i := 0; i < len(d.hits); i++ {
					d.focus = (d.focus + step + len(d.hits)) % len(d.hits)
					if d.hits[d.focus].enabled {
						break
					}
				}
			}
			d.redraw()
			return 0
		}
		if wp == 13 || wp == 32 {
			if d.focus >= 0 && d.focus < len(d.hits) && d.hits[d.focus].enabled {
				d.hits[d.focus].run()
				d.redraw()
			}
			return 0
		}
	case 0x0010:
		if d.tray && d.router.getPrefs().MinimizeToTray {
			win(user, "ShowWindow", h, 0)
			return 0
		}
		d.exit()
		return 0
	case 0x0112:
		if wp&0xFFF0 == 0xF020 && d.tray && d.router.getPrefs().MinimizeToTray {
			win(user, "ShowWindow", h, 0)
			return 0
		}
	case trayMessage:
		if lp == 0x0202 || lp == 0x0203 || lp == 0x0205 {
			d.restore()
		}
		return 0
	case restoreMessage:
		d.restore()
		return 0
	case 0x0011:
		return 1
	case 0x0016:
		if wp != 0 {
			d.closing = true
			d.rotation = false
			_ = d.sys.Restore()
			d.m.Stop()
			d.removeTray()
			win(user, "DestroyWindow", h)
		}
		return 0
	case 0x0002:
		win(user, "KillTimer", h, 1)
		win(user, "PostQuitMessage", 0)
		return 0
	}
	if msg == d.taskbar && msg != 0 {
		d.tray = false
		d.addTray()
		if !d.tray {
			d.restore()
		}
		return 0
	}
	return win(user, "DefWindowProcW", h, uintptr(msg), wp, lp)
}
func fill(dc uintptr, r rect, c uintptr) {
	b := win(gdi, "CreateSolidBrush", c)
	win(user, "FillRect", dc, uintptr(unsafe.Pointer(&r)), b)
	win(gdi, "DeleteObject", b)
}
func (d *desktop) text(dc uintptr, s string, r rect, c uintptr, font int, flags uintptr) {
	old := win(gdi, "SelectObject", dc, d.fonts[font])
	win(gdi, "SetBkMode", dc, 1)
	win(gdi, "SetTextColor", dc, c)
	win(user, "DrawTextW", dc, uintptr(unsafe.Pointer(w(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags|0x800)
	win(gdi, "SelectObject", dc, old)
}
func (d *desktop) label(dc uintptr, s string, x, y, width int32, c uintptr, font int) {
	d.text(dc, s, rect{x, y, x + width, y + 39}, c, font, 0x20|0x8000)
}
func (d *desktop) paragraph(dc uintptr, s string, r rect, c uintptr) { d.text(dc, s, r, c, 0, 0x10) }
func (d *desktop) button(dc uintptr, r rect, s string, enabled, primary bool, fn func()) {
	i := len(d.hits)
	d.hits = append(d.hits, hit{r, s, fn, enabled})
	c := panel
	if primary && enabled {
		c = accent
	}
	if !enabled {
		c = rgb(20, 17, 27)
	}
	border := line
	if d.focus == i && enabled {
		border = violet
	}
	fill(dc, r, border)
	in := rect{r.L + 1, r.T + 1, r.R - 1, r.B - 1}
	fill(dc, in, c)
	tc := ink
	if !enabled {
		tc = rgb(105, 96, 119)
	}
	d.text(dc, s, in, tc, 0, 0x1|0x4|0x20|0x8000)
}
func (d *desktop) section(dc uintptr, title, sub string) {
	d.label(dc, title, 222, 18, 730, ink, 3)
	d.label(dc, sub, 224, 63, 730, muted, 0)
}
func (d *desktop) card(dc uintptr, r rect, title, value string) {
	fill(dc, r, line)
	fill(dc, rect{r.L + 1, r.T + 1, r.R - 1, r.B - 1}, panel)
	d.label(dc, title, r.L+14, r.T+4, r.R-r.L-28, muted, 1)
	d.label(dc, value, r.L+14, r.T+37, r.R-r.L-28, ink, 2)
}
func (d *desktop) paint(h uintptr) {
	var ps paintStruct
	dc := win(user, "BeginPaint", h, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer win(user, "EndPaint", h, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	win(user, "GetClientRect", h, uintptr(unsafe.Pointer(&rc)))
	if rc.R <= 0 || rc.B <= 0 {
		return
	}
	mem := win(gdi, "CreateCompatibleDC", dc)
	bmp := win(gdi, "CreateCompatibleBitmap", dc, 980, 650)
	if mem == 0 || bmp == 0 {
		if mem != 0 {
			win(gdi, "DeleteDC", mem)
		}
		if bmp != 0 {
			win(gdi, "DeleteObject", bmp)
		}
		d.draw(dc, rc)
		return
	}
	old := win(gdi, "SelectObject", mem, bmp)
	d.draw(mem, rect{0, 0, 980, 650})
	if rc.R == 980 && rc.B == 650 {
		win(gdi, "BitBlt", dc, 0, 0, 980, 650, mem, 0, 0, 0x00CC0020)
	} else {
		win(gdi, "SetStretchBltMode", dc, 4)
		win(gdi, "StretchBlt", dc, 0, 0, arg(rc.R), arg(rc.B), mem, 0, 0, 980, 650, 0x00CC0020)
	}
	win(gdi, "SelectObject", mem, old)
	win(gdi, "DeleteObject", bmp)
	win(gdi, "DeleteDC", mem)
}
func (d *desktop) draw(dc uintptr, rc rect) {
	d.hits = nil
	fill(dc, rc, bg)
	fill(dc, rect{0, 0, 198, 650}, panel)
	fill(dc, rect{197, 0, 198, 650}, line)
	win(user, "DrawIconEx", dc, 68, 22, d.icon, 62, 62, 0, 0, 3)
	d.text(dc, "DARKUSSURF", rect{0, 88, 198, 119}, violet, 2, 0x1|0x20|0x4)
	d.text(dc, "NATIVE EDITION   /   0.4.0", rect{0, 120, 198, 145}, muted, 1, 0x1|0x20|0x4)
	for i, p := range []string{"Dashboard", "Applications", "Network", "Traffic", "Circuits", "Bridges", "Diagnostics", "Settings", "About"} {
		p := p
		y := int32(155 + i*44)
		d.button(dc, rect{14, y, 184, y + 38}, p, true, d.page == p, func() { d.page = p; d.focus = -1 })
	}
	d.button(dc, rect{14, 565, 184, 603}, "Exit DarkusSurf", true, false, d.exit)
	d.label(dc, "OPT-IN PROXY ROUTING", 17, 612, 170, muted, 1)
	s := d.m.Snapshot()
	connected := s.State == "connected"
	idle := s.State == "disconnected" || s.State == "error"
	switch d.page {
	case "Dashboard":
		d.section(dc, "Dashboard", "Privacy. Control. Anonymity.")
		fill(dc, rect{222, 112, 956, 300}, panel)
		d.label(dc, strings.ToUpper(s.State), 244, 116, 650, violet, 2)
		d.paragraph(dc, s.Detail, rect{244, 164, 934, 198}, ink)
		fill(dc, rect{244, 205, 934, 214}, line)
		fill(dc, rect{244, 205, 244 + 690*int32(s.Progress)/100, 214}, accent)
		d.label(dc, fmt.Sprintf("%d%% bootstrap", s.Progress), 244, 220, 300, muted, 1)
		d.button(dc, rect{244, 254, 450, 289}, "Connect to Tor", idle && !d.busy, true, d.connect)
		d.button(dc, rect{463, 254, 669, 289}, "Disconnect / stop retries", (!idle && s.State != "stopping") || (s.State == "error" && d.retry.enabled && d.retry.desired), false, d.disconnect)
		ip := s.IP
		if ip == "" {
			ip = "Not verified"
		}
		duration := "00:00:00"
		if s.Started > 0 {
			sec := int64(time.Since(time.Unix(s.Started, 0)).Seconds())
			if sec < 0 {
				sec = 0
			}
			duration = fmt.Sprintf("%02d:%02d:%02d", sec/3600, (sec/60)%60, sec%60)
		}
		d.card(dc, rect{222, 316, 580, 406}, "Tor exit IP (controller check)", ip)
		d.card(dc, rect{598, 316, 956, 406}, "Session duration", duration)
		d.card(dc, rect{222, 420, 580, 510}, "Local SOCKS5 address", or(s.Proxy, "Not connected"))
		v := "Not verified"
		if s.Checking {
			v = "Checking..."
		} else if s.Verified {
			v = "Tor exit confirmed"
		}
		d.card(dc, rect{598, 420, 956, 510}, "Controller verification", v)
		d.button(dc, rect{222, 526, 454, 563}, "Verify Tor exit", connected && !s.Checking && !d.busy, false, func() { d.action("Verification requested.", d.m.Verify) })
		d.button(dc, rect{468, 526, 700, 563}, "New circuit", connected && !s.Checking && !d.busy, false, func() { d.action("New circuit requested; IP may not change.", d.m.NewCircuit) })
		d.paragraph(dc, "Only configured applications use Tor. Windows proxy mode is opt-in and not a system-wide firewall.", rect{222, 577, 956, 611}, muted)
	case "Applications":
		d.section(dc, "Application routing", "Select, save, connect, then launch a separate browser profile.")
		for i, id := range browserOrder {
			id := id
			y := int32(115 + i*79)
			fill(dc, rect{222, y, 956, y + 68}, panel)
			d.label(dc, browserNames[id], 240, y+2, 295, ink, 2)
			path := ""
			for _, b := range d.browsers {
				if b.ID == id {
					path = b.Path
				}
			}
			detected := path != ""
			sub := "Not detected - selection allowed"
			if detected {
				sub = "Detected - separate profile"
			}
			d.label(dc, sub, 240, y+35, 330, muted, 1)
			checked := d.prefs.Selected[id]
			toggle := "[ ]  Use Tor"
			if checked {
				toggle = "[x]  Use Tor"
			}
			d.button(dc, rect{595, y + 14, 745, y + 53}, toggle, !d.busy, checked, func() { d.prefs.Selected[id] = !d.prefs.Selected[id]; d.dirty = true })
			can := connected && detected && checked && !d.dirty && !d.busy
			d.button(dc, rect{756, y + 14, 942, y + 53}, "Launch via Tor", can, false, func() {
				d.action(browserNames[id]+" launch requested. Check the new window.", func() error { return d.router.launch(id) })
			})
		}
		d.button(dc, rect{222, 519, 418, 559}, "Save selection", !d.busy, true, d.save)
		d.button(dc, rect{432, 519, 625, 559}, "Refresh detection", !d.busy, false, d.scan)
		hint := "Save selections, connect Tor and launch a detected browser."
		if d.dirty {
			hint = "Unsaved changes — save before launching."
		}
		d.paragraph(dc, hint, rect{222, 576, 956, 609}, muted)
	case "Network":
		d.section(dc, "Network and privacy", "Opt-in Windows proxy for compatible HTTP/HTTPS applications.")
		d.card(dc, rect{222, 112, 578, 201}, "Tor SOCKS5", or(s.Proxy, "Not connected"))
		status := "Off"
		if d.sys.Active() {
			status = "On / compatible apps only"
			if !connected {
				status = "On / Tor unavailable"
			}
		}
		d.card(dc, rect{598, 112, 956, 201}, "Windows system proxy", status)
		d.paragraph(dc, "Remote DNS is used by configured SOCKS5 browsers and the local HTTP relay. Unsafe SOCKS requests are rejected by Tor. Browser profiles reduce WebRTC / QUIC bypass risk. These measures do not block all OS DNS, IPv6 or UDP traffic.", rect{222, 220, 956, 310}, ink)
		d.button(dc, rect{222, 320, 560, 362}, "Enable Windows proxy (opt-in)", connected && !d.sys.Active() && !d.busy, true, func() {
			if win(user, "MessageBoxW", d.hwnd, uintptr(unsafe.Pointer(w("Change the current user's Windows proxy for compatible applications? Apps that ignore the proxy can still connect directly. Disabling restores normal network settings. Some existing apps may need restarting."))), uintptr(unsafe.Pointer(w("Enable Windows proxy"))), 0x24) == 6 {
				d.action("Windows proxy enabled; verify in each application.", func() error { return d.sys.Enable(d.m) })
			}
		})
		d.button(dc, rect{574, 320, 956, 362}, "Restore original Windows proxy", !d.busy, false, func() { d.action("Original proxy settings restored.", d.sys.Restore) })
		d.paragraph(dc, "Settings are backed up before changes. Disconnect / exit restores them; after a crash, restart DarkusSurf to recover. Unexpected Tor loss blocks new relay requests; disable and re-enable after reconnecting. No system-wide packet interception is installed.", rect{222, 380, 956, 466}, muted)
		d.button(dc, rect{222, 482, 480, 522}, "Verify Tor controller", connected && !s.Checking && !d.busy, false, func() { d.action("Verification requested.", d.m.Verify) })
		d.button(dc, rect{494, 482, 700, 522}, "Copy SOCKS address", s.Proxy != "", false, func() { d.copyText(s.Proxy) })
		d.paragraph(dc, "Test DNS / IPv6 / WebRTC independently inside each browser. Do not assume an IP check alone proves leak protection.", rect{222, 542, 956, 610}, muted)
	case "Traffic":
		d.section(dc, "Live network monitor", "Tor process traffic only; no browsing destinations or application logs.")
		v := s.Monitor
		fresh := v.Available && time.Now().UnixMilli()-v.Updated < 8000 && connected
		vals := []string{"Unavailable", "Unavailable", "Unavailable", "Unavailable"}
		if fresh {
			vals = []string{formatBytes(v.Down) + "/s", formatBytes(v.Up) + "/s", formatBytes(v.Read), formatBytes(v.Written)}
		}
		d.card(dc, rect{222, 115, 580, 209}, "Download rate", vals[0])
		d.card(dc, rect{598, 115, 956, 209}, "Upload rate", vals[1])
		d.card(dc, rect{222, 225, 580, 319}, "Total Tor bytes received", vals[2])
		d.card(dc, rect{598, 225, 956, 319}, "Total Tor bytes sent", vals[3])
		fill(dc, rect{222, 337, 956, 478}, panel)
		d.label(dc, "Tor traffic activity", 240, 344, 690, ink, 2)
		if fresh {
			for i, n := range []uint64{v.Down, v.Up} {
				y := int32(397 + i*35)
				d.label(dc, []string{"Down", "Up"}[i], 240, y-10, 90, muted, 1)
				fill(dc, rect{330, y, 930, y + 12}, line)
				width := int32(min(float64(n)/1048576, 1) * 600)
				fill(dc, rect{330, y, 330 + width, y + 12}, accent)
			}
		}
		d.paragraph(dc, "Counters come from Tor's control protocol, including Tor protocol overhead. Rates refresh approximately every two seconds. Unrouted network traffic is not measured. Bars use a 1 MiB/s scale.", rect{222, 497, 956, 594}, muted)
	case "Circuits":
		d.section(dc, "Tor circuits", "Live built general-purpose circuits; not a per-site route guarantee.")
		if !s.Monitor.Available || len(s.Monitor.Circuits) == 0 || time.Now().UnixMilli()-s.Monitor.Updated >= 8000 {
			d.paragraph(dc, "No current circuit data. Connect to Tor and wait for a sample.", rect{222, 112, 956, 175}, muted)
		} else {
			for i, c := range s.Monitor.Circuits {
				if i >= 4 {
					break
				}
				y := int32(112 + i*64)
				fill(dc, rect{222, y, 956, y + 58}, panel)
				d.label(dc, "#"+c.ID, 234, y+9, 80, violet, 1)
				d.label(dc, strings.Join(c.Hops, "  >  "), 314, y+9, 632, ink, 0)
			}
		}
		label := "Circuit refresh: OFF"
		if d.rotation {
			label = "Circuit refresh: ON"
		}
		d.button(dc, rect{222, 384, 550, 428}, label, connected && !d.busy, d.rotation, func() {
			d.rotation = !d.rotation
			d.nextRefresh = time.Now().Add(time.Duration(max(d.interval, 10)) * time.Second)
		})
		d.button(dc, rect{565, 384, 615, 428}, "-", !d.busy, false, func() { d.interval = max(3, d.interval-3) })
		d.label(dc, fmt.Sprintf("%ds interval", d.interval), 625, 384, 143, ink, 2)
		d.button(dc, rect{775, 384, 825, 428}, "+", !d.busy, false, func() { d.interval = min(30, d.interval+3) })
		d.button(dc, rect{222, 442, 550, 482}, "Request new circuit now", connected && !d.busy && !s.Checking, false, func() { d.action("New circuit requested; IP may not change.", d.m.NewCircuit) })
		d.paragraph(dc, "Off by default. 3–30 second requested interval; effective request spacing is at least 10 seconds and Tor can rate-limit further. Existing streams continue and the exit IP may remain unchanged. This is not an untraceable mode.", rect{222, 498, 956, 608}, muted)
	case "Bridges":
		d.section(dc, "Tor bridges", "Optional vanilla / obfs4 bridges from a trusted Tor Project source.")
		d.card(dc, rect{222, 112, 956, 210}, "Configured bridges (this app session)", fmt.Sprintf("%d", d.m.bridgeCount()))
		d.paragraph(dc, "Obtain bridge lines from bridges.torproject.org, copy them, then import from clipboard while disconnected. Only structured vanilla and obfs4 lines are accepted. The Windows bundle includes the lyrebird transport.", rect{222, 234, 956, 318}, ink)
		d.button(dc, rect{222, 330, 550, 372}, "Import bridge lines from clipboard", idle && !d.busy, true, func() {
			raw, e := d.readClipboard()
			if e == nil {
				e = d.m.setBridges(raw)
			}
			if e != nil {
				d.notice = e.Error()
			} else {
				d.notice = "Bridges imported. Connect to Tor to use them."
			}
		})
		d.button(dc, rect{564, 330, 956, 372}, "Clear bridges / use normal Tor", idle && !d.busy, false, func() {
			if e := d.m.setBridges(""); e != nil {
				d.notice = e.Error()
			} else {
				d.notice = "Bridges cleared."
			}
		})
		d.paragraph(dc, "Bridge addresses are kept in memory for this application session and are not written to preferences or event logs. Live bridge connectivity depends on the supplied bridge and network.", rect{222, 400, 956, 490}, muted)
	case "Diagnostics":
		d.section(dc, "Diagnostics", "Recent controller events; no browsing destinations recorded.")
		fill(dc, rect{222, 112, 956, 569}, panel)
		logs := s.Logs
		if len(logs) > 17 {
			logs = logs[len(logs)-17:]
		}
		if len(logs) == 0 {
			logs = []string{"No session events."}
		}
		for i, l := range logs {
			d.label(dc, l, 240, 118+int32(i)*25, 697, ink, 1)
		}
		d.button(dc, rect{222, 579, 450, 611}, "Verify Tor exit", connected && !s.Checking && !d.busy, false, func() { d.action("Verification requested.", d.m.Verify) })
		d.button(dc, rect{464, 579, 680, 611}, "Copy session events", len(s.Logs) > 0, false, func() { d.copyText(strings.Join(s.Logs, "\r\n")) })
	case "Settings":
		d.section(dc, "Settings", "Preferences are stored locally. No application telemetry.")
		items := []struct {
			label, desc string
			v           *bool
		}{{"Auto-verify Tor exit", "Check one controller request at each successful connection.", &d.prefs.AutoVerify}, {"Automatic reconnection", "Up to three retries after a Tor failure; old browser profiles are not relaunched.", &d.prefs.AutoReconnect}, {"Minimize / close to tray", "Keep Tor running; use Exit DarkusSurf to stop it.", &d.prefs.MinimizeToTray}, {"Compact window", "Use a smaller window the next time DarkusSurf starts.", &d.prefs.Compact}}
		for i, it := range items {
			it := it
			y := int32(112 + i*96)
			fill(dc, rect{222, y, 956, y + 87}, panel)
			d.label(dc, it.label, 240, y+4, 510, ink, 2)
			d.paragraph(dc, it.desc, rect{240, y + 45, 790, y + 88}, muted)
			t := "Off"
			if *it.v {
				t = "On"
			}
			d.button(dc, rect{813, y + 24, 938, y + 62}, t, !d.busy, *it.v, func() { *it.v = !*it.v; d.dirty = true })
		}
		d.button(dc, rect{222, 506, 430, 547}, "Save preferences", !d.busy, true, d.save)
		if d.dirty {
			d.label(dc, "Unsaved changes", 446, 507, 460, violet, 0)
		}
		d.paragraph(dc, "Settings apply after saving. Auto-reconnect is opt-in. Tray behavior is available only while the system tray icon is registered.", rect{222, 562, 940, 611}, muted)
	case "About":
		d.section(dc, "About DarkusSurf", "v0.4.0 / Windows native preview")
		d.label(dc, "Privacy. Control. Anonymity.", 222, 123, 734, violet, 3)
		d.paragraph(dc, "Independent Tor controller and browser-profile router. Windows native Win32 interface, Go backend, embedded Tor Expert Bundle. No external browser is required for the dashboard.", rect{222, 185, 930, 270}, ink)
		d.paragraph(dc, "Unsigned development preview. Tor bundle signature and native Windows GUI require independent verification. Not affiliated with the Tor Project. Not a VPN, firewall or guarantee of anonymity. For sensitive browsing use official Tor Browser.", rect{222, 285, 930, 395}, muted)
		d.paragraph(dc, "License and upstream notices are provided in the source distribution. The logo is based on the supplied Darkus emblem.", rect{222, 416, 930, 485}, muted)
	}
	fill(dc, rect{198, 620, 980, 650}, line)
	notice := d.notice
	if d.busy {
		notice = "Working..."
	}
	if notice == "" {
		notice = "Ready. Routing is opt-in only."
	}
	d.text(dc, notice, rect{222, 620, 958, 650}, ink, 1, 0x20|0x4|0x8000)
}
func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (d *desktop) readClipboard() (string, error) {
	if win(user, "OpenClipboard", d.hwnd) == 0 {
		return "", errors.New("clipboard is busy")
	}
	defer win(user, "CloseClipboard")
	h := win(user, "GetClipboardData", 13)
	if h == 0 {
		return "", errors.New("clipboard does not contain text")
	}
	size := win(kernel, "GlobalSize", h)
	if size < 2 || size > 32768 {
		return "", errors.New("clipboard text is too large")
	}
	p := win(kernel, "GlobalLock", h)
	if p == 0 {
		return "", errors.New("clipboard unavailable")
	}
	defer win(kernel, "GlobalUnlock", h)
	a := unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(size/2))
	return syscall.UTF16ToString(a), nil
}
