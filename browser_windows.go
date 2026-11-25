//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func browserPath(id string) string {
	paths := map[string]string{"firefox": `Mozilla Firefox\firefox.exe`, "chrome": `Google\Chrome\Application\chrome.exe`, "edge": `Microsoft\Edge\Application\msedge.exe`, "chromium": `Chromium\Application\chrome.exe`, "brave": `BraveSoftware\Brave-Browser\Application\brave.exe`}
	rel := paths[id]
	if rel == "" {
		return ""
	}
	name := filepath.Base(rel)
	valid := func(p string) bool {
		if !filepath.IsAbs(p) || !strings.EqualFold(filepath.Base(p), name) {
			return false
		}
		s, e := os.Stat(p)
		return e == nil && s.Mode().IsRegular()
	}
	for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "LOCALAPPDATA"} {
		if base := os.Getenv(key); base != "" {
			for _, prefix := range []string{"", "Programs"} {
				p := filepath.Join(base, prefix, rel)
				if valid(p) {
					return p
				}
			}
		}
	}
	// Chromium and Chrome use the same executable name. Do not infer Chromium
	// from Chrome's registry entry or PATH.
	if id == "chromium" {
		return ""
	}
	// Windows App Paths covers custom install locations for registered browsers.
	reg := filepath.Join(os.Getenv("SystemRoot"), "System32", "reg.exe")
	if !filepath.IsAbs(reg) {
		return ""
	}
	for _, hive := range []string{"HKCU", "HKLM"} {
		for _, view := range []string{"/reg:64", "/reg:32"} {
			key := hive + `\Software\Microsoft\Windows\CurrentVersion\App Paths\` + name
			cmd := exec.Command(reg, "query", key, "/ve", view)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			out, e := cmd.Output()
			if e != nil {
				continue
			}
			for _, line := range strings.Split(string(out), "\n") {
				for _, kind := range []string{"REG_SZ", "REG_EXPAND_SZ"} {
					if i := strings.Index(line, kind); i >= 0 {
						p := strings.Trim(strings.TrimSpace(line[i+len(kind):]), `"`)
						if valid(p) {
							return p
						}
					}
				}
			}
		}
	}
	return ""
}
