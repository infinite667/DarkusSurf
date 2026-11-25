//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func prepareTor(dir string) (string, []string, error) {
	exe, _ := os.Executable()
	base := filepath.Join(filepath.Dir(exe), "vendor", "tor", "linux")
	p := filepath.Join(base, "tor", "tor")
	if s, e := os.Stat(p); e == nil && s.Mode().IsRegular() {
		return p, []string{"--GeoIPFile", filepath.Join(base, "data", "geoip"), "--GeoIPv6File", filepath.Join(base, "data", "geoip6")}, nil
	}
	p, e := exec.LookPath("tor")
	return p, nil, e
}
func hideProcess(c *exec.Cmd) {}
func openUI(url string) error { return exec.Command("xdg-open", url).Start() }
func fatal(err error)         { _, _ = os.Stderr.WriteString("DarkusSurf: " + err.Error() + "\n") }
