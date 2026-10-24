package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/markbrutx/greenroom/internal/config"
	"github.com/markbrutx/greenroom/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

const (
	CookieName    = "greenroom_session"
	SessionLength = 30 * 24 * time.Hour
	APIHeader     = "X-Greenroom"
	MaxNameRunes  = 40
	peopleWindow  = 90 * 24 * time.Hour
	touchEvery    = 10 * time.Minute
)

type Auth struct {
	cfg     *config.Config
	db      *store.DB
	log     *slog.Logger
	key     []byte
	limiter *Limiter
	argon   chan struct{}

	mu      sync.Mutex
	touched map[string]time.Time
}

type ctxKey struct{}

func New(ctx context.Context, cfg *config.Config, db *store.DB, log *slog.Logger) (*Auth, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, "auth", sub); err != nil {
		return nil, err
	}
	secret := cfg.Secret
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		if cfg.AuthMode != config.AuthHeader {
			log.Warn("GREENROOM_SECRET is not set: using a random key, everyone signs in again after a restart")
		}
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("session|" + cfg.AuthMode + "|" + cfg.PasswordHash))
	return &Auth{
		cfg:     cfg,
		db:      db,
		log:     log,
		key:     m.Sum(nil),
		limiter: NewLimiter(5, 15*time.Minute),
		argon:   make(chan struct{}, 2),
		touched: map[string]time.Time{},
	}, nil
}

func (a *Auth) Mode() string { return a.cfg.AuthMode }

func (a *Auth) UsesLoginForm() bool { return a.cfg.AuthMode != config.AuthHeader }

func (a *Auth) NeedsPassword() bool { return a.cfg.AuthMode == config.AuthPassword }

func (a *Auth) HasAllowList() bool { return len(a.cfg.Users) > 0 }

func User(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

func WithUser(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ctxKey{}, name)
}

func CleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u200b' || r == '\ufeff' {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxNameRunes {
		s = strings.TrimSpace(string(r[:MaxNameRunes]))
	}
	return s
}

func (a *Auth) allowed(name string) string {
	for _, u := range a.cfg.Users {
		if strings.EqualFold(u, name) {
			return u
		}
	}
	return ""
}

var (
	ErrNoName       = errors.New("Enter your name.")
	ErrNotOnList    = errors.New("This name is not on the team list. Ask your admin to add it.")
	ErrWrongPass    = errors.New("Wrong team password.")
	ErrNotLoginMode = errors.New("Sign-in is handled by your company login.")
)

type BlockedError struct{ Until time.Time }

func (e BlockedError) Error() string {
	return "Too many failed attempts from your network. Try again in " + strconv.Itoa(int(time.Until(e.Until).Minutes())+1) + " min."
}

func (a *Auth) Login(ctx context.Context, ip, rawName, password string) (string, error) {
	if !a.UsesLoginForm() {
		return "", ErrNotLoginMode
	}
	if until, blocked := a.limiter.Blocked(ip); blocked {
		return "", BlockedError{until}
	}
	name := CleanName(rawName)
	if name == "" {
		return "", ErrNoName
	}
	if a.HasAllowList() {
		canon := a.allowed(name)
		if canon == "" {
			a.limiter.Fail(ip)
			return "", ErrNotOnList
		}
		name = canon
	}
	if a.NeedsPassword() {
		a.argon <- struct{}{}
		ok := CheckPassword(a.cfg.PasswordHash, password)
		<-a.argon
		if !ok {
			a.limiter.Fail(ip)
			return "", ErrWrongPass
		}
	}
	a.limiter.Reset(ip)
	canon, err := a.remember(ctx, name, !a.HasAllowList())
	if err != nil {
		return "", err
	}
	return canon, nil
}

