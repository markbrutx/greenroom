package retro

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/markbrutx/moored/internal/auth"
	"github.com/markbrutx/moored/internal/web"
)

type homeData struct {
	Name    string
	Open    bool
	Err     string
	Code    string
	JoinErr string
	Notice  string
	TTL     string
}

type joinData struct {
	Code       string
	Host       string
	Open       bool
	State      string
	Name       string
	Err        string
	HostOnline bool
	Here       int
}

type goneData struct {
	Heading string
	Message string
}

var notices = map[string]string{
	"deleted": "The room was deleted. Its cards are gone.",
	"expired": "The room was deleted after a long time without activity.",
}

func (s *Service) ip(r *http.Request) string { return auth.ClientIP(r, s.app.Cfg.TrustedProxies) }

func (s *Service) home(w http.ResponseWriter, r *http.Request) {
	d := homeData{Name: auth.LastName(r), Notice: notices[r.URL.Query().Get("n")], TTL: s.ttlText()}
	s.renderHome(w, r, d, http.StatusOK)
}

func (s *Service) renderHome(w http.ResponseWriter, r *http.Request, d homeData, status int) {
	d.TTL = s.ttlText()
	s.app.RenderStatus(w, r, s.homeT, web.View{Title: "Retro rooms", Data: d}, status)
}

func (s *Service) gone(w http.ResponseWriter, r *http.Request, status int, heading, msg string) {
	s.app.RenderStatus(w, r, s.goneT, web.View{Title: heading, Data: goneData{Heading: heading, Message: msg}}, status)
}

func (s *Service) tooMany(w http.ResponseWriter, r *http.Request, until time.Time, what string) {
	mins := int(time.Until(until).Minutes()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())+1))
	s.gone(w, r, http.StatusTooManyRequests, "Slow down",
		"Too many "+what+" from your network. Try again in "+strconv.Itoa(mins)+" min.")
}

func (s *Service) dbFail(w http.ResponseWriter, r *http.Request, err error) {
	s.app.Log.Error("database", "err", err, "path", r.URL.Path)
	http.Error(w, "The database did not answer. Try again in a moment.", http.StatusInternalServerError)
}

