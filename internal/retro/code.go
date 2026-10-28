package retro

import (
	"crypto/rand"
	"net/url"
	"regexp"
	"strings"
)

var codePattern = regexp.MustCompile(`^[a-z]{3}-[a-z]{4}-[a-z]{3}$`)

func ValidCode(s string) bool { return codePattern.MatchString(s) }

func NewCode() (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, 12)
	buf := make([]byte, 1)
	for len(out) < 12 {
		if len(out) == 3 || len(out) == 8 {
			out = append(out, '-')
			continue
		}
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		if buf[0] >= 26*9 {
			continue
		}
		out = append(out, letters[buf[0]%26])
	}
	return string(out), nil
}

func ParseCode(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.Path != "" && (u.Scheme != "" || strings.HasPrefix(s, "/")) {
		s = u.Path
	}
	s = strings.Trim(strings.ToLower(s), "/ ")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if !strings.Contains(s, "-") && len(s) == 10 {
		s = s[:3] + "-" + s[3:7] + "-" + s[7:]
	}
	return s, ValidCode(s)
}
