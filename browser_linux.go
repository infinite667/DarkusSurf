//go:build linux

package main

import (
	"os/exec"
	"path/filepath"
)

func browserPath(id string) string {
	names := map[string][]string{"firefox": {"firefox", "firefox-esr"}, "chrome": {"google-chrome-stable", "google-chrome"}, "edge": {"microsoft-edge-stable", "microsoft-edge"}, "chromium": {"chromium", "chromium-browser"}, "brave": {"brave-browser", "brave-browser-stable"}}
	for _, n := range names[id] {
		if p, e := exec.LookPath(n); e == nil && filepath.IsAbs(p) {
			return p
		}
	}
	return ""
}
