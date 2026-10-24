package retro

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/markbrutx/greenroom/internal/store"
	"github.com/markbrutx/greenroom/internal/web"
)

type listRow struct {
	ID    int64
	Date  string
	Title string
	Cols  string
	Cards int
}

type listData struct {
	Rows []listRow
	Sets []string
	Open int
}

type State struct {
	ID      int64    `json:"id"`
	Date    string   `json:"date"`
	Title   string   `json:"title"`
	Warmup  string   `json:"warmup"`
	Cols    []string `json:"cols"`
	Hidden  bool     `json:"hidden"`
	TimerOn bool     `json:"timerOn"`
	TimerMs int64    `json:"timerMs"`
	Cards   []Card   `json:"cards"`
	Carry   []Carry  `json:"carry"`
	Me      string   `json:"me"`
	Names   []string `json:"names"`
	Here    []string `json:"here"`
	Emojis  []string `json:"emojis"`
	Sets    []string `json:"sets"`
	created int64
}

type roomData struct {
	State *State
	Title string
	Date  string
}

type actionRow struct {
	ID      int64
	Body    string
	Owner   string
	RetroID int64
	From    string
	Retro   string
}

func (t *Tool) fail(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

func (t *Tool) dbFail(w http.ResponseWriter, r *http.Request, err error) {
	t.app.Log.Error("retro database", "err", err, "path", r.URL.Path)
	t.fail(w, http.StatusInternalServerError, "The database did not answer. Try again in a moment.")
}

func (t *Tool) list(w http.ResponseWriter, r *http.Request) {
	rows, err := t.repo.List(r.Context(), listLimit)
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	open, _, err := t.repo.Stats(r.Context())
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	d := listData{Sets: t.pool.SetNames(), Open: open, Rows: make([]listRow, 0, len(rows))}
	for _, x := range rows {
		d.Rows = append(d.Rows, listRow{ID: x.ID, Date: t.fullDate(x.CreatedAt), Title: x.Title, Cols: strings.Join(x.Cols, " · "), Cards: x.Cards})
	}
	t.app.Render(w, r, t.listT, web.View{Title: "Retro", Tool: Slug, Crumbs: []web.Crumb{{Label: "Retro"}}, Data: d})
}

func (t *Tool) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		t.fail(w, http.StatusRequestEntityTooLarge, "The form is too large.")
		return
	}
	set := t.pool.RandomSet(nil)
	if name := r.PostForm.Get("set"); name != "" {
		s, ok := t.pool.Set(name)
		if !ok {
			t.fail(w, http.StatusBadRequest, "There is no column set called "+strconv.Quote(name)+".")
			return
		}
		set = s
	}
	title := clip(strings.TrimSpace(r.PostForm.Get("title")), maxTitle)
	id, err := t.repo.CreateRetro(r.Context(), title, t.pool.RandomWarmup(""), set.Columns)
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	t.app.Log.Info("retro created", "id", id, "by", me(r), "set", set.Name)
	http.Redirect(w, r, "/retro/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (t *Tool) actions(w http.ResponseWriter, r *http.Request) {
	list, err := t.repo.OpenActions(r.Context())
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	rows := make([]actionRow, 0, len(list))
	for _, a := range list {
		rows = append(rows, actionRow{ID: a.ID, Body: a.Body, Owner: a.Owner, RetroID: a.RetroID, From: t.day(a.CreatedAt), Retro: a.RetroTitle})
	}
	t.app.Render(w, r, t.actionsT, web.View{Title: "Open actions", Tool: Slug,
		Crumbs: []web.Crumb{{Label: "Retro", Href: "/retro"}, {Label: "Open actions"}}, Data: rows})
}