func (s *Service) findRoom(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("code")
	code, ok := ParseCode(raw)
	if !ok {
		s.renderHome(w, r, homeData{Name: auth.LastName(r), Code: clip(raw, 200),
			JoinErr: "That is not a room code. Codes look like abc-defg-hij."}, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/"+code, http.StatusSeeOther)
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "The form is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	name := auth.CleanName(r.PostForm.Get("name"))
	open := r.PostForm.Get("access") == "open"
	if name == "" {
		s.renderHome(w, r, homeData{Open: open, Err: "Enter your name."}, http.StatusBadRequest)
		return
	}
	if until, ok := s.limits.Create.Allow(s.ip(r)); !ok {
		s.tooMany(w, r, until, "new rooms")
		return
	}
	secret, err := auth.Token(16)
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	set := s.pool.Sets[0]
	var room *Room
	var host *Member
	for tries := 0; tries < 5; tries++ {
		code, cerr := NewCode()
		if cerr != nil {
			s.dbFail(w, r, cerr)
			return
		}
		room, host, err = s.repo.CreateRoom(r.Context(), code, secret, open, s.pool.RandomWarmup(""), append([]string(nil), set.Columns...), name)
		if !errors.Is(err, ErrCodeTaken) {
			break
		}
	}
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	s.app.Log.Info("room created", "room", room.Code, "open", open, "ip", s.ip(r))
	s.auth.SetMember(w, r, room.Code, host.ID)
	auth.RememberName(w, r, name)
	http.Redirect(w, r, "/"+room.Code+"?welcome=1", http.StatusSeeOther)
}

func (s *Service) lookupMiss(w http.ResponseWriter, r *http.Request) {
	code, _, _ := cutPath(r.URL.Path)
	if ValidCode(code) {
		s.limits.Lookup.Hit(s.ip(r))
	}
	s.gone(w, r, http.StatusNotFound, "No room here",
		"There is no room at this address. Rooms are deleted by their host or after "+s.ttlText()+" without activity.")
}

func (s *Service) visit(w http.ResponseWriter, r *http.Request, code string) (*Room, *Member, bool) {
	if until, blocked := s.limits.Lookup.Blocked(s.ip(r)); blocked {
		s.tooMany(w, r, until, "unknown room codes")
		return nil, nil, false
	}
	room, err := s.repo.RoomByCode(r.Context(), code)
	if errors.Is(err, ErrNotFound) {
		s.lookupMiss(w, r)
		return nil, nil, false
	}
	if err != nil {
		s.dbFail(w, r, err)
		return nil, nil, false
	}
	var m *Member
	if id, ok := s.auth.Member(r, code); ok {
		m, err = s.repo.Member(r.Context(), room.ID, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			s.dbFail(w, r, err)
			return nil, nil, false
		}
	}
	return room, m, true
}

func (s *Service) page(w http.ResponseWriter, r *http.Request, code string) {
	room, m, ok := s.visit(w, r, code)
	if !ok {
		return
	}
	if secret := r.URL.Query().Get("host"); secret != "" {
		s.claimHost(w, r, room, secret)
		return
	}
	if m == nil {
		s.renderJoin(w, r, room, joinData{State: "join", Name: auth.LastName(r)}, http.StatusOK)
		return
	}
	switch m.Status {
	case Admitted:
		s.room(w, r, room, m)
	case Pending:
		s.renderJoin(w, r, room, joinData{State: "waiting", Name: m.Name}, http.StatusOK)
	default:
		s.renderJoin(w, r, room, joinData{State: m.Status, Name: m.Name}, http.StatusOK)
	}
}

func (s *Service) claimHost(w http.ResponseWriter, r *http.Request, room *Room, secret string) {
	if subtle.ConstantTimeCompare([]byte(secret), []byte(room.HostSecret)) != 1 {
		s.limits.Lookup.Hit(s.ip(r))
		s.gone(w, r, http.StatusForbidden, "Host link not valid",
			"This host link does not match the room. Ask the host for the room link instead.")
		return
	}
	host, err := s.repo.HostMember(r.Context(), room.ID)
	if err != nil {
		s.dbFail(w, r, err)
		return
	}
	s.repo.Touch(r.Context(), room.ID)
	s.app.Log.Info("host link used", "room", room.Code, "ip", s.ip(r))
	s.auth.SetMember(w, r, room.Code, host.ID)
	http.Redirect(w, r, "/"+room.Code, http.StatusSeeOther)
}

func (s *Service) renderJoin(w http.ResponseWriter, r *http.Request, room *Room, d joinData, status int) {
	d.Code = room.Code
	d.Open = room.Open
	if host, err := s.repo.HostMember(r.Context(), room.ID); err == nil {
		d.Host = host.Name
	}
	d.HostOnline = s.app.Hub.HostHere(topic(room.Code))
	d.Here = len(s.app.Hub.Here(topic(room.Code)))
	title := "Join retro"
	if d.State == "waiting" {
		title = "Waiting to join"
	}
	s.app.RenderStatus(w, r, s.joinT, web.View{Title: title, Data: d}, status)
}

func (s *Service) join(w http.ResponseWriter, r *http.Request, code string) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "The form is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	room, m, ok := s.visit(w, r, code)
	if !ok {
		return
	}
	if m != nil && (m.Status == Admitted || m.Status == Pending) {
		http.Redirect(w, r, "/"+code, http.StatusSeeOther)
		return
	}
	state := "join"
	if m != nil {
		state = m.Status
	}
	name := auth.CleanName(r.PostForm.Get("name"))
	if name == "" {
		s.renderJoin(w, r, room, joinData{State: state, Err: "Enter your name."}, http.StatusBadRequest)
		return
	}
	if until, ok := s.limits.Join.Allow(s.ip(r)); !ok {
		s.tooMany(w, r, until, "join requests")
		return
	}
	status := Pending
	if room.Open && m == nil {
		status = Admitted
	}
	var existing int64
	if m != nil {
		existing = m.ID
	}
	nm, err := s.repo.Join(r.Context(), room.ID, existing, name, status)
	switch {
	case errors.Is(err, ErrNameTaken):
		s.renderJoin(w, r, room, joinData{State: state, Name: name,
			Err: "Someone in this room already goes by " + name + ". Add an initial or a nickname."}, http.StatusConflict)
		return
	case errors.Is(err, ErrFull):
		s.renderJoin(w, r, room, joinData{State: state, Name: name, Err: "This room is full."}, http.StatusConflict)
		return
	case err != nil:
		s.dbFail(w, r, err)
		return
	}
	s.auth.SetMember(w, r, code, nm.ID)
	auth.RememberName(w, r, name)
	if status == Admitted {
		s.app.Log.Info("joined", "room", code, "member", nm.ID)
		s.sendMembers(r, room)
	} else {
		s.app.Log.Info("asked to join", "room", code, "member", nm.ID)
		s.sendLobby(r, room)
	}
	http.Redirect(w, r, "/"+code, http.StatusSeeOther)
}
