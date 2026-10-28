package retro_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/markbrutx/greenroom/internal/auth"
	"github.com/markbrutx/greenroom/internal/config"
	"github.com/markbrutx/greenroom/internal/live"
	"github.com/markbrutx/greenroom/internal/retro"
	"github.com/markbrutx/greenroom/internal/storetest"
	"github.com/markbrutx/greenroom/internal/web"
)

type server struct {
	*httptest.Server
	t   *testing.T
	svc *retro.Service
}

func start(t *testing.T, env map[string]string, limits *retro.Limits) *server {
	t.Helper()
	db := storetest.Open(t)
	cfg, err := config.Parse(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := auth.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	hub := live.NewHub()
	app, err := web.New(cfg, db, hub, log)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := retro.LoadPool("")
	if err != nil {
		t.Fatal(err)
	}
	l := retro.DefaultLimits()
	if limits != nil {
		l = *limits
	}
	svc, err := retro.New(app, a, pool, l)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return &server{srv, t, svc}
}

func (s *server) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *server) post(c *http.Client, path, body string, header map[string]string) *http.Response {
	s.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, s.URL+path, strings.NewReader(body))
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

func (s *server) get(c *http.Client, path string) (int, string) {
	s.t.Helper()
	resp, err := c.Get(s.URL + path)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (s *server) create(c *http.Client, name, access string) string {
	s.t.Helper()
	resp := s.post(c, "/new", url.Values{"name": {name}, "access": {access}}.Encode(), nil)
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	code := strings.TrimSuffix(strings.TrimPrefix(loc, "/"), "?welcome=1")
	if resp.StatusCode != http.StatusSeeOther || !retro.ValidCode(code) {
		s.t.Fatalf("create room: %d %q", resp.StatusCode, loc)
	}
	return code
}

func (s *server) join(c *http.Client, code, name string) (int, string) {
	s.t.Helper()
	resp := s.post(c, "/"+code+"/join", url.Values{"name": {name}}.Encode(), nil)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (s *server) call(c *http.Client, code, action string, op map[string]any) (int, string) {
	s.t.Helper()
	b, _ := json.Marshal(op)
	resp := s.post(c, "/"+code+"/"+action, string(b), map[string]string{"Content-Type": "application/json", "X-Greenroom": "1"})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func (s *server) do(c *http.Client, code string, op map[string]any) (int, string) {
	return s.call(c, code, "do", op)
}

func (s *server) host(c *http.Client, code string, op map[string]any) (int, string) {
	return s.call(c, code, "host", op)
}

func (s *server) state(c *http.Client, code string) retro.State {
	s.t.Helper()
	status, body := s.get(c, "/"+code+"/state")
	if status != http.StatusOK {
		s.t.Fatalf("state: %d %s", status, body)
	}
	var st retro.State
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		s.t.Fatal(err)
	}
	return st
}

type stream struct {
	t     *testing.T
	lines chan string
	stop  context.CancelFunc
}

func (s *server) stream(c *http.Client, code string) *stream {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/"+code+"/events", nil)
	resp, err := c.Do(req)
	if err != nil {
		cancel()
		s.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		s.t.Fatalf("events: %d", resp.StatusCode)
	}
	st := &stream{t: s.t, lines: make(chan string, 256), stop: cancel}
	go func() {
		defer close(st.lines)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				st.lines <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	s.t.Cleanup(cancel)
	return st
}

func (st *stream) waitFor(want string) string {
	st.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-st.lines:
			if !ok {
				st.t.Fatalf("stream ended before %q", want)
			}
			if strings.Contains(line, want) {
				return line
			}
		case <-timeout:
			st.t.Fatalf("no %q within 5s", want)
		}
	}
}

func (st *stream) waitEnd() {
	st.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-st.lines:
			if !ok {
				return
			}
		case <-timeout:
			st.t.Fatal("the stream must be closed")
		}
	}
}

func cookieFor(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, ck := range resp.Cookies() {
		if ck.Name == name {
			return ck
		}
	}
	t.Fatalf("no %s cookie", name)
	return nil
}

