package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	bufferSize   = 128
	pingInterval = 20 * time.Second
)

type Peer struct {
	ID      int64
	Name    string
	Host    bool
	Waiting bool
}

type Hub struct {
	mu     sync.Mutex
	rooms  map[string]map[*subscriber]struct{}
	closed chan struct{}
	once   sync.Once
}

type subscriber struct {
	Peer
	ch     chan []byte
	kicked chan struct{}
	final  []byte
	gone   bool
}

func NewHub() *Hub {
	return &Hub{rooms: map[string]map[*subscriber]struct{}{}, closed: make(chan struct{})}
}

func (h *Hub) Close() { h.once.Do(func() { close(h.closed) }) }

func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func (h *Hub) Send(room string, v any) {
	h.sendWhere(room, v, func(s *subscriber) bool { return !s.Waiting })
}

func (h *Hub) SendHosts(room string, v any) {
	h.sendWhere(room, v, func(s *subscriber) bool { return s.Host && !s.Waiting })
}

func (h *Hub) SendPeer(room string, id int64, v any) {
	h.sendWhere(room, v, func(s *subscriber) bool { return s.ID == id })
}

func (h *Hub) sendWhere(room string, v any, match func(*subscriber) bool) {
	b := encode(v)
	if b == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendLocked(room, b, match)
}

func (h *Hub) sendLocked(room string, b []byte, match func(*subscriber) bool) {
	for s := range h.rooms[room] {
		if s.gone || !match(s) {
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

func (h *Hub) Kick(room string, id int64, last any) {
	h.endWhere(room, last, func(s *subscriber) bool { return s.ID == id })
}

func (h *Hub) CloseRoom(room string, last any) {
	h.endWhere(room, last, func(*subscriber) bool { return true })
}

func (h *Hub) endWhere(room string, last any, match func(*subscriber) bool) {
	b := encode(last)
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.rooms[room] {
		if s.gone || !match(s) {
			continue
		}
		s.gone = true
		s.final = b
		close(s.kicked)
	}
}

func (h *Hub) Here(room string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hereLocked(room)
}

func (h *Hub) HereIDs(room string) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[int64]bool{}
	out := []int64{}
	for s := range h.rooms[room] {
		if !s.Waiting && !s.gone && !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s.ID)
		}
	}
	return out
}

func (h *Hub) HostHere(room string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hostHereLocked(room)
}

func (h *Hub) hostHereLocked(room string) bool {
	for s := range h.rooms[room] {
		if s.Host && !s.Waiting && !s.gone {
			return true
		}
	}
	return false
}

func (h *Hub) hereLocked(room string) []string {
	seen := map[string]bool{}
	out := []string{}
	for s := range h.rooms[room] {
		if !s.Waiting && !s.gone && !seen[s.Name] {
			seen[s.Name] = true
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (h *Hub) presenceLocked(room string) {
	here := encode(map[string]any{"t": "here", "names": h.hereLocked(room)})
	h.sendLocked(room, here, func(s *subscriber) bool { return !s.Waiting })
	host := encode(map[string]any{"t": "host", "online": h.hostHereLocked(room)})
	h.sendLocked(room, host, func(s *subscriber) bool { return s.Waiting })
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, room string, p Peer) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is not supported by this connection.", http.StatusInternalServerError)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	s := &subscriber{Peer: p, ch: make(chan []byte, bufferSize), kicked: make(chan struct{})}
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
			h.mu.Lock()
			final := s.final
			h.mu.Unlock()
			if final != nil {
				write("data: %s\n\n", final)
			}
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