func (a *Auth) remember(ctx context.Context, name string, adoptStored bool) (string, error) {
	key := strings.ToLower(name)
	now := store.Now()
	if adoptStored {
		var stored string
		err := a.db.QueryRow(ctx, `select name from people where key = ?`, key).Scan(&stored)
		if err == nil {
			name = stored
		}
	}
	_, err := a.db.Exec(ctx, `insert into people (key, name, last_seen) values (?, ?, ?)
		on conflict (key) do update set last_seen = excluded.last_seen`, key, name, now)
	if err != nil {
		return "", fmt.Errorf("remember person: %w", err)
	}
	a.mu.Lock()
	a.touched[key] = time.Now()
	a.mu.Unlock()
	return name, nil
}

func (a *Auth) touch(ctx context.Context, name string) {
	key := strings.ToLower(name)
	a.mu.Lock()
	last, ok := a.touched[key]
	if ok && time.Since(last) < touchEvery {
		a.mu.Unlock()
		return
	}
	a.touched[key] = time.Now()
	a.mu.Unlock()
	if _, err := a.remember(ctx, name, false); err != nil {
		a.log.Warn("update last seen", "err", err)
	}
}

func (a *Auth) People(ctx context.Context) ([]string, error) {
	if a.HasAllowList() {
		return append([]string(nil), a.cfg.Users...), nil
	}
	rows, err := a.db.Query(ctx, `select name from people where last_seen > ? order by key`,
		time.Now().Add(-peopleWindow).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (a *Auth) Known(ctx context.Context, name string) bool {
	if a.HasAllowList() {
		return a.allowed(name) == name
	}
	var n int
	err := a.db.QueryRow(ctx, `select count(*) from people where key = ? and name = ?`, strings.ToLower(name), name).Scan(&n)
	return err == nil && n > 0
}

func (a *Auth) sign(v string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Auth) StartSession(w http.ResponseWriter, r *http.Request, name string) {
	exp := time.Now().Add(SessionLength).Unix()
	v := base64.RawURLEncoding.EncodeToString([]byte(name)) + "." + strconv.FormatInt(exp, 10)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    v + "." + a.sign(v),
		Path:     "/",
		MaxAge:   int(SessionLength.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) EndSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *Auth) fromCookie(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return ""
	}
	if !hmac.Equal([]byte(a.sign(parts[0]+"."+parts[1])), []byte(parts[2])) {
		return ""
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	name := string(raw)
	if a.HasAllowList() && a.allowed(name) != name {
		return ""
	}
	return name
}

type Denied struct {
	Status int
	Reason string
}

func (a *Auth) Identify(r *http.Request) (string, *Denied) {
	if a.cfg.AuthMode != config.AuthHeader {
		if name := a.fromCookie(r); name != "" {
			return name, nil
		}
		return "", &Denied{http.StatusUnauthorized, "Your session has ended. Sign in again."}
	}
	peer := PeerIP(r)
	if !peer.IsValid() || !Trusted(peer, a.cfg.TrustedProxies) {
		return "", &Denied{http.StatusUnauthorized, "This request did not come through the trusted sign-in proxy."}
	}
	id := CleanName(r.Header.Get(a.cfg.AuthHeader))
	if id == "" {
		return "", &Denied{http.StatusUnauthorized, "The sign-in proxy did not send the " + a.cfg.AuthHeader + " header."}
	}
	if a.HasAllowList() {
		canon := a.allowed(id)
		if canon == "" {
			return "", &Denied{http.StatusForbidden, id + " is not on the team list. Ask your admin to add it."}
		}
		return canon, nil
	}
	if a.cfg.AuthNameHeader != "" {
		if display := CleanName(r.Header.Get(a.cfg.AuthNameHeader)); display != "" {
			return display, nil
		}
	}
	return id, nil
}

func (a *Auth) Page(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, denied := a.Identify(r)
		if denied != nil {
			if a.UsesLoginForm() {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
				return
			}
			http.Error(w, denied.Reason, denied.Status)
			return
		}
		a.touch(r.Context(), name)
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), name)))
	})
}

func (a *Auth) API(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, denied := a.Identify(r)
		if denied != nil {
			http.Error(w, denied.Reason, denied.Status)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(APIHeader) != "1" {
			http.Error(w, "Missing the "+APIHeader+" request header.", http.StatusForbidden)
			return
		}
		a.touch(r.Context(), name)
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), name)))
	})
}

func SafeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}
