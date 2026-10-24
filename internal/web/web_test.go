package web_test

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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/markbrutx/greenroom/internal/auth"
	"github.com/markbrutx/greenroom/internal/config"
	"github.com/markbrutx/greenroom/internal/live"
	"github.com/markbrutx/greenroom/internal/retro"
	"github.com/markbrutx/greenroom/internal/store"
	"github.com/markbrutx/greenroom/internal/storetest"
	"github.com/markbrutx/greenroom/internal/web"
)

const testPassword = "correct-horse"

type server struct {
	*httptest.Server
	t *testing.T
}

func start(t *testing.T, env map[string]string) *server {
	t.Helper()
	return startDB(t, storetest.Open(t), env)
}

func startDB(t *testing.T, db *store.DB, env map[string]string) *server {
	t.Helper()
	base := map[string]string{"GREENROOM_PASSWORD": testPassword}
	for k, v := range env {
		base[k] = v
	}
	cfg, err := config.Parse(func(k string) string { return base[k] })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := auth.New(context.Background(), cfg, db, log)
	if err != nil {
		t.Fatal(err)
	}
	hub := live.NewHub()
	app, err := web.New(cfg, db, hub, a, log)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := retro.LoadPool("")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Register(retro.New(pool)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return &server{srv, t}
}

func (s *server) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *server) post(c *http.Client, path string, body io.Reader, header map[string]string) *http.Response {
	s.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, s.URL+path, body)
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
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

