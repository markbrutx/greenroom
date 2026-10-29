package config

import (
	"strings"
	"testing"
	"time"
)

func parse(env map[string]string) (*Config, error) {
	return Parse(func(k string) string { return env[k] })
}

func TestDefaults(t *testing.T) {
	c, err := parse(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.DatabaseURL != "sqlite:moored.db" || c.Name != "moored.dev" || c.TZ.String() != "UTC" {
		t.Fatalf("defaults: %+v", c)
	}
	if c.RoomTTL != 7*24*time.Hour {
		t.Fatalf("rooms expire after 7 days by default, got %s", c.RoomTTL)
	}
	if len(c.TrustedProxies) != 2 || !c.TrustedProxies[0].Addr().IsLoopback() || !c.TrustedProxies[1].Addr().IsLoopback() {
		t.Fatalf("trusted proxies default to loopback: %v", c.TrustedProxies)
	}
}

func TestValidation(t *testing.T) {
	bad := []map[string]string{
		{"MOORED_ACCENT": "green"},
		{"MOORED_TZ": "Mars/Base"},
		{"MOORED_TRUSTED_PROXIES": "not-an-ip"},
		{"MOORED_TLS_CERT": "/x.pem"},
		{"MOORED_ROOM_TTL": "a week"},
		{"MOORED_ROOM_TTL": "1s"},
		{"MOORED_DATABASE_URL": "postgres://db/moored"},
	}
	for _, env := range bad {
		if _, err := parse(env); err == nil {
			t.Errorf("expected an error for %v", env)
		}
	}
	c, err := parse(map[string]string{
		"MOORED_TRUSTED_PROXIES": "10.0.0.0/8, 127.0.0.1",
		"MOORED_ACCENT":          "#2266AA",
		"MOORED_ROOM_TTL":        "48h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].Bits() != 32 || c.RoomTTL != 48*time.Hour {
		t.Fatalf("parsed: %+v", c)
	}
}

func TestRenamedVariables(t *testing.T) {
	if err := renamed([]string{"HOME=/root", "MOORED_ADDR=:8080"}); err != nil {
		t.Fatal(err)
	}
	err := renamed([]string{"GREENROOM_DATABASE_URL=sqlite:/data/greenroom.db"})
	if err == nil || !strings.Contains(err.Error(), "MOORED_DATABASE_URL") {
		t.Fatalf("old variables must stop the start with a hint: %v", err)
	}
}
