package config

import (
	"strings"
	"testing"
)

func parse(env map[string]string) (*Config, error) {
	return Parse(func(k string) string { return env[k] })
}

func TestDefaults(t *testing.T) {
	c, err := parse(map[string]string{"GREENROOM_PASSWORD_HASH": "$argon2id$x"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.DatabaseURL != "sqlite:greenroom.db" || c.AuthMode != AuthPassword || c.Name != "Greenroom" || c.TZ.String() != "UTC" || !c.SecretMissing {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestValidation(t *testing.T) {
	bad := []map[string]string{
		{},
		{"GREENROOM_AUTH": "header"},
		{"GREENROOM_AUTH": "magic"},
		{"GREENROOM_AUTH": "open", "GREENROOM_SECRET": "short"},
		{"GREENROOM_AUTH": "open", "GREENROOM_ACCENT": "green"},
		{"GREENROOM_AUTH": "open", "GREENROOM_TZ": "Mars/Base"},
		{"GREENROOM_AUTH": "open", "GREENROOM_TRUSTED_PROXIES": "not-an-ip"},
		{"GREENROOM_AUTH": "open", "GREENROOM_TLS_CERT": "/x.pem"},
	}
	for _, env := range bad {
		if _, err := parse(env); err == nil {
			t.Errorf("expected an error for %v", env)
		}
	}
	c, err := parse(map[string]string{
		"GREENROOM_AUTH":            "header",
		"GREENROOM_TRUSTED_PROXIES": "10.0.0.0/8, 127.0.0.1",
		"GREENROOM_USERS":           " alice, ,Bob ",
		"GREENROOM_SECRET":          strings.Repeat("k", 32),
		"GREENROOM_ACCENT":          "#2266AA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].Bits() != 32 || len(c.Users) != 2 || c.Users[1] != "Bob" {
		t.Fatalf("parsed: %+v", c)
	}
}
