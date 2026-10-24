package web

import (
	"strings"
	"testing"
)

func TestBrandCSSKeepsContrast(t *testing.T) {
	if brandCSS("") != "\n" {
		t.Fatal("no accent means no overrides")
	}
	for _, accent := range []string{"#ffcc00", "#1f6feb", "#3e8342", "#000000", "#ffffff"} {
		css := brandCSS(accent)
		if !strings.Contains(css, "--accent:"+accent) {
			t.Errorf("%s: base accent missing in %q", accent, css)
		}
		vars := map[string]string{}
		for _, block := range strings.Split(css, "\n") {
			for _, part := range strings.Split(block, ";") {
				if i := strings.Index(part, "--accent-ink:"); i >= 0 && !strings.Contains(block, "dark") {
					vars["ink"] = strings.TrimSuffix(part[i+len("--accent-ink:"):], "}")
				}
			}
		}
		if c := contrast(parseHex(vars["ink"]), lightPaper); c < 4.5 {
			t.Errorf("%s: link color %s has contrast %.2f on light paper", accent, vars["ink"], c)
		}
	}
	if !strings.Contains(brandCSS("#ffcc00"), "--on-accent:#191414") {
		t.Error("a yellow accent needs dark button text")
	}
}