func TestCreateRoom(t *testing.T) {
	s := start(t, nil, nil)
	c := s.client()
	status, body := s.get(c, "/")
	if status != http.StatusOK || !strings.Contains(body, "Create room") || !strings.Contains(body, "or join with a code") {
		t.Fatalf("home: %d", status)
	}
	if resp := s.post(c, "/new", "name=++&access=open", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty name: %d", resp.StatusCode)
	}
	resp := s.post(c, "/new", "name=Maya", nil)
	code := strings.TrimSuffix(strings.TrimPrefix(resp.Header.Get("Location"), "/"), "?welcome=1")
	if resp.StatusCode != http.StatusSeeOther || !retro.ValidCode(code) {
		t.Fatalf("create: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	ck := cookieFor(t, resp, "gr_"+code)
	if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/"+code {
		t.Fatalf("membership cookie: %+v", ck)
	}
	status, body = s.get(c, "/"+code+"?welcome=1")
	if status != http.StatusOK || !strings.Contains(body, `id="retro-state"`) || !strings.Contains(body, "Copy link") {
		t.Fatalf("host lands in the room: %d", status)
	}
	st := s.state(c, code)
	if !st.Host || st.Open || st.Me != "Maya" || len(st.Members) != 1 || len(st.Cols) == 0 || st.Warmup == "" {
		t.Fatalf("state: %+v", st)
	}
	if len(st.HostSecret) != 22 {
		t.Fatalf("the host secret must be 128 random bits, got %q", st.HostSecret)
	}
	if st.TTL != "7 days" {
		t.Fatalf("ttl text: %q", st.TTL)
	}
	resp, _ = c.Get(s.URL + "/join?code=" + url.QueryEscape("https://elsewhere.example/"+strings.ToUpper(code)))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/"+code {
		t.Fatalf("join with a link: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if status, _ := s.get(c, "/join?code=nonsense"); status != http.StatusBadRequest {
		t.Fatalf("join with nonsense: %d", status)
	}
}

func TestOpenRoomJoin(t *testing.T) {
	s := start(t, nil, nil)
	host, guest := s.client(), s.client()
	code := s.create(host, "Maya", "open")
	status, body := s.get(guest, "/"+code)
	if status != http.StatusOK || !strings.Contains(body, "Join Maya’s retro") || !strings.Contains(body, ">Join</button>") {
		t.Fatalf("join page: %d", status)
	}
	if status, _ := s.get(guest, "/"+code+"/state"); status != http.StatusUnauthorized {
		t.Fatalf("state before joining: %d", status)
	}
	if status, body := s.join(guest, code, "MAYA"); status != http.StatusConflict || !strings.Contains(body, "already goes by") {
		t.Fatalf("taken name: %d", status)
	}
	hs := s.stream(host, code)
	hs.waitFor(`"t":"here"`)
	if status, _ := s.join(guest, code, "  Jonas  "); status != http.StatusSeeOther {
		t.Fatalf("join: %d", status)
	}
	hs.waitFor(`"name":"Jonas"`)
	st := s.state(guest, code)
	if st.Host || st.Me != "Jonas" || len(st.Members) != 2 || st.HostSecret != "" || st.Pending != nil {
		t.Fatalf("guest state: %+v", st)
	}
	if code2, _ := s.do(guest, code, map[string]any{"op": "add", "col": "0"}); code2 != http.StatusOK {
		t.Fatalf("guest can write: %d", code2)
	}
	if status, _ := s.host(guest, code, map[string]any{"op": "access", "open": false}); status != http.StatusForbidden {
		t.Fatalf("guest host op: %d", status)
	}
}

func TestKnockAdmitDeny(t *testing.T) {
	s := start(t, nil, nil)
	host, jonas, priya, sam := s.client(), s.client(), s.client(), s.client()
	code := s.create(host, "Maya", "closed")

	_, body := s.get(jonas, "/"+code)
	if !strings.Contains(body, "Ask to join") {
		t.Fatal("a closed room asks to join")
	}
	if status, _ := s.join(jonas, code, "Jonas"); status != http.StatusSeeOther {
		t.Fatalf("knock: %d", status)
	}
	_, body = s.get(jonas, "/"+code)
	if !strings.Contains(body, "Asking to join") || strings.Contains(body, `id="host-note" hidden`) {
		t.Fatal("waiting page must say the host is not here")
	}
	if status, _ := s.get(jonas, "/"+code+"/state"); status != http.StatusForbidden {
		t.Fatalf("a waiting guest sees no state: %d", status)
	}
	if status, _ := s.do(jonas, code, map[string]any{"op": "add", "col": "0"}); status != http.StatusForbidden {
		t.Fatalf("a waiting guest cannot write: %d", status)
	}
	if status, _ := s.get(jonas, "/"+code+"/export.md"); status != http.StatusForbidden {
		t.Fatalf("a waiting guest cannot export: %d", status)
	}

	js := s.stream(jonas, code)
	js.waitFor(`"online":false`)
	hs := s.stream(host, code)
	js.waitFor(`"online":true`)
	st := s.state(host, code)
	if len(st.Pending) != 1 || st.Pending[0].Name != "Jonas" {
		t.Fatalf("host sees the knock: %+v", st.Pending)
	}
	_, body = s.get(jonas, "/"+code)
	if !strings.Contains(body, `id="host-note" hidden`) {
		t.Fatal("the host note hides while the host is online")
	}

	s.join(priya, code, "Priya")
	hs.waitFor(`"name":"Priya"`)
	ps := s.stream(priya, code)
	if status, _ := s.host(host, code, map[string]any{"op": "admit", "id": st.Pending[0].ID}); status != http.StatusOK {
		t.Fatalf("admit: %d", status)
	}
	js.waitFor(`"t":"admitted"`)
	hs.waitFor(`"t":"members"`)
	if got := s.state(jonas, code); got.Me != "Jonas" {
		t.Fatalf("admitted state: %+v", got)
	}
	if status, _ := s.host(host, code, map[string]any{"op": "admit", "id": st.Pending[0].ID}); status != http.StatusNotFound {
		t.Fatalf("admitting twice: %d", status)
	}

	pid := s.state(host, code).Pending[0].ID
	if status, _ := s.host(host, code, map[string]any{"op": "deny", "id": pid}); status != http.StatusOK {
		t.Fatalf("deny: %d", status)
	}
	ps.waitFor(`"t":"denied"`)
	ps.waitEnd()
	_, body = s.get(priya, "/"+code)
	if !strings.Contains(body, "You were not let in") || !strings.Contains(body, "Ask to join again") {
		t.Fatal("denied page")
	}
	if status, _ := s.get(priya, "/"+code+"/events"); status != http.StatusForbidden {
		t.Fatalf("denied guests get no stream: %d", status)
	}

	s.join(priya, code, "Priya")
	s.join(sam, code, "Sam")
	if n := len(s.state(host, code).Pending); n != 2 {
		t.Fatalf("two waiting: %d", n)
	}
	if status, _ := s.host(host, code, map[string]any{"op": "admitall"}); status != http.StatusOK {
		t.Fatalf("admit all: %d", status)
	}
	if st := s.state(host, code); len(st.Members) != 4 || len(st.Pending) != 0 {
		t.Fatalf("after admit all: %+v", st)
	}
}

func TestAccessToggleAdmitsWaiting(t *testing.T) {
	s := start(t, nil, nil)
	host, guest, late := s.client(), s.client(), s.client()
	code := s.create(host, "Maya", "closed")
	s.join(guest, code, "Jonas")
	gs := s.stream(guest, code)
	hs := s.stream(host, code)
	if status, _ := s.host(host, code, map[string]any{"op": "access", "open": true}); status != http.StatusOK {
		t.Fatalf("open: %d", status)
	}
	hs.waitFor(`"open":true,"t":"access"`)
	gs.waitFor(`"t":"admitted"`)
	s.join(late, code, "Sam")
	if st := s.state(late, code); !st.Open || st.Me != "Sam" {
		t.Fatalf("open room joins at once: %+v", st)
	}
	s.host(host, code, map[string]any{"op": "access", "open": false})
	if st := s.state(host, code); st.Open {
		t.Fatal("closed again")
	}
}

func TestRemoveParticipant(t *testing.T) {
	s := start(t, nil, nil)
	host, guest := s.client(), s.client()
	code := s.create(host, "Maya", "open")
	s.join(guest, code, "Jonas")
	gs := s.stream(guest, code)
	gs.waitFor(`"t":"here"`)
	st := s.state(host, code)
	var jonas, maya int64
	for _, m := range st.Members {
		if m.Name == "Jonas" {
			jonas = m.ID
		} else {
			maya = m.ID
		}
	}
	if status, _ := s.host(guest, code, map[string]any{"op": "remove", "id": maya}); status != http.StatusForbidden {
		t.Fatalf("a guest cannot remove the host: %d", status)
	}
	if status, _ := s.host(host, code, map[string]any{"op": "remove", "id": maya}); status != http.StatusNotFound {
		t.Fatalf("the host cannot remove themselves: %d", status)
	}
	if status, _ := s.host(host, code, map[string]any{"op": "remove", "id": jonas}); status != http.StatusOK {
		t.Fatalf("remove: %d", status)
	}
	gs.waitFor(`"t":"removed"`)
	gs.waitEnd()
	if status, _ := s.do(guest, code, map[string]any{"op": "warmup"}); status != http.StatusForbidden {
		t.Fatalf("a removed guest cannot write: %d", status)
	}
	_, body := s.get(guest, "/"+code)
	if !strings.Contains(body, "You were removed") {
		t.Fatal("removed page")
	}
	s.join(guest, code, "Jonas")
	if st := s.state(host, code); len(st.Pending) != 1 || len(st.Members) != 1 {
		t.Fatalf("a removed guest must ask again even in an open room: %+v", st)
	}
}

func TestHostLink(t *testing.T) {
	s := start(t, nil, nil)
	host := s.client()
	code := s.create(host, "Maya", "closed")
	secret := s.state(host, code).HostSecret

	other := s.client()
	if status, body := s.get(other, "/"+code+"?host=wrong"); status != http.StatusForbidden || !strings.Contains(body, "not valid") {
		t.Fatalf("wrong host secret: %d", status)
	}
	resp, _ := other.Get(s.URL + "/" + code + "?host=" + url.QueryEscape(secret))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/"+code {
		t.Fatalf("host link: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	st := s.state(other, code)
	if !st.Host || st.Me != "Maya" || st.HostSecret != secret {
		t.Fatalf("the host link signs in as the host: %+v", st)
	}
	if status, _ := s.host(other, code, map[string]any{"op": "access", "open": true}); status != http.StatusOK {
		t.Fatalf("host op from the second device: %d", status)
	}
}

func TestDeleteBroadcast(t *testing.T) {
	s := start(t, nil, nil)
	host, guest, waiting := s.client(), s.client(), s.client()
	code := s.create(host, "Maya", "open")
	keep := s.create(host, "Maya", "open")
	s.join(guest, code, "Jonas")
	s.do(guest, code, map[string]any{"op": "add", "col": "0"})
	s.host(host, code, map[string]any{"op": "access", "open": false})
	s.join(waiting, code, "Sam")
	hs, gs, ws := s.stream(host, code), s.stream(guest, code), s.stream(waiting, code)
	hs.waitFor(`"t":"here"`)

	if status, _ := s.host(guest, code, map[string]any{"op": "delete"}); status != http.StatusForbidden {
		t.Fatalf("a guest cannot delete: %d", status)
	}
	if status, _ := s.host(host, code, map[string]any{"op": "delete"}); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}
	for _, st := range []*stream{hs, gs, ws} {
		st.waitFor(`"t":"deleted","why":"host"`)
		st.waitEnd()
	}
	if status, body := s.get(guest, "/"+code); status != http.StatusNotFound || !strings.Contains(body, "No room here") {
		t.Fatalf("deleted room: %d", status)
	}
	if status, _ := s.do(guest, code, map[string]any{"op": "warmup"}); status != http.StatusNotFound {
		t.Fatalf("op in a deleted room: %d", status)
	}
	rooms, members, cards, err := s.svc.Repo().Counts(context.Background())
	if err != nil || rooms != 1 || members != 1 || cards != 0 {
		t.Fatalf("hard delete leaves only the other room: %v rooms=%d members=%d cards=%d", err, rooms, members, cards)
	}
	if status, _ := s.get(host, "/"+keep); status != http.StatusOK {
		t.Fatalf("the other room stays: %d", status)
	}
	if _, body := s.get(host, "/?n=deleted"); !strings.Contains(body, "room was deleted") {
		t.Fatal("home shows the deleted notice")
	}
}

func TestJanitorDeletesIdleRooms(t *testing.T) {
	s := start(t, map[string]string{"GREENROOM_ROOM_TTL": "2h"}, nil)
	host := s.client()
	idle := s.create(host, "Maya", "open")
	fresh := s.create(host, "Maya", "open")
	if s.svc.Sweep(context.Background()) != 0 {
		t.Fatal("fresh rooms stay")
	}
	room, err := s.svc.Repo().RoomByCode(context.Background(), idle)
	if err != nil {
		t.Fatal(err)
	}
	st := s.stream(host, idle)
	st.waitFor(`"t":"here"`)
	if err := s.svc.Repo().SetActiveAt(context.Background(), room.ID, time.Now().Add(-3*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if n := s.svc.Sweep(context.Background()); n != 1 {
		t.Fatalf("sweep deleted %d rooms, want 1", n)
	}
	st.waitFor(`"why":"expired"`)
	if status, _ := s.get(host, "/"+idle); status != http.StatusNotFound {
		t.Fatalf("idle room: %d", status)
	}
	if status, _ := s.get(host, "/"+fresh); status != http.StatusOK {
		t.Fatalf("active room: %d", status)
	}
	if _, body := s.get(host, "/"); !strings.Contains(body, "2 hours without activity") {
		t.Fatal("the home page names the configured lifetime")
	}
}

func TestExport(t *testing.T) {
	s := start(t, nil, nil)
	host, guest := s.client(), s.client()
	code := s.create(host, "Maya", "open")
	s.join(guest, code, "Jonas")
	add := func(c *http.Client, col, text string) int64 {
		_, body := s.do(c, code, map[string]any{"op": "add", "col": col})
		var res struct{ C retro.Card }
		json.Unmarshal([]byte(body), &res)
		s.do(c, code, map[string]any{"op": "text", "id": res.C.ID, "text": text, "seq": 1, "final": true})
		return res.C.ID
	}
	add(host, "w", "Focused")
	good := add(guest, "0", "Release went\nout <fine>")
	action := add(host, "a", "Fix the flaky tests")
	add(host, "1", "  ")
	s.do(host, code, map[string]any{"op": "react", "id": good, "emoji": "🎉", "on": true})
	s.do(guest, code, map[string]any{"op": "react", "id": good, "emoji": "🎉", "on": true})
	if status, _ := s.do(host, code, map[string]any{"op": "owner", "id": action, "text": "Jonas"}); status != http.StatusOK {
		t.Fatalf("owner: %d", status)
	}

	resp, err := guest.Get(s.URL + "/" + code + "/export.md?download=1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	md := string(b)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "retro-") || !strings.Contains(resp.Header.Get("Content-Disposition"), code) {
		t.Fatalf("export headers: %v", resp.Header)
	}
	for _, want := range []string{
		"# Team retro, ",
		"Participants (2): Maya (host), Jonas",
		"## Warm-up: ",
		"- Focused (Maya)",
		"- Release went out <fine> (Jonas) 🎉 2",
		"- [ ] Fix the flaky tests (owner: Jonas)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export lacks %q:\n%s", want, md)
		}
	}
	for _, gone := range []string{"From earlier", "From last time", "_No actions._"} {
		if strings.Contains(md, gone) {
			t.Errorf("export must not contain %q", gone)
		}
	}
	if status, _ := s.get(s.client(), "/"+code+"/export.md"); status != http.StatusUnauthorized {
		t.Fatalf("export without membership: %d", status)
	}
}

func TestRateLimits(t *testing.T) {
	limits := retro.Limits{
		Create: auth.NewLimiter(2, time.Hour),
		Join:   auth.NewLimiter(2, time.Hour),
		Lookup: auth.NewLimiter(3, time.Hour),
	}
	s := start(t, nil, &limits)
	c := s.client()
	code := s.create(c, "Maya", "open")
	s.create(c, "Maya", "open")
	if resp := s.post(c, "/new", "name=Maya", nil); resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("third room in the window: %d", resp.StatusCode)
	}

	s.join(s.client(), code, "A")
	s.join(s.client(), code, "B")
	if status, _ := s.join(s.client(), code, "C"); status != http.StatusTooManyRequests {
		t.Fatalf("third join in the window: %d", status)
	}

	for i := 0; i < 3; i++ {
		if status, _ := s.get(c, "/zzz-zzzz-zz"+string(rune('a'+i))); status != http.StatusNotFound {
			t.Fatalf("unknown code: %d", status)
		}
	}
	if status, _ := s.get(c, "/"+code); status != http.StatusTooManyRequests {
		t.Fatalf("after 3 unknown codes even a real room waits: %d", status)
	}
	if status, _ := s.get(c, "/"+code+"/state"); status != http.StatusTooManyRequests {
		t.Fatalf("API lookups wait too: %d", status)
	}
	if status, _ := s.get(c, "/not-a-code"); status != http.StatusNotFound {
		t.Fatalf("malformed paths: %d", status)
	}
}

