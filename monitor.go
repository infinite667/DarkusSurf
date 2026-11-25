package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Circuit struct {
	ID   string
	Hops []string
}
type Monitor struct {
	Read, Written, Down, Up uint64
	Circuits                []Circuit
	Updated                 int64
	Available               bool
}

func parseMonitor(lines []string) (Monitor, error) {
	var m Monitor
	var haveRead, haveWritten bool
	for _, line := range lines {
		if strings.HasPrefix(line, "traffic/read=") {
			v, e := strconv.ParseUint(strings.TrimPrefix(line, "traffic/read="), 10, 64)
			if e == nil {
				m.Read = v
				haveRead = true
			}
			continue
		}
		if strings.HasPrefix(line, "traffic/written=") {
			v, e := strconv.ParseUint(strings.TrimPrefix(line, "traffic/written="), 10, 64)
			if e == nil {
				m.Written = v
				haveWritten = true
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "BUILT" || !strings.Contains(line, "PURPOSE=GENERAL") {
			continue
		}
		if _, e := strconv.ParseUint(f[0], 10, 64); e != nil {
			continue
		}
		c := Circuit{ID: f[0]}
		for _, h := range strings.Split(f[2], ",") {
			if len(h) < 41 || h[0] != '$' {
				continue
			}
			if i := strings.IndexAny(h, "~="); i >= 0 && i+1 < len(h) {
				h = h[i+1:]
			} else {
				h = h[1:9]
			}
			if len(h) > 24 {
				h = h[:24]
			}
			c.Hops = append(c.Hops, h)
		}
		if len(c.Hops) > 0 && len(m.Circuits) < 12 {
			m.Circuits = append(m.Circuits, c)
		}
	}
	if !haveRead || !haveWritten {
		return m, errors.New("Tor traffic counters unavailable")
	}
	m.Available = true
	return m, nil
}
func (m *Manager) sample(g uint64, addr string, auth []byte, previous Monitor) Monitor {
	lines, e := controlCommand(addr, auth, "GETINFO traffic/read traffic/written circuit-status")
	if e != nil {
		return previous
	}
	v, e := parseMonitor(lines)
	if e != nil {
		return previous
	}
	now := time.Now().UnixMilli()
	v.Updated = now
	if previous.Available && now > previous.Updated && v.Read >= previous.Read && v.Written >= previous.Written {
		elapsed := float64(now-previous.Updated) / 1000
		v.Down = uint64(float64(v.Read-previous.Read) / elapsed)
		v.Up = uint64(float64(v.Written-previous.Written) / elapsed)
	}
	m.mu.Lock()
	if m.generation == g && m.s.State == "connected" {
		m.s.Monitor = v
	}
	m.mu.Unlock()
	return v
}
func formatBytes(n uint64) string {
	v := float64(n)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// Bridge entries are structured input, not arbitrary torrc directives.
var fingerprintRE = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)
var certRE = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)

func parseBridges(raw string) ([]string, error) {
	if len(raw) > 16384 {
		return nil, errors.New("bridge text too large")
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) > 0 && strings.EqualFold(f[0], "Bridge") {
			f = f[1:]
		}
		if len(f) < 2 {
			return nil, errors.New("expected a vanilla or obfs4 bridge line")
		}
		obfs := f[0] == "obfs4"
		i := 0
		if obfs {
			i = 1
		}
		if len(f) < i+2 {
			return nil, errors.New("bridge missing address or fingerprint")
		}
		host, port, e := net.SplitHostPort(f[i])
		n, pe := strconv.Atoi(port)
		if e != nil || pe != nil || n < 1 || n > 65535 || net.ParseIP(host) == nil || !fingerprintRE.MatchString(f[i+1]) {
			return nil, errors.New("invalid bridge address or fingerprint")
		}
		if obfs {
			if len(f) != 5 || !strings.HasPrefix(f[3], "cert=") || !certRE.MatchString(strings.TrimPrefix(f[3], "cert=")) || (f[4] != "iat-mode=0" && f[4] != "iat-mode=1" && f[4] != "iat-mode=2") {
				return nil, errors.New("invalid obfs4 bridge parameters")
			}
		} else if len(f) != 2 {
			return nil, errors.New("unexpected vanilla bridge parameters")
		}
		out = append(out, strings.Join(f, " "))
		if len(out) > 20 {
			return nil, errors.New("maximum 20 bridge entries")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no bridge entries found")
	}
	return out, nil
}
func bridgeArgs(executable string, lines []string) ([]string, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	a := []string{"--UseBridges", "1"}
	need := false
	for _, l := range lines {
		if strings.HasPrefix(l, "obfs4 ") {
			need = true
		}
	}
	if need {
		name := "lyrebird"
		if strings.HasSuffix(strings.ToLower(executable), ".exe") {
			name += ".exe"
		}
		p := filepath.Join(filepath.Dir(executable), "pluggable_transports", name)
		if _, e := os.Stat(p); e != nil {
			return nil, errors.New("obfs4 transport missing from Tor bundle")
		}
		a = append(a, "--ClientTransportPlugin", `obfs4 exec "`+strings.ReplaceAll(p, `\`, `\\`)+`"`)
	}
	for _, l := range lines {
		a = append(a, "--Bridge", l)
	}
	return a, nil
}
func (m *Manager) setBridges(raw string) error {
	var lines []string
	var e error
	if raw != "" {
		lines, e = parseBridges(raw)
		if e != nil {
			return e
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.s.State != "disconnected" && m.s.State != "error" {
		return errors.New("disconnect before changing bridges")
	}
	m.bridges = lines
	return nil
}
func (m *Manager) bridgeCount() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.bridges) }
