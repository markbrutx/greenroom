package auth

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestLimiterBlocksAndExpires(t *testing.T) {
	l := NewLimiter(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		if _, blocked := l.Blocked("1.2.3.4"); blocked {
			t.Fatalf("blocked too early at %d", i)
		}
		l.Fail("1.2.3.4")
	}
	if _, blocked := l.Blocked("1.2.3.4"); !blocked {
		t.Fatal("expected a block after 3 failures")
	}
	if _, blocked := l.Blocked("5.6.7.8"); blocked {
		t.Fatal("other addresses stay open")
	}
	time.Sleep(60 * time.Millisecond)
	if _, blocked := l.Blocked("1.2.3.4"); blocked {
		t.Fatal("the block must expire")
	}
	l.lastSweep = time.Time{}
	l.Blocked("x")
	if l.Len() != 0 {
		t.Fatalf("expired entries must be swept, %d left", l.Len())
	}
}

func TestClientIP(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.1.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.2.2.2")
	if got := ClientIP(r, proxies); got != "203.0.113.9" {
		t.Fatalf("behind trusted proxies: %s", got)
	}
	r.RemoteAddr = "198.51.100.7:5000"
	if got := ClientIP(r, proxies); got != "198.51.100.7" {
		t.Fatalf("untrusted peer must not be able to spoof: %s", got)
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"  Ada   Lovelace ": "Ada Lovelace",
		"bad\x00name\n":     "badname",
		"":                  "",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
	long := CleanName("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz")
	if len([]rune(long)) != MaxNameRunes {
		t.Fatalf("long names are clipped to %d runes, got %d", MaxNameRunes, len([]rune(long)))
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/retro/1": "/retro/1", "//evil.com": "/", "https://evil.com": "/", "/\\evil": "/", "": "/"} {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
