package config

import "testing"

func parse(env map[string]string) (*Config, error) {
	return Parse(func(k string) string { return env[k] })
}

func TestDefaults(t *testing.T) {
	c, err := parse(map[string]string{"GREENROOM_PASSWORD": "team pass"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.DatabaseURL != "sqlite:greenroom.db" || c.Password != "team pass" || c.Name != "Greenroom" || c.TZ.String() != "UTC" {
		t.Fatalf("defaults: %+v", c)
	}
	if len(c.TrustedProxies) != 2 || !c.TrustedProxies[0].Addr().IsLoopback() || !c.TrustedProxies[1].Addr().IsLoopback() {
		t.Fatalf("trusted proxies default to loopback: %v", c.TrustedProxies)
	}
}

func TestValidation(t *testing.T) {
	bad := []map[string]string{
		{},
		{"GREENROOM_PASSWORD": "   "},
		{"GREENROOM_PASSWORD": "x", "GREENROOM_ACCENT": "green"},
		{"GREENROOM_PASSWORD": "x", "GREENROOM_TZ": "Mars/Base"},
		{"GREENROOM_PASSWORD": "x", "GREENROOM_TRUSTED_PROXIES": "not-an-ip"},
		{"GREENROOM_PASSWORD": "x", "GREENROOM_TLS_CERT": "/x.pem"},
	}
	for _, env := range bad {
		if _, err := parse(env); err == nil {
			t.Errorf("expected an error for %v", env)
		}
	}
	c, err := parse(map[string]string{
		"GREENROOM_PASSWORD":        "x",
		"GREENROOM_TRUSTED_PROXIES": "10.0.0.0/8, 127.0.0.1",
		"GREENROOM_ACCENT":          "#2266AA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].Bits() != 32 {
		t.Fatalf("parsed: %+v", c)
	}
}
