package retro

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/markbrutx/greenroom/internal/auth"
	"github.com/markbrutx/greenroom/internal/live"
	"github.com/markbrutx/greenroom/internal/web"
)

type State struct {
	Code       string   `json:"code"`
	Date       string   `json:"date"`
	Warmup     string   `json:"warmup"`
	Cols       []string `json:"cols"`
	Cards      []Card   `json:"cards"`
	Me         string   `json:"me"`
	MeID       int64    `json:"meId"`
	Host       bool     `json:"host"`
	Open       bool     `json:"open"`
	Members    []Member `json:"members"`
	Pending    []Member `json:"pending,omitempty"`
	HostSecret string   `json:"hostSecret,omitempty"`
	Here       []string `json:"here"`
	Emojis     []string `json:"emojis"`
	TTL        string   `json:"ttl"`
	Welcome    bool     `json:"welcome,omitempty"`
	created    int64
}

type roomHandler func(w http.ResponseWriter, r *http.Request, room *Room, m *Member)

func fail(w http.ResponseWriter, status int, msg string) { http.Error(w, msg, status) }

func (s *Service) api(w http.ResponseWriter, r *http.Request, code string, h roomHandler) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(auth.APIHeader) != "1" {
		fail(w, http.StatusForbidden, "Missing the "+auth.APIHeader+" request header.")
		return
	}
	if _, blocked := s.limits.Lookup.Blocked(s.ip(r)); blocked {
		fail(w, http.StatusTooManyRequests, "Too many unknown room codes from your network.")
		return
	}
	room, err := s.repo.RoomByCode(r.Context(), code)
	if errors.Is(err, ErrNotFound) {
		s.limits.Lookup.Hit(s.ip(r))
		fail(w, http.StatusNotFound, "This room was deleted.")
		return
	}
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	id, ok := s.auth.Member(r, code)
	if !ok {
		fail(w, http.StatusUnauthorized, "Join the room first.")
		return
	}
	m, err := s.repo.Member(r.Context(), room.ID, id)
	if errors.Is(err, ErrNotFound) {
		fail(w, http.StatusUnauthorized, "Join the room first.")
		return
	}
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	if m.Status != Admitted {
		fail(w, http.StatusForbidden, "You are not in this room.")
		return
	}
	h(w, r, room, m)
}

