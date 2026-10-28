package auth

import (
	"sync"
	"time"
)

const limiterMaxEntries = 10000

type Limiter struct {
	max    int
	window time.Duration

	mu        sync.Mutex
	entries   map[string]*window
	lastSweep time.Time
}

type window struct {
	hits  int
	start time.Time
}

func NewLimiter(max int, per time.Duration) *Limiter {
	return &Limiter{max: max, window: per, entries: map[string]*window{}}
}

func (l *Limiter) Blocked(key string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	e := l.entries[key]
	if e == nil || now.Sub(e.start) >= l.window || e.hits < l.max {
		return time.Time{}, false
	}
	return e.start.Add(l.window), true
}

func (l *Limiter) Hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hitLocked(key, time.Now())
}

func (l *Limiter) Allow(key string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	e := l.entries[key]
	if e != nil && now.Sub(e.start) < l.window && e.hits >= l.max {
		return e.start.Add(l.window), false
	}
	l.hitLocked(key, now)
	return time.Time{}, true
}

func (l *Limiter) hitLocked(key string, now time.Time) {
	e := l.entries[key]
	if e == nil || now.Sub(e.start) >= l.window {
		if e == nil && len(l.entries) >= limiterMaxEntries {
			l.entries = map[string]*window{}
		}
		e = &window{start: now}
		l.entries[key] = e
	}
	e.hits++
}

func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, e := range l.entries {
		if now.Sub(e.start) >= l.window {
			delete(l.entries, k)
		}
	}
}