func (s *server) login(c *http.Client, name, password string) *http.Response {
	form := url.Values{"name": {name}, "password": {password}}
	return s.post(c, "/login", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

func (s *server) newRetro(c *http.Client) int64 {
	s.t.Helper()
	resp := s.post(c, "/retro/new", strings.NewReader(""), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	id, err := strconv.ParseInt(strings.TrimPrefix(loc, "/retro/"), 10, 64)
	if resp.StatusCode != http.StatusSeeOther || err != nil {
		s.t.Fatalf("create retro: %d %q", resp.StatusCode, loc)
	}
	return id
}

func (s *server) do(c *http.Client, id int64, op map[string]any) (int, string) {
	s.t.Helper()
	b, _ := json.Marshal(op)
	resp := s.post(c, "/retro/"+strconv.FormatInt(id, 10)+"/do", strings.NewReader(string(b)),
		map[string]string{"Content-Type": "application/json", "X-Greenroom": "1"})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.CookieName {
			return ck
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestPasswordLogin(t *testing.T) {
	db := storetest.Open(t)
	s := startDB(t, db, nil)
	c := s.client()

	resp, _ := c.Get(s.URL + "/retro")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("anonymous page: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := s.login(c, "Alice", "wrong-password"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp := s.login(c, "   ", testPassword); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty name: %d", resp.StatusCode)
	}
	resp = s.login(c, "  Alice ", testPassword)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookie := sessionCookie(t, resp)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", cookie)
	}
	resp, _ = c.Get(s.URL + "/")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/retro" {
		t.Fatalf("home must open the first tool: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = c.Get(s.URL + "/retro")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Alice") {
		t.Fatalf("retro list after login: %d", resp.StatusCode)
	}

	other := s.client()
	s.login(other, "alice", testPassword)
	resp, _ = other.Get(s.URL + "/retro")
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Alice") {
		t.Fatal("a second login with another case must reuse the stored name")
	}

	s.post(c, "/logout", nil, nil)
	resp, _ = c.Get(s.URL + "/login")
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `value="Alice"`) {
		t.Fatal("the login form must offer the last used name")
	}

	forged := s.client()
	u, _ := url.Parse(s.URL)
	forged.Jar.SetCookies(u, []*http.Cookie{{Name: auth.CookieName, Value: strings.Replace(cookie.Value, ".", "x.", 1)}})
	if resp, _ := forged.Get(s.URL + "/retro"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forged cookie: %d", resp.StatusCode)
	}

	again := startDB(t, db, nil)
	kept := again.client()
	ku, _ := url.Parse(again.URL)
	kept.Jar.SetCookies(ku, []*http.Cookie{{Name: auth.CookieName, Value: cookie.Value}})
	if resp, _ := kept.Get(again.URL + "/retro"); resp.StatusCode != http.StatusOK {
		t.Fatalf("a restart with the same password keeps sessions: %d", resp.StatusCode)
	}

	changed := startDB(t, db, map[string]string{"GREENROOM_PASSWORD": "a new team password"})
	gone := changed.client()
	cu, _ := url.Parse(changed.URL)
	gone.Jar.SetCookies(cu, []*http.Cookie{{Name: auth.CookieName, Value: cookie.Value}})
	if resp, _ := gone.Get(changed.URL + "/retro"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("a new password must sign everyone out: %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	s := start(t, nil)
	c := s.client()
	for i := 0; i < 5; i++ {
		s.login(c, "eve", "nope-nope")
	}
	if resp := s.login(c, "eve", testPassword); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures the right password must wait: %d", resp.StatusCode)
	}
}

func TestCrossOriginAndHeaderChecks(t *testing.T) {
	s := start(t, nil)
	c := s.client()
	s.login(c, "alice", testPassword)
	id := s.newRetro(c)

	resp := s.post(c, "/retro/new", strings.NewReader(""), map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site form post: %d", resp.StatusCode)
	}
	resp = s.post(c, "/login", strings.NewReader("name=a&password=b"), map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": ""})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin login post: %d", resp.StatusCode)
	}
	resp = s.post(c, "/retro/"+strconv.FormatInt(id, 10)+"/do", strings.NewReader(`{"op":"warmup"}`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("JSON call without X-Greenroom: %d", resp.StatusCode)
	}
	anon := s.client()
	resp = s.post(anon, "/retro/"+strconv.FormatInt(id, 10)+"/do", strings.NewReader(`{"op":"warmup"}`), map[string]string{"X-Greenroom": "1"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous op: %d", resp.StatusCode)
	}
	if code, _ := s.do(c, id, map[string]any{"op": "warmup"}); code != http.StatusOK {
		t.Fatalf("valid op: %d", code)
	}
	r, _ := c.Get(s.URL + "/retro")
	csp := r.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") || r.Header.Get("X-Robots-Tag") == "" {
		t.Fatalf("security headers: %q", csp)
	}
}

func TestOpValidation(t *testing.T) {
	s := start(t, nil)
	c := s.client()
	s.login(c, "alice", testPassword)
	id := s.newRetro(c)
	other := s.newRetro(c)

	code, body := s.do(c, id, map[string]any{"op": "add", "col": "a", "tmp": "t1"})
	if code != http.StatusOK {
		t.Fatalf("add: %d %s", code, body)
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
		{"owner not on team", map[string]any{"op": "owner", "id": cardID, "text": "Nobody"}, 400},
		{"owner everyone", map[string]any{"op": "owner", "id": cardID, "text": "Everyone"}, 200},
		{"owner known", map[string]any{"op": "owner", "id": cardID, "text": "alice"}, 200},
		{"empty column title", map[string]any{"op": "col", "i": 0, "text": "  "}, 400},
		{"column index", map[string]any{"op": "col", "i": 7, "text": "x"}, 400},
		{"empty warm-up", map[string]any{"op": "warmuptext", "text": ""}, 400},
		{"random set", map[string]any{"op": "cols"}, 200},
		{"timer is gone", map[string]any{"op": "timer", "min": 5}, 400},
		{"title is gone", map[string]any{"op": "title", "text": "x"}, 400},
		{"done on own retro", map[string]any{"op": "done", "id": cardID, "done": true}, 404},
	}
	for _, tc := range cases {
		code, body := s.do(c, id, tc.op)
		if code != tc.code {
			t.Errorf("%s: got %d want %d (%s)", tc.name, code, tc.code, strings.TrimSpace(body))
		}
	}
	if code, _ := s.do(c, other, map[string]any{"op": "react", "id": cardID, "emoji": "👍", "on": true}); code != http.StatusNotFound {
		t.Errorf("react through another retro: got %d", code)
	}
	if code, _ := s.do(c, other, map[string]any{"op": "text", "id": cardID, "text": "x", "seq": 1}); code != http.StatusNotFound {
		t.Errorf("text through another retro: got %d", code)
	}
	if code, _ := s.do(c, id, map[string]any{"op": "react", "id": cardID, "emoji": "👍", "on": true}); code != http.StatusOK {
		t.Errorf("second react: got %d", code)
	}
	if code, _ := s.do(c, id, map[string]any{"op": "add", "col": "0"}); code != http.StatusOK {
		t.Errorf("board card: got %d", code)
	}
	if code, _ := s.do(c, id, map[string]any{"op": "cols"}); code != http.StatusConflict {
		t.Errorf("column set change with cards: got %d", code)
	}

	resp, _ := c.Get(s.URL + "/retro/" + strconv.FormatInt(id, 10) + "/state")
	var st retro.State
	json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if len(st.Cols) == 0 || st.Me != "alice" {
		t.Fatalf("state: %+v", st)
	}
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
	s := start(t, nil)
	alice, bob := s.client(), s.client()
	s.login(alice, "alice", testPassword)
	s.login(bob, "bob", testPassword)
	id := s.newRetro(alice)
	_, body := s.do(alice, id, map[string]any{"op": "add", "col": "0"})
	var added struct{ C retro.Card }
	json.Unmarshal([]byte(body), &added)
	cid := added.C.ID

	if code, _ := s.do(bob, id, map[string]any{"op": "text", "id": cid, "text": "mine now", "seq": 1}); code != http.StatusConflict {
		t.Fatalf("bob writing into a card alice is typing: %d", code)
	}
	if code, _ := s.do(bob, id, map[string]any{"op": "del", "id": cid}); code != http.StatusConflict {
		t.Fatalf("bob deleting a card alice is typing: %d", code)
	}
	s.do(alice, id, map[string]any{"op": "text", "id": cid, "text": "second", "seq": 2})
	code, body := s.do(alice, id, map[string]any{"op": "text", "id": cid, "text": "first", "seq": 1})
	if code != http.StatusOK || !strings.Contains(body, "stale") {
		t.Fatalf("older sequence must be ignored: %d %s", code, body)
	}
	s.do(alice, id, map[string]any{"op": "text", "id": cid, "text": "final", "seq": 3, "final": true})
	if code, _ := s.do(bob, id, map[string]any{"op": "text", "id": cid, "text": "bob edit", "seq": 1, "final": true}); code != http.StatusOK {
		t.Fatalf("after the final write bob can edit: %d", code)
	}
}

func TestEventsBroadcastReactions(t *testing.T) {
	s := start(t, nil)
	alice, bob := s.client(), s.client()
	s.login(alice, "alice", testPassword)
	s.login(bob, "bob", testPassword)
	id := s.newRetro(alice)
	_, body := s.do(alice, id, map[string]any{"op": "add", "col": "w", "tmp": "abc"})
	var added struct{ C retro.Card }
	json.Unmarshal([]byte(body), &added)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/retro/"+strconv.FormatInt(id, 10)+"/events", nil)
	resp, err := bob.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	waitFor := func(want string) {
		t.Helper()
		for lines.Scan() {
			if strings.Contains(lines.Text(), want) {
				return
			}
		}
		t.Fatalf("stream ended before %q", want)
	}
	waitFor(`"t":"here"`)
	s.do(alice, id, map[string]any{"op": "react", "id": added.C.ID, "emoji": "🎉", "on": true})
	waitFor(`"t":"react"`)
	s.do(alice, id, map[string]any{"op": "warmup"})
	waitFor(`"t":"meta"`)
}
