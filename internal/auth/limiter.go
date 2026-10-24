package auth

import (
	"sync"
	"time"
)

const limiterMaxEntries = 10000

type Limiter struct {
	max   int
	block time.Duration

	mu        sync.Mutex
	entries   map[string]*attempts
	lastSweep time.Time
}

type attempts struct {
	fails int
	first time.Time
	until time.Time
}

func NewLimiter(max int, block time.Duration) *Limiter {
	return &Limiter{max: max, block: block, entries: map[string]*attempts{}}
}

func (l *Limiter) Blocked(ip string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	e := l.entries[ip]
	if e != nil && e.until.After(now) {
		return e.until, true
	}
	return time.Time{}, false
}

func (l *Limiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e := l.entries[ip]
	if e == nil || l.expired(e, now) {
		if len(l.entries) >= limiterMaxEntries {
			l.entries = map[string]*attempts{}
		}
		e = &attempts{first: now}
		l.entries[ip] = e
	}
	e.fails++
	if e.fails >= l.max {
		e.until = now.Add(l.block)
	}
}

func (l *Limiter) Reset(ip string) {
	l.mu.Lock()
	delete(l.entries, ip)
	l.mu.Unlock()
}

func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *Limiter) expired(e *attempts, now time.Time) bool {
	if !e.until.IsZero() {
		return !e.until.After(now)
	}
	return now.Sub(e.first) > l.block
}

func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for ip, e := range l.entries {
		if l.expired(e, now) {
			delete(l.entries, ip)
		}
	}
}
