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

	"github.com/markbrutx/greenroom/internal/auth"
	"github.com/markbrutx/greenroom/internal/web"
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
	Slug       = "retro"
	Everyone   = "Everyone"
	maxBody    = 2000
	maxTitle   = 80
	maxWarmup  = 300
	maxColumns = 6
	maxTimer   = 60
	listLimit  = 300
)

var Emojis = []string{"👍", "❤️", "😂", "🎉", "🤔", "😮"}

type Tool struct {
	pool  *Pool
	repo  *Repo
	locks *Locks
	app   *web.App

	listT    *template.Template
	roomT    *template.Template
	actionsT *template.Template
}

func New(pool *Pool) *Tool {
	return &Tool{pool: pool, locks: NewLocks()}
}

func (t *Tool) Info() web.ToolInfo {
	return web.ToolInfo{
		Slug:    Slug,
		Name:    "Retro",
		Summary: "Live team retrospective: warm-up, columns, reactions and action items that follow you to the next one.",
		Icon:    "board",
		Badge:   t.badge,
	}
}

func (t *Tool) Mount(app *web.App) error {
	t.app = app
	t.repo = NewRepo(app.DB)
	mig, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}
	if err := app.DB.Migrate(context.Background(), Slug, mig); err != nil {
		return err
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	if err := app.AddStatic(Slug, static); err != nil {
		return err
	}
	for name, dst := range map[string]**template.Template{
		"templates/list.html": &t.listT, "templates/room.html": &t.roomT, "templates/actions.html": &t.actionsT,
	} {
		if *dst, err = app.Templates(templateFS, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	app.Page("GET /retro", t.list)
	app.Page("POST /retro/new", t.create)
	app.Page("GET /retro/actions", t.actions)
	app.API("POST /retro/actions/done", t.actionDone)
	app.Page("GET /retro/{id}", t.room)
	app.API("GET /retro/{id}/state", t.stateJSON)
	app.API("GET /retro/{id}/events", t.events)
	app.API("POST /retro/{id}/do", t.do)
	app.API("GET /retro/{id}/export.md", t.export)
	return nil
}

func (t *Tool) badge(ctx context.Context) string {
	open, last, err := t.repo.Stats(ctx)
	if err != nil {
		return ""
	}
	if last == 0 {
		return "No retros yet"
	}
	s := "Last retro " + t.day(last)
	switch open {
	case 0:
		return s
	case 1:
		return s + " · 1 open action"
	default:
		return s + " · " + strconv.Itoa(open) + " open actions"
	}
}

func (t *Tool) when(ms int64) time.Time { return time.UnixMilli(ms).In(t.app.Cfg.TZ) }

func (t *Tool) day(ms int64) string {
	at := t.when(ms)
	if at.Year() == time.Now().In(t.app.Cfg.TZ).Year() {
		return at.Format("Jan 2")
	}
	return at.Format("Jan 2, 2006")
}

func (t *Tool) fullDate(ms int64) string { return t.when(ms).Format("Mon, Jan 2, 2006") }

func (t *Tool) isoDate(ms int64) string { return t.when(ms).Format("2006-01-02") }

func room(id int64) string { return "retro:" + strconv.FormatInt(id, 10) }

func retroID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func me(r *http.Request) string { return auth.User(r.Context()) }