func TestSecurityChecks(t *testing.T) {
	s := start(t, nil, nil)
	c := s.client()
	code := s.create(c, "Maya", "open")
	other := s.create(c, "Maya", "open")

	if resp := s.post(c, "/new", "name=x", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site create: %d", resp.StatusCode)
	}
	if resp := s.post(c, "/"+code+"/join", "name=x", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": ""}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin join: %d", resp.StatusCode)
	}
	if resp := s.post(c, "/"+code+"/do", `{"op":"warmup"}`, map[string]string{"Content-Type": "application/json"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("JSON call without X-Greenroom: %d", resp.StatusCode)
	}
	if resp := s.post(c, "/"+code+"/host", `{"op":"delete"}`, map[string]string{"Content-Type": "application/json"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("host call without X-Greenroom: %d", resp.StatusCode)
	}
	if status, _ := s.do(c, code, map[string]any{"op": "warmup"}); status != http.StatusOK {
		t.Fatalf("valid op: %d", status)
	}

	u, _ := url.Parse(s.URL)
	var ck *http.Cookie
	for _, x := range c.Jar.Cookies(&url.URL{Scheme: "http", Host: u.Host, Path: "/" + code}) {
		if x.Name == "gr_"+code {
			ck = x
		}
	}
	thief := s.client()
	thief.Jar.SetCookies(&url.URL{Scheme: "http", Host: u.Host, Path: "/" + other}, []*http.Cookie{{Name: "gr_" + other, Value: ck.Value, Path: "/" + other}})
	if status, _ := s.get(thief, "/"+other+"/state"); status != http.StatusUnauthorized {
		t.Fatalf("a cookie from one room must not open another: %d", status)
	}

	resp, _ := c.Get(s.URL + "/" + code)
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") || resp.Header.Get("X-Robots-Tag") == "" {
		t.Fatalf("security headers: %q", csp)
	}
	resp.Body.Close()

	evil := s.client()
	s.join(evil, code, `<script>alert(1)</script>`)
	_, body := s.get(c, "/"+code)
	if strings.Contains(body, "<script>alert(1)") {
		t.Fatal("names must be escaped in the page")
	}
	if status, _ := s.get(c, "/"+code+"/nothing"); status != http.StatusNotFound {
		t.Fatalf("unknown room action: %d", status)
	}
}

