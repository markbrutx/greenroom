package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	AuthPassword = "password"
	AuthHeader   = "header"
	AuthOpen     = "open"
)

type Config struct {
	Addr        string
	DatabaseURL string
	TLSCert     string
	TLSKey      string

	AuthMode       string
	PasswordHash   string
	Users          []string
	AuthHeader     string
	AuthNameHeader string
	TrustedProxies []netip.Prefix
	Secret         []byte
	SecretMissing  bool

	Name      string
	Logo      string
	Accent    string
	TZ        *time.Location
	Templates string

	LogLevel  string
	LogFormat string
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func FromEnv() (*Config, error) { return Parse(os.Getenv) }

func Parse(get func(string) string) (*Config, error) {
	env := func(k, def string) string {
		if v := strings.TrimSpace(get("GREENROOM_" + k)); v != "" {
			return v
		}
		return def
	}
	c := &Config{
		Addr:           env("ADDR", ":8080"),
		DatabaseURL:    env("DATABASE_URL", "sqlite:greenroom.db"),
		TLSCert:        env("TLS_CERT", ""),
		TLSKey:         env("TLS_KEY", ""),
		AuthMode:       strings.ToLower(env("AUTH", AuthPassword)),
		PasswordHash:   env("PASSWORD_HASH", ""),
		AuthHeader:     env("AUTH_HEADER", "X-Forwarded-User"),
		AuthNameHeader: env("AUTH_NAME_HEADER", ""),
		Name:           env("NAME", "Greenroom"),
		Logo:           env("LOGO", ""),
		Accent:         env("ACCENT", ""),
		Templates:      env("TEMPLATES", ""),
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:      strings.ToLower(env("LOG_FORMAT", "text")),
	}
	for _, u := range strings.Split(get("GREENROOM_USERS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			c.Users = append(c.Users, u)
		}
	}
	for _, p := range strings.Split(get("GREENROOM_TRUSTED_PROXIES"), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pref, err := parsePrefix(p)
		if err != nil {
			return nil, fmt.Errorf("GREENROOM_TRUSTED_PROXIES: %q is not an IP or CIDR", p)
		}
		c.TrustedProxies = append(c.TrustedProxies, pref)
	}
	tz, err := time.LoadLocation(env("TZ", "UTC"))
	if err != nil {
		return nil, fmt.Errorf("GREENROOM_TZ: %w", err)
	}
	c.TZ = tz

	if s := get("GREENROOM_SECRET"); s != "" {
		if len(s) < 32 {
			return nil, errors.New("GREENROOM_SECRET must be at least 32 bytes (try: openssl rand -hex 32)")
		}
		c.Secret = []byte(s)
	} else {
		c.SecretMissing = true
	}

	switch c.AuthMode {
	case AuthPassword:
		if c.PasswordHash == "" {
			return nil, errors.New("GREENROOM_PASSWORD_HASH is required in password mode (create one with: greenroom hash)")
		}
	case AuthHeader:
		if len(c.TrustedProxies) == 0 {
			return nil, errors.New("GREENROOM_TRUSTED_PROXIES is required in header mode")
		}
	case AuthOpen:
	default:
		return nil, fmt.Errorf("GREENROOM_AUTH: unknown mode %q (password, header or open)", c.AuthMode)
	}
	if c.Accent != "" && !hexColor.MatchString(c.Accent) {
		return nil, fmt.Errorf("GREENROOM_ACCENT: %q is not a #rrggbb color", c.Accent)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return nil, errors.New("set both GREENROOM_TLS_CERT and GREENROOM_TLS_KEY, or neither")
	}
	if len([]rune(c.Name)) > 40 {
		return nil, errors.New("GREENROOM_NAME is longer than 40 characters")
	}
	return c, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}
