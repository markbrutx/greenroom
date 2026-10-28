package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/markbrutx/greenroom/internal/config"
)

const maxLogoBytes = 512 << 10

type Brand struct {
	Name    string
	Version string
	Mark    template.HTML
	css     []byte
	logo    []byte
	logoCT  string
}

func loadBrand(cfg *config.Config) (Brand, error) {
	b := Brand{Name: cfg.Name}
	if cfg.Logo != "" {
		info, err := os.Stat(cfg.Logo)
		if err != nil {
			return b, fmt.Errorf("GREENROOM_LOGO: %w", err)
		}
		if info.Size() > maxLogoBytes {
			return b, fmt.Errorf("GREENROOM_LOGO: %s is larger than 512 KB", cfg.Logo)
		}
		ct, ok := map[string]string{
			".svg": "image/svg+xml", ".png": "image/png", ".webp": "image/webp",
			".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".ico": "image/x-icon",
		}[strings.ToLower(filepath.Ext(cfg.Logo))]
		if !ok {
			return b, fmt.Errorf("GREENROOM_LOGO: use an .svg, .png, .webp, .jpg or .ico file")
		}
		if b.logo, err = os.ReadFile(cfg.Logo); err != nil {
			return b, fmt.Errorf("GREENROOM_LOGO: %w", err)
		}
		b.logoCT = ct
	} else {
		logo, err := staticFS.ReadFile("static/logo.svg")
		if err != nil {
			return b, err
		}
		b.logo, b.logoCT = logo, "image/svg+xml"
		b.Mark = inlineMark(logo)
	}
	b.css = []byte(brandCSS(cfg.Accent))
	sum := sha256.New()
	sum.Write(b.css)
	sum.Write(b.logo)
	b.Version = hex.EncodeToString(sum.Sum(nil))[:10]
	return b, nil
}

func (app *App) serveBrandCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	app.cacheBrand(w, r)
	w.Write(app.brand.css)
}

func (app *App) serveLogo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", app.brand.logoCT)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	app.cacheBrand(w, r)
	w.Write(app.brand.logo)
}

func (app *App) cacheBrand(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("v") == app.brand.Version {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

type rgb struct{ r, g, b float64 }

func parseHex(s string) rgb {
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	return rgb{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}
}

func (c rgb) hex() string {
	clamp := func(x float64) int { return int(math.Max(0, math.Min(255, math.Round(x)))) }
	return fmt.Sprintf("#%02x%02x%02x", clamp(c.r), clamp(c.g), clamp(c.b))
}

func (c rgb) mix(o rgb, t float64) rgb {
	return rgb{c.r + (o.r-c.r)*t, c.g + (o.g-c.g)*t, c.b + (o.b-c.b)*t}
}

func (c rgb) luminance() float64 {
	ch := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
}

func contrast(a, b rgb) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

var (
	white      = rgb{255, 255, 255}
	ink        = rgb{0x19, 0x14, 0x14}
	lightPaper = rgb{0xf6, 0xf6, 0xf2}
	darkPaper  = rgb{0x14, 0x22, 0x19}
)

func brandCSS(accent string) string {
	if accent == "" {
		return "\n"
	}
	base := parseHex(accent)
	onLight := white
	if contrast(base, white) < 4.5 {
		onLight = ink
	}
	textLight := base
	for i := 1; i <= 20 && contrast(textLight, lightPaper) < 4.5; i++ {
		textLight = base.mix(ink, float64(i)/20)
	}
	dark := base
	for i := 1; i <= 20 && contrast(dark, darkPaper) < 4.8; i++ {
		dark = base.mix(white, float64(i)/20)
	}
	onDark := ink
	if contrast(dark, ink) < 4.5 {
		onDark = white
	}
	return fmt.Sprintf(`:root{--accent:%s;--accent-ink:%s;--on-accent:%s}
[data-theme="dark"]{--accent:%s;--accent-ink:%s;--on-accent:%s}
`, base.hex(), textLight.hex(), onLight.hex(), dark.hex(), dark.hex(), onDark.hex())
}

var svgStyle = regexp.MustCompile(`<style>.*?</style>`)

func inlineMark(svg []byte) template.HTML {
	s := svgStyle.ReplaceAllString(string(svg), "")
	s = strings.Replace(s, "<svg ", `<svg class="mark" width="28" height="28" aria-hidden="true" focusable="false" `, 1)
	return template.HTML(s)
}
