package retro

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/markbrutx/moored/internal/auth"
	"github.com/markbrutx/moored/internal/web"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

//go:embed pool.toml
var defaultPool []byte

const (
	Everyone   = "Everyone"
	maxBody    = 2000
	maxTitle   = 80
	maxWarmup  = 300
	maxColumns = 6
	sweepEvery = time.Hour
)

var Emojis = []string{"👍", "❤️", "😂", "🎉", "🤔", "😮"}

type Limits struct {
	Create *auth.Limiter
	Join   *auth.Limiter
	Lookup *auth.Limiter
}

func DefaultLimits() Limits {
	return Limits{
		Create: auth.NewLimiter(20, time.Hour),
		Join:   auth.NewLimiter(60, 10*time.Minute),
		Lookup: auth.NewLimiter(20, 15*time.Minute),
	}
}

type Service struct {
	pool   *Pool
	repo   *Repo
	locks  *Locks
	app    *web.App
	auth   *auth.Auth
	limits Limits

	homeT *template.Template
	joinT *template.Template
	roomT *template.Template
	goneT *template.Template
}

func New(app *web.App, a *auth.Auth, pool *Pool, limits Limits) (*Service, error) {
	s := &Service{pool: pool, locks: NewLocks(), app: app, auth: a, limits: limits, repo: NewRepo(app.DB)}
	mig, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	if err := app.DB.Migrate(context.Background(), "rooms", mig); err != nil {
		return nil, err
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	if err := app.AddStatic("retro", static); err != nil {
		return nil, err
	}
	for name, dst := range map[string]**template.Template{
		"templates/home.html": &s.homeT, "templates/join.html": &s.joinT,
		"templates/room.html": &s.roomT, "templates/gone.html": &s.goneT,
	} {
		if *dst, err = app.Templates(templateFS, name); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	app.HandleFunc("GET /{$}", s.home)
	app.HandleFunc("GET /join", s.findRoom)
	app.HandleFunc("POST /new", s.create)
	app.Handle("/", http.HandlerFunc(s.dispatch))
	return s, nil
}

func (s *Service) Repo() *Repo { return s.repo }

func (s *Service) dispatch(w http.ResponseWriter, r *http.Request) {
	code, action, _ := cutPath(r.URL.Path)
	if !ValidCode(code) {
		s.lookupMiss(w, r)
		return
	}
	switch {
	case action == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		s.page(w, r, code)
	case action == "join" && r.Method == http.MethodPost:
		s.join(w, r, code)
	case action == "events" && r.Method == http.MethodGet:
		s.events(w, r, code)
	case action == "state" && r.Method == http.MethodGet:
		s.api(w, r, code, s.stateJSON)
	case action == "do" && r.Method == http.MethodPost:
		s.api(w, r, code, s.do)
	case action == "host" && r.Method == http.MethodPost:
		s.api(w, r, code, s.hostOp)
	case action == "export.md" && r.Method == http.MethodGet:
		s.api(w, r, code, s.export)
	case action == "join" || action == "events" || action == "state" || action == "do" || action == "host" || action == "export.md" || action == "":
		w.Header().Set("Allow", allowFor(action))
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
	default:
		http.NotFound(w, r)
	}
}

func allowFor(action string) string {
	switch action {
	case "join", "do", "host":
		return http.MethodPost
	}
	return http.MethodGet
}

func cutPath(p string) (code, action string, ok bool) {
	if len(p) < 2 || p[0] != '/' {
		return "", "", false
	}
	p = p[1:]
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return p[:i], p[i+1:], true
		}
	}
	return p, "", true
}

func (s *Service) Janitor(ctx context.Context) {
	s.Sweep(ctx)
	tick := time.NewTicker(sweepEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.Sweep(ctx)
		}
	}
}

func (s *Service) Sweep(ctx context.Context) int {
	before := time.Now().Add(-s.app.Cfg.RoomTTL).UnixMilli()
	idle, err := s.repo.Idle(ctx, before)
	if err != nil {
		s.app.Log.Error("find idle rooms", "err", err)
		return 0
	}
	n := 0
	for _, x := range idle {
		if err := s.repo.DeleteRoom(ctx, x.ID); err != nil {
			s.app.Log.Error("delete idle room", "err", err, "room", x.Code)
			continue
		}
		s.app.Hub.CloseRoom(topic(x.Code), map[string]any{"t": "deleted", "why": "expired"})
		n++
	}
	if n > 0 {
		s.app.Log.Info("idle rooms deleted", "count", n, "ttl", s.app.Cfg.RoomTTL.String())
	}
	return n
}

func (s *Service) when(ms int64) time.Time { return time.UnixMilli(ms).In(s.app.Cfg.TZ) }

func (s *Service) fullDate(ms int64) string { return s.when(ms).Format("Mon, Jan 2, 2006") }

func (s *Service) isoDate(ms int64) string { return s.when(ms).Format("2006-01-02") }

func (s *Service) ttlText() string {
	ttl := s.app.Cfg.RoomTTL
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(n) + " " + unit + "s"
	}
	switch {
	case ttl >= 24*time.Hour && ttl%(24*time.Hour) == 0:
		return plural(int(ttl/(24*time.Hour)), "day")
	case ttl >= time.Hour && ttl%time.Hour == 0:
		return plural(int(ttl/time.Hour), "hour")
	default:
		return plural(int((ttl+time.Minute-1)/time.Minute), "minute")
	}
}

func topic(code string) string { return "room:" + code }
