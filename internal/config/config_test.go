package config

import (
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
	if c.Addr != ":8080" || c.DatabaseURL != "sqlite:greenroom.db" || c.Name != "Greenroom" || c.TZ.String() != "UTC" {
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
		{"GREENROOM_ACCENT": "green"},
		{"GREENROOM_TZ": "Mars/Base"},
		{"GREENROOM_TRUSTED_PROXIES": "not-an-ip"},
		{"GREENROOM_TLS_CERT": "/x.pem"},
		{"GREENROOM_ROOM_TTL": "a week"},
		{"GREENROOM_ROOM_TTL": "1s"},
		{"GREENROOM_DATABASE_URL": "postgres://db/greenroom"},
	}
	for _, env := range bad {
		if _, err := parse(env); err == nil {
			t.Errorf("expected an error for %v", env)
		}
	}
	c, err := parse(map[string]string{
		"GREENROOM_TRUSTED_PROXIES": "10.0.0.0/8, 127.0.0.1",
		"GREENROOM_ACCENT":          "#2266AA",
		"GREENROOM_ROOM_TTL":        "48h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].Bits() != 32 || c.RoomTTL != 48*time.Hour {
		t.Fatalf("parsed: %+v", c)
	}
}
