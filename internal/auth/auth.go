package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
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
	NameCookie    = "greenroom_name"
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
	pass    [32]byte
	limiter *Limiter

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
	stored, err := sessionKey(ctx, db)
	if err != nil {
		return nil, err
	}
	m := hmac.New(sha256.New, stored)
	m.Write([]byte(cfg.Password))
	return &Auth{
		cfg:     cfg,
		db:      db,
		log:     log,
		key:     m.Sum(nil),
		pass:    sha256.Sum256([]byte(cfg.Password)),
		limiter: NewLimiter(5, 15*time.Minute),
		touched: map[string]time.Time{},
	}, nil
}

func sessionKey(ctx context.Context, db *store.DB) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := db.Exec(ctx, `insert into settings (key, value) values ('session_key', ?) on conflict (key) do nothing`,
		hex.EncodeToString(fresh)); err != nil {
		return nil, fmt.Errorf("store session key: %w", err)
	}
	var v string
	if err := db.QueryRow(ctx, `select value from settings where key = 'session_key'`).Scan(&v); err != nil {
		return nil, fmt.Errorf("read session key: %w", err)
	}
	key, err := hex.DecodeString(v)
	if err != nil || len(key) < 32 {
		return nil, errors.New("the session key in the settings table is damaged; delete that row to sign everyone out and make a new one")
	}
	return key, nil
}

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

var (
	ErrNoName    = errors.New("Enter your name.")
	ErrWrongPass = errors.New("Wrong team password.")
)

type BlockedError struct{ Until time.Time }

func (e BlockedError) Error() string {
	return "Too many failed attempts from your network. Try again in " + strconv.Itoa(int(time.Until(e.Until).Minutes())+1) + " min."
}

func (a *Auth) Login(ctx context.Context, ip, rawName, password string) (string, error) {
	if until, blocked := a.limiter.Blocked(ip); blocked {
		return "", BlockedError{until}
	}
	name := CleanName(rawName)
	if name == "" {
		return "", ErrNoName
	}
	got := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(got[:], a.pass[:]) != 1 {
		a.limiter.Fail(ip)
		return "", ErrWrongPass
	}
	a.limiter.Reset(ip)
	return a.remember(ctx, name, true)
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
	http.SetCookie(w, &http.Cookie{
		Name:     NameCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(name)),
		Path:     "/login",
		MaxAge:   int(365 * 24 * time.Hour / time.Second),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func LastName(r *http.Request) string {
	c, err := r.Cookie(NameCookie)
	if err != nil {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return ""
	}
	return CleanName(string(raw))
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
	return string(raw)
}

type Denied struct {
	Status int
	Reason string
}

func (a *Auth) Identify(r *http.Request) (string, bool) {
	name := a.fromCookie(r)
	return name, name != ""
}

func (a *Auth) Page(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := a.Identify(r)
		if !ok {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		a.touch(r.Context(), name)
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), name)))
	})
}

func (a *Auth) API(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := a.Identify(r)
		if !ok {
			http.Error(w, "Your session has ended. Sign in again.", http.StatusUnauthorized)
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
