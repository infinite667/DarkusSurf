package main

import "time"

// Reconnect is opt-in, bounded, and never relaunches browser profiles.
type retryPolicy struct {
	enabled, desired bool
	attempts         int
	due              time.Time
}

func (p *retryPolicy) request() { p.desired = true; p.attempts = 0; p.due = time.Time{} }
func (p *retryPolicy) stop()    { p.desired = false; p.due = time.Time{} }
func (p *retryPolicy) tick(state string, now time.Time) bool {
	if !p.enabled || !p.desired {
		p.due = time.Time{}
		return false
	}
	if state == "connected" {
		p.attempts = 0
		p.due = time.Time{}
		return false
	}
	if state != "error" {
		p.due = time.Time{}
		return false
	}
	if p.attempts >= 3 {
		return false
	}
	if p.due.IsZero() {
		p.due = now.Add(time.Duration(5<<p.attempts) * time.Second)
		return false
	}
	if now.Before(p.due) {
		return false
	}
	p.attempts++
	p.due = time.Time{}
	return true
}
