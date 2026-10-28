package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/markbrutx/greenroom/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

const (
	CookiePrefix = "gr_"
	NameCookie   = "greenroom_name"
	CookieLength = 30 * 24 * time.Hour
	APIHeader    = "X-Greenroom"
	MaxNameRunes = 40
)

type Auth struct {
	key []byte
}

func New(ctx context.Context, db *store.DB) (*Auth, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, "auth", sub); err != nil {
		return nil, err
	}
	key, err := signingKey(ctx, db)
	if err != nil {
		return nil, err
	}
	return &Auth{key: key}, nil
}

func NewWithKey(key []byte) *Auth { return &Auth{key: key} }

func signingKey(ctx context.Context, db *store.DB) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := db.Exec(ctx, `insert into settings (key, value) values ('cookie_key', ?) on conflict (key) do nothing`,
		hex.EncodeToString(fresh)); err != nil {
		return nil, fmt.Errorf("store cookie key: %w", err)
	}
	var v string
	if err := db.QueryRow(ctx, `select value from settings where key = 'cookie_key'`).Scan(&v); err != nil {
		return nil, fmt.Errorf("read cookie key: %w", err)
	}
	key, err := hex.DecodeString(v)
	if err != nil || len(key) < 32 {
		return nil, errors.New("the cookie key in the settings table is damaged; delete that row to sign everyone out of every room")
	}
	return key, nil
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

func (a *Auth) sign(v string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func CookieName(code string) string { return CookiePrefix + code }

func (a *Auth) SetMember(w http.ResponseWriter, r *http.Request, code string, member int64) {
	exp := time.Now().Add(CookieLength).Unix()
	v := strconv.FormatInt(member, 10) + "." + strconv.FormatInt(exp, 10)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName(code),
		Value:    v + "." + a.sign(code+"|"+v),
		Path:     "/" + code,
		MaxAge:   int(CookieLength.Seconds()),
		HttpOnly: true,
		Secure:   IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) Member(r *http.Request, code string) (int64, bool) {
	c, err := r.Cookie(CookieName(code))
	if err != nil {
		return 0, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return 0, false
	}
	v := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(a.sign(code+"|"+v)), []byte(parts[2])) {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func ClearMember(w http.ResponseWriter, r *http.Request, code string) {
	http.SetCookie(w, &http.Cookie{Name: CookieName(code), Value: "", Path: "/" + code, MaxAge: -1,
		HttpOnly: true, Secure: IsHTTPS(r), SameSite: http.SameSiteLaxMode})
}

func RememberName(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     NameCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(name)),
		Path:     "/",
		MaxAge:   int(365 * 24 * time.Hour / time.Second),
		HttpOnly: true,
		Secure:   IsHTTPS(r),
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

func IsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func Token(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