func (s *Service) load(r *http.Request, room *Room, m *Member) (*State, error) {
	ctx := r.Context()
	cards, err := s.repo.Cards(ctx, room.ID)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.Members(ctx, room.ID, Admitted)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		s.locks.Decorate(room.ID, &cards[i])
	}
	st := &State{
		Code: room.Code, Date: s.fullDate(room.CreatedAt), Warmup: room.Warmup, Cols: room.Cols, Cards: cards,
		Me: m.Name, MeID: m.ID, Host: m.Host, Open: room.Open, Members: members,
		Here: s.app.Hub.Here(topic(room.Code)), Emojis: Emojis, TTL: s.ttlText(), created: room.CreatedAt,
	}
	if m.Host {
		st.HostSecret = room.HostSecret
		if st.Pending, err = s.repo.Members(ctx, room.ID, Pending); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (s *Service) room(w http.ResponseWriter, r *http.Request, room *Room, m *Member) {
	st, err := s.load(r, room, m)
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	st.Welcome = m.Host && r.URL.Query().Get("welcome") == "1"
	s.repo.Touch(r.Context(), room.ID)
	s.repo.Seen(r.Context(), m.ID)
	s.app.Render(w, r, s.roomT, web.View{Title: "Retro " + room.Code, Data: st})
}

func (s *Service) stateJSON(w http.ResponseWriter, r *http.Request, room *Room, m *Member) {
	st, err := s.load(r, room, m)
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	writeJSON(w, st)
}

func (s *Service) events(w http.ResponseWriter, r *http.Request, code string) {
	room, err := s.repo.RoomByCode(r.Context(), code)
	if errors.Is(err, ErrNotFound) {
		s.limits.Lookup.Hit(s.ip(r))
		fail(w, http.StatusNotFound, "This room was deleted.")
		return
	}
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	id, ok := s.auth.Member(r, code)
	if !ok {
		fail(w, http.StatusUnauthorized, "Join the room first.")
		return
	}
	m, err := s.repo.Member(r.Context(), room.ID, id)
	if err != nil || (m.Status != Admitted && m.Status != Pending) {
		fail(w, http.StatusForbidden, "You are not in this room.")
		return
	}
	s.repo.Seen(r.Context(), m.ID)
	s.app.Hub.Serve(w, r, topic(code), live.Peer{ID: m.ID, Name: m.Name, Host: m.Host, Waiting: m.Status == Pending})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Service) sendMembers(r *http.Request, room *Room) {
	members, err := s.repo.Members(r.Context(), room.ID, Admitted)
	if err != nil {
		s.app.Log.Error("list members", "err", err)
		return
	}
	s.app.Hub.Send(topic(room.Code), map[string]any{"t": "members", "members": members})
}

func (s *Service) sendLobby(r *http.Request, room *Room) {
	pending, err := s.repo.Members(r.Context(), room.ID, Pending)
	if err != nil {
		s.app.Log.Error("list pending", "err", err)
		return
	}
	s.app.Hub.SendHosts(topic(room.Code), map[string]any{"t": "lobby", "pending": pending})
}

type hostReq struct {
	Op   string `json:"op"`
	ID   int64  `json:"id"`
	Open bool   `json:"open"`
}

func (s *Service) hostOp(w http.ResponseWriter, r *http.Request, room *Room, m *Member) {
	if !m.Host {
		fail(w, http.StatusForbidden, "Only the host can do that.")
		return
	}
	var o hostReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&o); err != nil {
		fail(w, http.StatusBadRequest, "The request is not valid JSON.")
		return
	}
	ctx := r.Context()
	hub := s.app.Hub
	tp := topic(room.Code)
	gone := func(err error, msg string) {
		if errors.Is(err, ErrNotFound) {
			fail(w, http.StatusNotFound, msg)
			return
		}
		s.dbFail(w, r, err)
	}
	switch o.Op {
	case "admit":
		x, err := s.repo.SetStatus(ctx, room.ID, o.ID, Pending, Admitted)
		if err != nil {
			gone(err, "This person is no longer waiting.")
			return
		}
		hub.SendPeer(tp, x.ID, map[string]any{"t": "admitted"})
		s.sendMembers(r, room)
		s.sendLobby(r, room)
	case "admitall":
		ids, err := s.repo.AdmitAll(ctx, room.ID)
		if err != nil {
			s.dbFail(w, r, err)
			return
		}
		for _, id := range ids {
			hub.SendPeer(tp, id, map[string]any{"t": "admitted"})
		}
		s.sendMembers(r, room)
		s.sendLobby(r, room)
	case "deny":
		x, err := s.repo.SetStatus(ctx, room.ID, o.ID, Pending, Denied)
		if err != nil {
			gone(err, "This person is no longer waiting.")
			return
		}
		hub.Kick(tp, x.ID, map[string]any{"t": "denied"})
		s.sendLobby(r, room)
	case "remove":
		x, err := s.repo.SetStatus(ctx, room.ID, o.ID, Admitted, Removed)
		if err != nil {
			gone(err, "This person is not in the room.")
			return
		}
		s.app.Log.Info("member removed", "room", room.Code, "member", x.ID)
		hub.Kick(tp, x.ID, map[string]any{"t": "removed"})
		s.sendMembers(r, room)
	case "access":
		if err := s.repo.SetOpen(ctx, room.ID, o.Open); err != nil {
			s.dbFail(w, r, err)
			return
		}
		hub.Send(tp, map[string]any{"t": "access", "open": o.Open})
		if o.Open {
			ids, err := s.repo.AdmitAll(ctx, room.ID)
			if err != nil {
				s.dbFail(w, r, err)
				return
			}
			for _, id := range ids {
				hub.SendPeer(tp, id, map[string]any{"t": "admitted"})
			}
			if len(ids) > 0 {
				s.sendMembers(r, room)
				s.sendLobby(r, room)
			}
		}
	case "delete":
		if err := s.repo.DeleteRoom(ctx, room.ID); err != nil {
			gone(err, "This room was already deleted.")
			return
		}
		s.app.Log.Info("room deleted", "room", room.Code)
		hub.CloseRoom(tp, map[string]any{"t": "deleted", "why": "host"})
		writeJSON(w, map[string]any{})
		return
	default:
		fail(w, http.StatusBadRequest, "Unknown host action "+strconv.Quote(o.Op)+".")
		return
	}
	s.repo.Touch(ctx, room.ID)
	writeJSON(w, map[string]any{})
}

type op struct {
	Op    string `json:"op"`
	ID    int64  `json:"id"`
	Col   string `json:"col"`
	I     int    `json:"i"`
	Text  string `json:"text"`
	Seq   int64  `json:"seq"`
	Final bool   `json:"final"`
	Tmp   string `json:"tmp"`
	Emoji string `json:"emoji"`
	On    bool   `json:"on"`
}

var tmpPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

func validCol(c string, n int) bool {
	if c == "a" || c == "w" {
		return true
	}
	i, err := strconv.Atoi(c)
	return err == nil && strconv.Itoa(i) == c && i >= 0 && i < n
}

func validEmoji(e string) bool {
	for _, x := range Emojis {
		if x == e {
			return true
		}
	}
	return false
}