func (t *Tool) actionDone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   int64 `json:"id"`
		Done bool  `json:"done"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil || in.ID <= 0 {
		t.fail(w, http.StatusBadRequest, "The request is not valid JSON with an action id.")
		return
	}
	err := t.repo.SetActionDone(r.Context(), in.ID, in.Done)
	if errors.Is(err, ErrNotFound) {
		t.fail(w, http.StatusNotFound, "This action no longer exists.")
		return
	}
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	t.app.Hub.SendPrefix("retro:", map[string]any{"t": "carry", "id": in.ID, "done": in.Done})
	writeJSON(w, map[string]any{})
}

func (t *Tool) load(r *http.Request, id int64) (*State, error) {
	ctx := r.Context()
	x, err := t.repo.Retro(ctx, id)
	if err != nil {
		return nil, err
	}
	cards, err := t.repo.Cards(ctx, id)
	if err != nil {
		return nil, err
	}
	carry, err := t.repo.Carry(ctx, id)
	if err != nil {
		return nil, err
	}
	names, err := t.app.Auth.People(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		t.locks.Decorate(&cards[i])
	}
	for i := range carry {
		carry[i].FromStr = t.day(carry[i].From)
	}
	on, ms := timerLeft(x.TimerEnd)
	return &State{
		ID: id, Date: t.fullDate(x.CreatedAt), Title: x.Title, Warmup: x.Warmup, Cols: x.Cols,
		Hidden: x.Hidden, TimerOn: on, TimerMs: ms, Cards: cards, Carry: carry,
		Me: me(r), Names: names, Here: t.app.Hub.Here(room(id)), Emojis: Emojis, Sets: t.pool.SetNames(),
		created: x.CreatedAt,
	}, nil
}

func timerLeft(end int64) (bool, int64) {
	if end == 0 || store.Now()-end > timerShownAfterEnd.Milliseconds() {
		return false, 0
	}
	left := end - store.Now()
	if left < 0 {
		left = 0
	}
	return true, left
}

func (t *Tool) room(w http.ResponseWriter, r *http.Request) {
	id, ok := retroID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s, err := t.load(r, id)
	if errors.Is(err, ErrNotFound) {
		t.fail(w, http.StatusNotFound, "This retro does not exist.")
		return
	}
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	title := s.Title
	if title == "" {
		title = "Retro " + s.Date
	}
	label := s.Title
	if label == "" {
		label = t.day(s.created)
	}
	t.app.Render(w, r, t.roomT, web.View{Title: title, Tool: Slug,
		Crumbs: []web.Crumb{{Label: "Retro", Href: "/retro"}, {Label: label}},
		Data:   roomData{State: s, Title: s.Title, Date: s.Date}})
}

func (t *Tool) stateJSON(w http.ResponseWriter, r *http.Request) {
	id, ok := retroID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s, err := t.load(r, id)
	if errors.Is(err, ErrNotFound) {
		t.fail(w, http.StatusNotFound, "This retro does not exist.")
		return
	}
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	writeJSON(w, s)
}

func (t *Tool) events(w http.ResponseWriter, r *http.Request) {
	id, ok := retroID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := t.repo.Retro(r.Context(), id); err != nil {
		t.fail(w, http.StatusNotFound, "This retro does not exist.")
		return
	}
	t.app.Hub.Serve(w, r, room(id), me(r))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

type op struct {
	Op    string `json:"op"`
	ID    int64  `json:"id"`
	Col   string `json:"col"`
	I     int    `json:"i"`
	Text  string `json:"text"`
	Seq   int64  `json:"seq"`
	Final bool   `json:"final"`
	Done  bool   `json:"done"`
	Tmp   string `json:"tmp"`
	Emoji string `json:"emoji"`
	On    bool   `json:"on"`
	Min   int    `json:"min"`
	Set   string `json:"set"`
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

func (t *Tool) do(w http.ResponseWriter, r *http.Request) {
	id, ok := retroID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var o op
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&o); err != nil {
		t.fail(w, http.StatusBadRequest, "The request is not valid JSON.")
		return
	}
	ctx := r.Context()
	x, err := t.repo.Retro(ctx, id)
	if errors.Is(err, ErrNotFound) {
		t.fail(w, http.StatusNotFound, "This retro does not exist.")
		return
	}
	if err != nil {
		t.dbFail(w, r, err)
		return
	}
	u := me(r)
	hub := t.app.Hub
	rm := room(id)
	sendMeta := func() {
		hub.Send(rm, map[string]any{"t": "meta", "cols": x.Cols, "warmup": x.Warmup, "title": x.Title, "hidden": x.Hidden})
	}
	cardErr := func(err error) {
		if errors.Is(err, ErrNotFound) {
			t.fail(w, http.StatusNotFound, "This card was deleted.")
			return
		}
		t.dbFail(w, r, err)
	}

	switch o.Op {
	case "add":
		if !validCol(o.Col, len(x.Cols)) {
			t.fail(w, http.StatusBadRequest, "This retro has no column "+strconv.Quote(o.Col)+".")
			return
		}
		if o.Tmp != "" && !tmpPattern.MatchString(o.Tmp) {
			t.fail(w, http.StatusBadRequest, "The client card id must be 1 to 40 letters, digits, - or _.")
			return
		}
		c, err := t.repo.AddCard(ctx, id, o.Col, u)
		if err != nil {
			t.dbFail(w, r, err)
			return
		}
		t.locks.Take(c.ID, u)
		t.locks.Decorate(&c)
		hub.Send(rm, map[string]any{"t": "card", "c": c, "tmp": o.Tmp})
		writeJSON(w, map[string]any{"c": c, "tmp": o.Tmp})
		return

	case "text":
		verdict, holder := t.locks.BeginWrite(o.ID, u, o.Seq, o.Final)
		switch verdict {
		case writeStale:
			writeJSON(w, map[string]any{"stale": true})
			return
		case writeLocked:
			t.fail(w, http.StatusConflict, holder+" is typing in this card right now.")
			return
		}
		c, err := t.repo.SetText(ctx, id, o.ID, clip(o.Text, maxBody))
		if err != nil {
			t.locks.Release(o.ID)
			cardErr(err)
			return
		}
		t.locks.Decorate(&c)
		hub.Send(rm, map[string]any{"t": "card", "c": c})

	case "del":
		if holder, _ := t.locks.Holder(o.ID, u); holder != "" {
			t.fail(w, http.StatusConflict, holder+" is typing in this card right now.")
			return
		}
		if err := t.repo.DeleteCard(ctx, id, o.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, map[string]any{})
				return
			}
			t.dbFail(w, r, err)
			return
		}
		t.locks.Release(o.ID)
		hub.Send(rm, map[string]any{"t": "del", "id": o.ID})

	case "owner":
		owner := strings.TrimSpace(o.Text)
		if owner != "" && owner != Everyone && !t.app.Auth.Known(ctx, owner) {
			t.fail(w, http.StatusBadRequest, strconv.Quote(owner)+" is not on the team list.")
			return
		}
		c, err := t.repo.SetOwner(ctx, id, o.ID, owner)
		if err != nil {
			cardErr(err)
			return
		}
		t.locks.Decorate(&c)
		hub.Send(rm, map[string]any{"t": "card", "c": c})

	case "done":
		if err := t.repo.SetCarryDone(ctx, id, o.ID, o.Done); err != nil {
			if errors.Is(err, ErrNotFound) {
				t.fail(w, http.StatusNotFound, "This action is not on the list from earlier retros.")
				return
			}
			t.dbFail(w, r, err)
			return
		}
		hub.Send(rm, map[string]any{"t": "carry", "id": o.ID, "done": o.Done})

	case "react":
		if !validEmoji(o.Emoji) {
			t.fail(w, http.StatusBadRequest, "Reactions are limited to "+strings.Join(Emojis, " ")+".")
			return
		}
		if _, err := t.repo.React(ctx, id, o.ID, u, o.Emoji, o.On); err != nil {
			cardErr(err)
			return
		}
		hub.Send(rm, map[string]any{"t": "react", "id": o.ID, "emoji": o.Emoji, "who": u, "on": o.On})

	case "cols":
		n, err := t.repo.BoardCardCount(ctx, id)
		if err != nil {
			t.dbFail(w, r, err)
			return
		}
		if n > 0 {
			t.fail(w, http.StatusConflict, "The columns already have cards, so the set stays. Click a column title to rename it.")
			return
		}
		set := t.pool.RandomSet(x.Cols)
		if o.Set != "" {
			s, ok := t.pool.Set(o.Set)
			if !ok {
				t.fail(w, http.StatusBadRequest, "There is no column set called "+strconv.Quote(o.Set)+".")
				return
			}
			set = s
		}
		x.Cols = append([]string(nil), set.Columns...)
		if err := t.repo.SetCols(ctx, id, x.Cols); err != nil {
			t.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "col":
		text := clip(strings.TrimSpace(o.Text), maxTitle)
		if o.I < 0 || o.I >= len(x.Cols) {
			t.fail(w, http.StatusBadRequest, "This retro has no column number "+strconv.Itoa(o.I+1)+".")
			return
		}
		if text == "" {
			t.fail(w, http.StatusBadRequest, "A column title cannot be empty.")
			return
		}
		x.Cols[o.I] = text
		if err := t.repo.SetCols(ctx, id, x.Cols); err != nil {
			t.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "warmup":
		x.Warmup = t.pool.RandomWarmup(x.Warmup)
		if err := t.repo.SetWarmup(ctx, id, x.Warmup); err != nil {
			t.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "warmuptext":
		text := clip(strings.TrimSpace(o.Text), maxWarmup)
		if text == "" {
			t.fail(w, http.StatusBadRequest, "The warm-up question cannot be empty.")
			return
		}
		x.Warmup = text
		if err := t.repo.SetWarmup(ctx, id, x.Warmup); err != nil {
			t.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "title":
		x.Title = clip(strings.Join(strings.Fields(o.Text), " "), maxTitle)
		if err := t.repo.SetTitle(ctx, id, x.Title); err != nil {
			t.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "hidden":
		x.Hidden = o.On
		if err := t.repo.SetHidden(ctx, id, x.Hidden); err != nil {
			t.dbFail(w, r, err)
			return
		}
		sendMeta()

	case "timer":
		if o.Min < 0 || o.Min > maxTimer {
			t.fail(w, http.StatusBadRequest, "The timer runs from 1 to "+strconv.Itoa(maxTimer)+" minutes, or 0 to stop it.")
			return
		}
		var end int64
		if o.Min > 0 {
			end = store.Now() + int64(time.Duration(o.Min)*time.Minute/time.Millisecond)
		}
		if err := t.repo.SetTimer(ctx, id, end); err != nil {
			t.dbFail(w, r, err)
			return
		}
		on, ms := timerLeft(end)
		hub.Send(rm, map[string]any{"t": "timer", "on": on, "ms": ms, "by": u})

	default:
		t.fail(w, http.StatusBadRequest, "Unknown action "+strconv.Quote(o.Op)+".")
		return
	}
	writeJSON(w, map[string]any{})
}
