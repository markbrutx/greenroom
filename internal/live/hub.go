package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	bufferSize   = 128
	pingInterval = 20 * time.Second
)

type Hub struct {
	mu     sync.Mutex
	rooms  map[string]map[*subscriber]struct{}
	closed chan struct{}
	once   sync.Once
}

type subscriber struct {
	name   string
	ch     chan []byte
	kicked chan struct{}
	gone   bool
}

func NewHub() *Hub {
	return &Hub{rooms: map[string]map[*subscriber]struct{}{}, closed: make(chan struct{})}
}

func (h *Hub) Close() { h.once.Do(func() { close(h.closed) }) }

func (h *Hub) Send(room string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendLocked(room, b)
}

func (h *Hub) SendPrefix(prefix string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for room := range h.rooms {
		if strings.HasPrefix(room, prefix) {
			h.sendLocked(room, b)
		}
	}
}

func (h *Hub) sendLocked(room string, b []byte) {
	for s := range h.rooms[room] {
		if s.gone {
			continue
		}
		select {
		case s.ch <- b:
		default:
			s.gone = true
			close(s.kicked)
		}
	}
}

func (h *Hub) Here(room string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hereLocked(room)
}

func (h *Hub) hereLocked(room string) []string {
	seen := map[string]bool{}
	out := []string{}
	for s := range h.rooms[room] {
		if !seen[s.name] {
			seen[s.name] = true
			out = append(out, s.name)
		}
	}
	sort.Strings(out)
	return out
}

func (h *Hub) Online() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, subs := range h.rooms {
		n += len(subs)
	}
	return n
}

func (h *Hub) presenceLocked(room string) {
	b, _ := json.Marshal(map[string]any{"t": "here", "names": h.hereLocked(room)})
	h.sendLocked(room, b)
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, room, name string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is not supported by this connection.", http.StatusInternalServerError)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	s := &subscriber{name: name, ch: make(chan []byte, bufferSize), kicked: make(chan struct{})}
	h.mu.Lock()
	if h.rooms[room] == nil {
		h.rooms[room] = map[*subscriber]struct{}{}
	}
	h.rooms[room][s] = struct{}{}
	h.presenceLocked(room)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.rooms[room], s)
		if len(h.rooms[room]) == 0 {
			delete(h.rooms, room)
		} else {
			h.presenceLocked(room)
		}
		h.mu.Unlock()
	}()

	write := func(format string, args ...any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !write("retry: 1500\n\n") {
		return
	}
	tick := time.NewTicker(pingInterval)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.closed:
			return
		case <-s.kicked:
			return
		case b := <-s.ch:
			if !write("data: %s\n\n", b) {
				return
			}
		case <-tick.C:
			if !write(": ping\n\n") {
				return
			}
		}
	}
}
