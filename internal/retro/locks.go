package retro

import (
	"strconv"
	"sync"
	"time"
)

const (
	lockTTL   = 5 * time.Second
	seqMaxAge = 30 * time.Minute
)

type lockEntry struct {
	who   string
	until time.Time
}

type seqEntry struct {
	n    int64
	seen time.Time
}

type Locks struct {
	mu    sync.Mutex
	locks map[int64]lockEntry
	seqs  map[string]seqEntry
	swept time.Time
}

func NewLocks() *Locks {
	return &Locks{locks: map[int64]lockEntry{}, seqs: map[string]seqEntry{}}
}

func (l *Locks) Holder(card int64, me string) (string, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.holderLocked(card, me, time.Now())
}

func (l *Locks) holderLocked(card int64, me string, now time.Time) (string, time.Duration) {
	e, ok := l.locks[card]
	if !ok || e.who == me {
		return "", 0
	}
	left := e.until.Sub(now)
	if left <= 0 {
		return "", 0
	}
	return e.who, left
}

func (l *Locks) Take(card int64, who string) {
	l.mu.Lock()
	l.locks[card] = lockEntry{who, time.Now().Add(lockTTL)}
	l.mu.Unlock()
}

func (l *Locks) Release(card int64) {
	l.mu.Lock()
	delete(l.locks, card)
	l.mu.Unlock()
}

type writeVerdict int

const (
	writeOK writeVerdict = iota
	writeStale
	writeLocked
)

func (l *Locks) BeginWrite(card int64, who string, seq int64, final bool) (writeVerdict, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweepLocked(now)
	key := strconv.FormatInt(card, 10) + "\x00" + who
	if seq <= l.seqs[key].n {
		return writeStale, ""
	}
	if holder, _ := l.holderLocked(card, who, now); holder != "" {
		return writeLocked, holder
	}
	l.seqs[key] = seqEntry{seq, now}
	if final {
		delete(l.locks, card)
	} else {
		l.locks[card] = lockEntry{who, now.Add(lockTTL)}
	}
	return writeOK, ""
}

func (l *Locks) Decorate(c *Card) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.locks[c.ID]
	if !ok {
		return
	}
	if left := time.Until(e.until); left > 0 {
		c.Lock, c.LockMs = e.who, left.Milliseconds()
	}
}

func (l *Locks) sweepLocked(now time.Time) {
	if now.Sub(l.swept) < time.Minute {
		return
	}
	l.swept = now
	for id, e := range l.locks {
		if now.After(e.until) {
			delete(l.locks, id)
		}
	}
	for k, e := range l.seqs {
		if now.Sub(e.seen) > seqMaxAge {
			delete(l.seqs, k)
		}
	}
}
