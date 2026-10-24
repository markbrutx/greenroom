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
	"github.com/markbrutx/greenroom/internal/storetest"
	"github.com/markbrutx/greenroom/internal/web"
)

const testPassword = "correct-horse"

var testHash = func() string {
	h, err := auth.HashPassword(testPassword)
	if err != nil {
		panic(err)
	}
	return h
}()

type server struct {
	*httptest.Server
	t *testing.T
}

func start(t *testing.T, env map[string]string) *server {
	t.Helper()
	base := map[string]string{
		"GREENROOM_PASSWORD_HASH": testHash,
		"GREENROOM_SECRET":        strings.Repeat("s", 40),
	}
	for k, v := range env {
		base[k] = v
	}
	cfg, err := config.Parse(func(k string) string { return base[k] })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := storetest.Open(t)
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
	resp := s.post(c, "/retro/new", strings.NewReader("title=Sprint+1"), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
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

func TestPasswordLogin(t *testing.T) {
	s := start(t, map[string]string{"GREENROOM_USERS": "Alice,Bob"})
	c := s.client()

	resp, _ := c.Get(s.URL + "/retro")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("anonymous page: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := s.login(c, "alice", "wrong-password"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp := s.login(c, "mallory", testPassword); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("name not on list: %d", resp.StatusCode)
	}
	resp = s.login(c, "  alice ", testPassword)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.CookieName {
			cookie = ck
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", cookie)
	}
	resp, _ = c.Get(s.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Hi, Alice.") {
		t.Fatalf("home after login: %d", resp.StatusCode)
	}

	forged := s.client()
	u, _ := url.Parse(s.URL)
	forged.Jar.SetCookies(u, []*http.Cookie{{Name: auth.CookieName, Value: strings.Replace(cookie.Value, ".", "x.", 1)}})
	if resp, _ := forged.Get(s.URL + "/retro"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forged cookie: %d", resp.StatusCode)
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

func TestOpenModeFreeNames(t *testing.T) {
	s := start(t, map[string]string{"GREENROOM_AUTH": "open", "GREENROOM_PASSWORD_HASH": ""})
	c := s.client()
	if resp := s.login(c, "   ", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty name: %d", resp.StatusCode)
	}
	if resp := s.login(c, "Dana", ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("open login: %d", resp.StatusCode)
	}
	other := s.client()
	s.login(other, "dana", "")
	resp, _ := other.Get(s.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Hi, Dana.") {
		t.Fatal("a second login with another case must reuse the stored name")
	}
}

func TestHeaderAuthTrustsOnlyProxies(t *testing.T) {
	trusted := start(t, map[string]string{
		"GREENROOM_AUTH":             "header",
		"GREENROOM_AUTH_HEADER":      "X-Auth-Request-Email",
		"GREENROOM_AUTH_NAME_HEADER": "X-Auth-Request-User",
		"GREENROOM_TRUSTED_PROXIES":  "127.0.0.1/32,::1",
	})
	get := func(s *server, header map[string]string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, s.URL+"/", nil)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, body := get(trusted, map[string]string{"X-Auth-Request-Email": "erin@example.com", "X-Auth-Request-User": "Erin"})
	if code != http.StatusOK || !strings.Contains(body, "Hi, Erin.") {
		t.Fatalf("trusted proxy: %d", code)
	}
	code, _ = get(trusted, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("missing header: %d", code)
	}

	untrusted := start(t, map[string]string{
		"GREENROOM_AUTH":            "header",
		"GREENROOM_TRUSTED_PROXIES": "10.0.0.0/8",
	})
	code, _ = get(untrusted, map[string]string{"X-Forwarded-User": "erin"})
	if code != http.StatusUnauthorized {
		t.Fatalf("header from an untrusted peer must be refused: %d", code)
	}

	listed := start(t, map[string]string{
		"GREENROOM_AUTH":            "header",
		"GREENROOM_TRUSTED_PROXIES": "127.0.0.1,::1",
		"GREENROOM_USERS":           "Frank@Example.com",
	})
	code, _ = get(listed, map[string]string{"X-Forwarded-User": "mallory@example.com"})
	if code != http.StatusForbidden {
		t.Fatalf("user outside the allow-list: %d", code)
	}
	code, body = get(listed, map[string]string{"X-Forwarded-User": "frank@example.com"})
	if code != http.StatusOK || !strings.Contains(body, "Frank@Example.com") {
		t.Fatalf("allow-listed user: %d", code)
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
		{"timer too long", map[string]any{"op": "timer", "min": 99}, 400},
		{"timer ok", map[string]any{"op": "timer", "min": 5}, 200},
		{"timer stop", map[string]any{"op": "timer", "min": 0}, 200},
		{"empty column title", map[string]any{"op": "col", "i": 0, "text": "  "}, 400},
		{"column index", map[string]any{"op": "col", "i": 7, "text": "x"}, 400},
		{"empty warm-up", map[string]any{"op": "warmuptext", "text": ""}, 400},
		{"unknown set", map[string]any{"op": "cols", "set": "Nope"}, 400},
		{"named set", map[string]any{"op": "cols", "set": "Mad, Sad, Glad"}, 200},
		{"done on own retro", map[string]any{"op": "done", "id": cardID, "done": true}, 404},
		{"hidden", map[string]any{"op": "hidden", "on": true}, 200},
		{"title", map[string]any{"op": "title", "text": "Sprint   9"}, 200},
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
	if st.Title != "Sprint 9" || !st.Hidden || st.TimerOn || strings.Join(st.Cols, "|") != "Mad|Sad|Glad" {
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
	s := start(t, map[string]string{"GREENROOM_AUTH": "open", "GREENROOM_PASSWORD_HASH": ""})
	alice, bob := s.client(), s.client()
	s.login(alice, "alice", "")
	s.login(bob, "bob", "")
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
	s := start(t, map[string]string{"GREENROOM_AUTH": "open", "GREENROOM_PASSWORD_HASH": ""})
	alice, bob := s.client(), s.client()
	s.login(alice, "alice", "")
	s.login(bob, "bob", "")
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
	s.do(alice, id, map[string]any{"op": "timer", "min": 3})
	waitFor(`"t":"timer"`)
}
