package auth

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestLimiterAllowsBlocksAndExpires(t *testing.T) {
	l := NewLimiter(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		if _, ok := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("blocked too early at %d", i)
		}
	}
	if _, ok := l.Allow("1.2.3.4"); ok {
		t.Fatal("the 4th call in the window must be refused")
	}
	if _, blocked := l.Blocked("1.2.3.4"); !blocked {
		t.Fatal("Blocked must agree with Allow")
	}
	if _, ok := l.Allow("5.6.7.8"); !ok {
		t.Fatal("other addresses stay open")
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok := l.Allow("1.2.3.4"); !ok {
		t.Fatal("the window must expire")
	}
	l.lastSweep = time.Time{}
	time.Sleep(60 * time.Millisecond)
	l.Blocked("x")
	if l.Len() != 0 {
		t.Fatalf("expired entries must be swept, %d left", l.Len())
	}
}

func TestLimiterHitCountsMisses(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	l.Hit("ip")
	if _, blocked := l.Blocked("ip"); blocked {
		t.Fatal("one miss is fine")
	}
	l.Hit("ip")
	if _, blocked := l.Blocked("ip"); !blocked {
		t.Fatal("two misses block")
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

func TestMemberCookie(t *testing.T) {
	a := NewWithKey([]byte("0123456789abcdef0123456789abcdef"))
	w := httptest.NewRecorder()
	a.SetMember(w, httptest.NewRequest("GET", "/", nil), "abc-defg-hij", 42)
	ck := w.Result().Cookies()[0]
	if ck.Name != "gr_abc-defg-hij" || ck.Path != "/abc-defg-hij" || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure {
		t.Fatalf("cookie attributes: %+v", ck)
	}
	r := httptest.NewRequest("GET", "/abc-defg-hij", nil)
	r.AddCookie(ck)
	if id, ok := a.Member(r, "abc-defg-hij"); !ok || id != 42 {
		t.Fatalf("member: %d %v", id, ok)
	}
	if _, ok := a.Member(r, "xyz-defg-hij"); ok {
		t.Fatal("a cookie for one room must not work for another")
	}
	other := httptest.NewRequest("GET", "/xyz-defg-hij", nil)
	other.AddCookie(&http.Cookie{Name: "gr_xyz-defg-hij", Value: ck.Value})
	if _, ok := a.Member(other, "xyz-defg-hij"); ok {
		t.Fatal("a cookie value copied to another room must not verify")
	}
	forged := httptest.NewRequest("GET", "/abc-defg-hij", nil)
	forged.AddCookie(&http.Cookie{Name: ck.Name, Value: strings.Replace(ck.Value, "42.", "43.", 1)})
	if _, ok := a.Member(forged, "abc-defg-hij"); ok {
		t.Fatal("a changed member id must not verify")
	}
	https := httptest.NewRequest("GET", "/", nil)
	https.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	a.SetMember(w, https, "abc-defg-hij", 1)
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("cookies are Secure behind HTTPS")
	}
}
