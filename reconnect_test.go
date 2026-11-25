package main

import (
	"testing"
	"time"
)

func TestRetryPolicy(t *testing.T) {
	var p retryPolicy
	now := time.Unix(1, 0)
	p.request()
	if p.tick("error", now) {
		t.Fatal("disabled retry")
	}
	p.enabled = true
	for _, d := range []time.Duration{5, 10, 20} {
		if p.tick("error", now) {
			t.Fatal("immediate retry")
		}
		if p.tick("error", now.Add(d*time.Second-time.Millisecond)) {
			t.Fatal("early retry")
		}
		now = now.Add(d * time.Second)
		if !p.tick("error", now) {
			t.Fatal("missing retry")
		}
		p.tick("starting", now)
	}
	if p.tick("error", now.Add(time.Hour)) {
		t.Fatal("unbounded retry")
	}
	p.tick("connected", now)
	if p.attempts != 0 {
		t.Fatal("not reset")
	}
	p.stop()
	p.tick("error", now)
	if p.tick("error", now.Add(time.Hour)) {
		t.Fatal("retries after stop")
	}
}
