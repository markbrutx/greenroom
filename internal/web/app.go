package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/markbrutx/greenroom/internal/config"
	"github.com/markbrutx/greenroom/internal/live"
	"github.com/markbrutx/greenroom/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type View struct {
	Title string
	Brand Brand
	Data  any
}

type App struct {
	Cfg *config.Config
	DB  *store.DB
	Hub *live.Hub
	Log *slog.Logger

	mux   *http.ServeMux
	base  *template.Template
	brand Brand

	assetsMu sync.RWMutex
	assets   map[string]asset
}

type asset struct {
	body    []byte
	version string
	ctype   string
}

func New(cfg *config.Config, db *store.DB, hub *live.Hub, log *slog.Logger) (*App, error) {
	app := &App{Cfg: cfg, DB: db, Hub: hub, Log: log, mux: http.NewServeMux(), assets: map[string]asset{}}
	brand, err := loadBrand(cfg)
	if err != nil {
		return nil, err
	}
	app.brand = brand
	core, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	if err := app.AddStatic("", core); err != nil {
		return nil, err
	}
	app.base, err = template.New("base").Funcs(template.FuncMap{
		"asset": app.assetURL,
		"icon":  icon,
	}).ParseFS(templateFS, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	app.routes()
	return app, nil
}

func (app *App) Brand() Brand { return app.brand }

func (app *App) Handle(pattern string, h http.Handler) { app.mux.Handle(pattern, h) }

func (app *App) HandleFunc(pattern string, h http.HandlerFunc) { app.mux.HandleFunc(pattern, h) }

func (app *App) Templates(fsys fs.FS, file string) (*template.Template, error) {
	t, err := app.base.Clone()
	if err != nil {
		return nil, err
	}
	return t.ParseFS(fsys, file)
}

func (app *App) Render(w http.ResponseWriter, r *http.Request, t *template.Template, v View) {
	app.RenderStatus(w, r, t, v, http.StatusOK)
}

func (app *App) RenderStatus(w http.ResponseWriter, r *http.Request, t *template.Template, v View, status int) {
	v.Brand = app.brand
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		app.Log.Error("render", "err", err, "path", r.URL.Path)
		http.Error(w, "The page could not be drawn. The server log has the details.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (app *App) AddStatic(prefix string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		ctype := mime.TypeByExtension(path.Ext(p))
		if path.Ext(p) == ".woff2" {
			ctype = "font/woff2"
		}
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		key := path.Join(prefix, p)
		app.assetsMu.Lock()
		app.assets[key] = asset{body: body, version: hex.EncodeToString(sum[:])[:10], ctype: ctype}
		app.assetsMu.Unlock()
		return nil
	})
}

func (app *App) assetURL(name string) string {
	app.assetsMu.RLock()
	a, ok := app.assets[name]
	app.assetsMu.RUnlock()
	if !ok {
		return "/static/" + name
	}
	return "/static/" + name + "?v=" + a.version
}

func (app *App) serveStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	app.assetsMu.RLock()
	a, ok := app.assets[name]
	app.assetsMu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.ctype)
	if r.URL.Query().Get("v") == a.version {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("ETag", `"`+a.version+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.body))
}

func (app *App) Handler() http.Handler {
	return secure(http.NewCrossOriginProtection().Handler(app.mux))
}

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; " +
	"font-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
