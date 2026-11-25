//go:build windows

package main

import (
	"embed"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

//go:embed vendor/tor/windows
var torBundle embed.FS

func prepareTor(dir string) (string, []string, error) {
	base := filepath.Join(dir, "bundle")
	err := fs.WalkDir(torBundle, "vendor/tor/windows", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "vendor/tor/windows")
		rel = strings.TrimPrefix(rel, "/")
		dest := filepath.Join(base, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, e := torBundle.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, b, 0600)
	})
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(base, "tor", "tor.exe"), []string{"--GeoIPFile", filepath.Join(base, "data", "geoip"), "--GeoIPv6File", filepath.Join(base, "data", "geoip6")}, nil
}
func hideProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func openUI(url string) error {
	// An app-style Edge window, if available. The browser is UI only, not routed via Tor.
	for _, key := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		if base := os.Getenv(key); base != "" {
			p := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
			if _, err := os.Stat(p); err == nil {
				if err = exec.Command(p, "--app="+url).Start(); err == nil {
					return nil
				}
			}
		}
	}
	return exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe"), "url.dll,FileProtocolHandler", url).Start()
}
func fatal(err error) {
	user32 := syscall.NewLazyDLL("user32.dll")
	msg := user32.NewProc("MessageBoxW")
	text, _ := syscall.UTF16PtrFromString(err.Error())
	title, _ := syscall.UTF16PtrFromString("DarkusSurf")
	msg.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