func TestOpValidation(t *testing.T) {
	s := start(t, nil, nil)
	c := s.client()
	code := s.create(c, "alice", "open")
	other := s.create(c, "alice", "open")

	status, body := s.do(c, code, map[string]any{"op": "add", "col": "a", "tmp": "t1"})
	if status != http.StatusOK {
		t.Fatalf("add: %d %s", status, body)
	}
	var added struct {
		C   retro.Card `json:"c"`
		Tmp string     `json:"tmp"`
	}
	json.Unmarshal([]byte(body), &added)
	if added.Tmp != "t1" || added.C.ID == 0 || added.C.Author != "alice" {
		t.Fatalf("add echo: %s", body)
	}
	cardID := added.C.ID

	cases := []struct {
		name string
		op   map[string]any
		code int
	}{
		{"unknown op", map[string]any{"op": "explode"}, 400},
		{"bad column", map[string]any{"op": "add", "col": "9"}, 400},
		{"padded column", map[string]any{"op": "add", "col": "01"}, 400},
		{"bad tmp", map[string]any{"op": "add", "col": "a", "tmp": "<script>"}, 400},
		{"bad emoji", map[string]any{"op": "react", "id": cardID, "emoji": "💩", "on": true}, 400},
		{"react", map[string]any{"op": "react", "id": cardID, "emoji": "👍", "on": true}, 200},
		{"owner not in room", map[string]any{"op": "owner", "id": cardID, "text": "Nobody"}, 400},
		{"owner everyone", map[string]any{"op": "owner", "id": cardID, "text": "Everyone"}, 200},
		{"owner member", map[string]any{"op": "owner", "id": cardID, "text": "alice"}, 200},
		{"empty column title", map[string]any{"op": "col", "i": 0, "text": "  "}, 400},
		{"column index", map[string]any{"op": "col", "i": 7, "text": "x"}, 400},
		{"empty warm-up", map[string]any{"op": "warmuptext", "text": ""}, 400},
		{"random set", map[string]any{"op": "cols"}, 200},
		{"carry-over is gone", map[string]any{"op": "done", "id": cardID, "done": true}, 400},
		{"unknown host op", map[string]any{"op": "admit"}, 400},
	}
	for _, tc := range cases {
		action := "do"
		if tc.name == "unknown host op" {
			action = "host"
			tc.op = map[string]any{"op": "promote"}
		}
		status, body := s.call(c, code, action, tc.op)
		if status != tc.code {
			t.Errorf("%s: got %d want %d (%s)", tc.name, status, tc.code, strings.TrimSpace(body))
		}
	}
	if status, _ := s.do(c, other, map[string]any{"op": "react", "id": cardID, "emoji": "👍", "on": true}); status != http.StatusNotFound {
		t.Errorf("react through another room: got %d", status)
	}
	if status, _ := s.do(c, other, map[string]any{"op": "text", "id": cardID, "text": "x", "seq": 1}); status != http.StatusNotFound {
		t.Errorf("text through another room: got %d", status)
	}
	if status, _ := s.do(c, code, map[string]any{"op": "add", "col": "0"}); status != http.StatusOK {
		t.Errorf("board card: got %d", status)
	}
	if status, _ := s.do(c, code, map[string]any{"op": "cols"}); status != http.StatusConflict {
		t.Errorf("column set change with cards: got %d", status)
	}
	st := s.state(c, code)
	var card retro.Card
	for _, x := range st.Cards {
		if x.ID == cardID {
			card = x
		}
	}
	if card.Owner != "alice" || len(card.R["👍"]) != 1 || card.Ver != 2 {
		t.Fatalf("card state: %+v", card)
	}
}