func (s *Service) do(w http.ResponseWriter, r *http.Request, x *Room, m *Member) {
	var o op
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&o); err != nil {
		fail(w, http.StatusBadRequest, "The request is not valid JSON.")
		return
	}
	ctx := r.Context()
	u := m.Name
	hub := s.app.Hub
	rm := topic(x.Code)
	id := x.ID
	lockKey := strconv.FormatInt(m.ID, 10)
	sendMeta := func() {
		hub.Send(rm, map[string]any{"t": "meta", "cols": x.Cols, "warmup": x.Warmup})
	}
	cardErr := func(err error) {
		if errors.Is(err, ErrNotFound) {
			fail(w, http.StatusNotFound, "This card was deleted.")
			return
		}
		s.dbFail(w, r, err)
	}
	s.repo.Touch(ctx, id)

	switch o.Op {
	case "add":
		if !validCol(o.Col, len(x.Cols)) {
			fail(w, http.StatusBadRequest, "This retro has no column "+strconv.Quote(o.Col)+".")
			return
		}
		if o.Tmp != "" && !tmpPattern.MatchString(o.Tmp) {
			fail(w, http.StatusBadRequest, "The client card id must be 1 to 40 letters, digits, - or _.")
			return
		}
		c, err := s.repo.AddCard(ctx, id, o.Col, u)
		if err != nil {
			s.dbFail(w, r, err)
			return
		}
		s.locks.Take(cardKey{id, c.ID}, lockKey, u)
		s.locks.Decorate(id, &c)
		hub.Send(rm, map[string]any{"t": "card", "c": c, "tmp": o.Tmp})
		writeJSON(w, map[string]any{"c": c, "tmp": o.Tmp})
		return

	case "text":
		verdict, holder := s.locks.BeginWrite(cardKey{id, o.ID}, lockKey, u, o.Seq, o.Final)
		switch verdict {
		case writeStale:
			writeJSON(w, map[string]any{"stale": true})
			return
		case writeLocked:
			fail(w, http.StatusConflict, holder+" is typing in this card right now.")
			return
		}
		c, err := s.repo.SetText(ctx, id, o.ID, clip(o.Text, maxBody))
		if err != nil {
			s.locks.Release(cardKey{id, o.ID})
			cardErr(err)
			return
		}
		s.locks.Decorate(id, &c)
		hub.Send(rm, map[string]any{"t": "card", "c": c})

	case "del":
		if holder, _ := s.locks.Holder(cardKey{id, o.ID}, lockKey); holder != "" {
			fail(w, http.StatusConflict, holder+" is typing in this card right now.")
			return
		}
		if err := s.repo.DeleteCard(ctx, id, o.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, map[string]any{})
				return
			}
			s.dbFail(w, r, err)
			return
		}
		s.locks.Release(cardKey{id, o.ID})
		hub.Send(rm, map[string]any{"t": "del", "id": o.ID})

	case "owner":
		owner := strings.TrimSpace(o.Text)
		if owner != "" && owner != Everyone {
			members, err := s.repo.Members(ctx, id, Admitted)
			if err != nil {
				s.dbFail(w, r, err)
				return
			}
			known := false
			for _, p := range members {
				if p.Name == owner {
					known = true
				}
			}
			if !known {
				fail(w, http.StatusBadRequest, strconv.Quote(owner)+" is not in this room.")
				return
			}
		}
		c, err := s.repo.SetOwner(ctx, id, o.ID, owner)
		if err != nil {
			cardErr(err)
			return
		}
		s.locks.Decorate(id, &c)
		hub.Send(rm, map[string]any{"t": "card", "c": c})

	case "react":
		if !validEmoji(o.Emoji) {
			fail(w, http.StatusBadRequest, "Reactions are limited to "+strings.Join(Emojis, " ")+".")
			return
		}
		if _, err := s.repo.React(ctx, id, o.ID, u, o.Emoji, o.On); err != nil {
			cardErr(err)
			return
		}
		hub.Send(rm, map[string]any{"t": "react", "id": o.ID, "emoji": o.Emoji, "who": u, "on": o.On})

	case "cols":
		n, err := s.repo.BoardCardCount(ctx, id)
		if err != nil {
			s.dbFail(w, r, err)
			return
		}
		if n > 0 {
			fail(w, http.StatusConflict, "The columns already have cards, so the set stays. Click a column title to rename it.")
			return
		}
		x.Cols = append([]string(nil), s.pool.RandomSet(x.Cols).Columns...)
		if err := s.repo.SetCols(ctx, id, x.Cols); err != nil {
			s.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "col":
		text := clip(strings.TrimSpace(o.Text), maxTitle)
		if o.I < 0 || o.I >= len(x.Cols) {
			fail(w, http.StatusBadRequest, "This retro has no column number "+strconv.Itoa(o.I+1)+".")
			return
		}
		if text == "" {
			fail(w, http.StatusBadRequest, "A column title cannot be empty.")
			return
		}
		x.Cols[o.I] = text
		if err := s.repo.SetCols(ctx, id, x.Cols); err != nil {
			s.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "warmup":
		x.Warmup = s.pool.RandomWarmup(x.Warmup)
		if err := s.repo.SetWarmup(ctx, id, x.Warmup); err != nil {
			s.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "warmuptext":
		text := clip(strings.TrimSpace(o.Text), maxWarmup)
		if text == "" {
			fail(w, http.StatusBadRequest, "The warm-up question cannot be empty.")
			return
		}
		x.Warmup = text
		if err := s.repo.SetWarmup(ctx, id, x.Warmup); err != nil {
			s.dbFail(w, r, err)
			return
		}
		sendMeta()

	default:
		fail(w, http.StatusBadRequest, "Unknown action "+strconv.Quote(o.Op)+".")
		return
	}
	writeJSON(w, map[string]any{})
}
