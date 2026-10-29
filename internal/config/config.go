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

type Config struct {
	Addr        string
	DatabaseURL string
	TLSCert     string
	TLSKey      string

	TrustedProxies []netip.Prefix
	RoomTTL        time.Duration

	Name      string
	Logo      string
	Accent    string
	TZ        *time.Location
	Templates string

	LogLevel  string
	LogFormat string
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func FromEnv() (*Config, error) {
	if err := renamed(os.Environ()); err != nil {
		return nil, err
	}
	return Parse(os.Getenv)
}

func renamed(environ []string) error {
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if rest, ok := strings.CutPrefix(k, "GREENROOM_"); ok {
			return fmt.Errorf("%s is no longer read: Greenroom is now Moored, rename it to MOORED_%s", k, rest)
		}
	}
	return nil
}

func Parse(get func(string) string) (*Config, error) {
	env := func(k, def string) string {
		if v := strings.TrimSpace(get("MOORED_" + k)); v != "" {
			return v
		}
		return def
	}
	c := &Config{
		Addr:        env("ADDR", ":8080"),
		DatabaseURL: env("DATABASE_URL", "sqlite:moored.db"),
		TLSCert:     env("TLS_CERT", ""),
		TLSKey:      env("TLS_KEY", ""),
		Name:        env("NAME", "moored.dev"),
		Logo:        env("LOGO", ""),
		Accent:      env("ACCENT", ""),
		Templates:   env("TEMPLATES", ""),
		LogLevel:    strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:   strings.ToLower(env("LOG_FORMAT", "text")),
	}
	for _, p := range strings.Split(env("TRUSTED_PROXIES", "127.0.0.1,::1"), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pref, err := parsePrefix(p)
		if err != nil {
			return nil, fmt.Errorf("MOORED_TRUSTED_PROXIES: %q is not an IP or CIDR", p)
		}
		c.TrustedProxies = append(c.TrustedProxies, pref)
	}
	tz, err := time.LoadLocation(env("TZ", "UTC"))
	if err != nil {
		return nil, fmt.Errorf("MOORED_TZ: %w", err)
	}
	c.TZ = tz

	ttl, err := time.ParseDuration(env("ROOM_TTL", "168h"))
	if err != nil || ttl < time.Minute {
		return nil, fmt.Errorf("MOORED_ROOM_TTL: %q is not a duration of at least 1m, for example 168h", env("ROOM_TTL", ""))
	}
	c.RoomTTL = ttl
	if !strings.HasPrefix(c.DatabaseURL, "sqlite:") {
		return nil, errors.New("MOORED_DATABASE_URL must start with sqlite:, for example sqlite:/data/moored.db")
	}
	if c.Accent != "" && !hexColor.MatchString(c.Accent) {
		return nil, fmt.Errorf("MOORED_ACCENT: %q is not a #rrggbb color", c.Accent)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return nil, errors.New("set both MOORED_TLS_CERT and MOORED_TLS_KEY, or neither")
	}
	if len([]rune(c.Name)) > 40 {
		return nil, errors.New("MOORED_NAME is longer than 40 characters")
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