func TestTextSoftLockAndSequence(t *testing.T) {
	s := start(t, nil, nil)
	alice, bob := s.client(), s.client()
	code := s.create(alice, "alice", "open")
	s.join(bob, code, "bob")
	_, body := s.do(alice, code, map[string]any{"op": "add", "col": "0"})
	var added struct{ C retro.Card }
	json.Unmarshal([]byte(body), &added)
	cid := added.C.ID

	if status, _ := s.do(bob, code, map[string]any{"op": "text", "id": cid, "text": "mine now", "seq": 1}); status != http.StatusConflict {
		t.Fatalf("bob writing into a card alice is typing: %d", status)
	}
	if status, _ := s.do(bob, code, map[string]any{"op": "del", "id": cid}); status != http.StatusConflict {
		t.Fatalf("bob deleting a card alice is typing: %d", status)
	}
	s.do(alice, code, map[string]any{"op": "text", "id": cid, "text": "second", "seq": 2})
	status, body := s.do(alice, code, map[string]any{"op": "text", "id": cid, "text": "first", "seq": 1})
	if status != http.StatusOK || !strings.Contains(body, "stale") {
		t.Fatalf("older sequence must be ignored: %d %s", status, body)
	}
	s.do(alice, code, map[string]any{"op": "text", "id": cid, "text": "final", "seq": 3, "final": true})
	if status, _ := s.do(bob, code, map[string]any{"op": "text", "id": cid, "text": "bob edit", "seq": 1, "final": true}); status != http.StatusOK {
		t.Fatalf("after the final write bob can edit: %d", status)
	}
}

func TestEventsBroadcastCards(t *testing.T) {
	s := start(t, nil, nil)
	alice, bob := s.client(), s.client()
	code := s.create(alice, "alice", "open")
	s.join(bob, code, "bob")
	bs := s.stream(bob, code)
	bs.waitFor(`"t":"here"`)
	_, body := s.do(alice, code, map[string]any{"op": "add", "col": "w", "tmp": "abc"})
	bs.waitFor(`"tmp":"abc"`)
	var added struct{ C retro.Card }
	json.Unmarshal([]byte(body), &added)
	s.do(alice, code, map[string]any{"op": "react", "id": added.C.ID, "emoji": "🎉", "on": true})
	bs.waitFor(`"t":"react"`)
	s.do(alice, code, map[string]any{"op": "warmup"})
	bs.waitFor(`"t":"meta"`)
	s.do(alice, code, map[string]any{"op": "del", "id": added.C.ID})
	bs.waitFor(`"t":"del"`)
}
