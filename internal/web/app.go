package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/markbrutx/greenroom/internal/auth"
	"github.com/markbrutx/greenroom/internal/config"
	"github.com/markbrutx/greenroom/internal/live"
	"github.com/markbrutx/greenroom/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Tool interface {
	Info() ToolInfo
	Mount(app *App) error
}

type ToolInfo struct {
	Slug    string
	Name    string
	Summary string
	Icon    string
	Badge   func(ctx context.Context) string
}

type Crumb struct {
	Label string
	Href  string
}

type View struct {
	Title  string
	Crumbs []Crumb
	Tool   string
	User   string
	Brand  Brand
	Mode   string
	Data   any
}

type App struct {
	Cfg  *config.Config
	DB   *store.DB
	Hub  *live.Hub
	Auth *auth.Auth
	Log  *slog.Logger

	mux    *http.ServeMux
	base   *template.Template
	homeT  *template.Template
	loginT *template.Template
	brand  Brand
	tools  []ToolInfo

	assetsMu sync.RWMutex
	assets   map[string]asset
}

type asset struct {
	body    []byte
	version string
	ctype   string
}

func New(cfg *config.Config, db *store.DB, hub *live.Hub, a *auth.Auth, log *slog.Logger) (*App, error) {
	app := &App{Cfg: cfg, DB: db, Hub: hub, Auth: a, Log: log, mux: http.NewServeMux(), assets: map[string]asset{}}
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
		"initials": func(s string) string {
			return Initials(s)
		},
	}).ParseFS(templateFS, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	if err := app.parsePages(); err != nil {
		return nil, err
	}
	app.routes()
	return app, nil
}

func (app *App) Brand() Brand { return app.brand }

func (app *App) Register(t Tool) error {
	info := t.Info()
	if info.Slug == "" || strings.ContainsAny(info.Slug, "/ ") {
		return fmt.Errorf("tool slug %q is invalid", info.Slug)
	}
	if err := t.Mount(app); err != nil {
		return fmt.Errorf("mount %s: %w", info.Slug, err)
	}
	app.tools = append(app.tools, info)
	return nil
}

func (app *App) Page(pattern string, h http.HandlerFunc) {
	app.mux.Handle(pattern, app.Auth.Page(h))
}

func (app *App) API(pattern string, h http.HandlerFunc) {
	app.mux.Handle(pattern, app.Auth.API(h))
}

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
	v.User = auth.User(r.Context())
	v.Brand = app.brand
	v.Mode = app.Cfg.AuthMode
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

func Initials(s string) string {
	s = strings.TrimSpace(s)
	if at := strings.IndexByte(s, '@'); at > 0 {
		s = s[:at]
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '.' || r == '_' || r == '-' })
	var out []rune
	for _, p := range parts {
		r := []rune(p)
		if len(r) > 0 {
			out = append(out, r[0])
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return strings.ToUpper(string(out))
}
