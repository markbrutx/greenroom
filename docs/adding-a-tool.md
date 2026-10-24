# Adding a tool

Greenroom ships one tool, Retro (`internal/retro`), on top of a small shell
that gives every tool sign-in, layout, branding, database and live updates.
Read Retro next to this page.

A tool implements `web.Tool`:

```go
type Tool interface {
	Info() ToolInfo
	Mount(app *App) error
}

type ToolInfo struct {
	Slug string
	Name string
}
```

`Slug` is the URL prefix and the migration component, for example `kudos`.
Register the tool in `cmd/greenroom/main.go` after Retro with
`app.Register(kudos.New())`. `/` redirects to the first registered tool. Once
there is more than one, the top bar shows a link to each by `Name`.

In `Mount`, the tool uses:

| | |
| --- | --- |
| `app.DB` | `Exec`, `Query`, `QueryRow`, `Begin`, `Migrate`; write `?` placeholders, they become `$n` on Postgres |
| `app.Hub` | SSE rooms with presence: `Serve`, `Send`, `Here` |
| `app.Auth` | `People(ctx)` and `Known(ctx, name)` |
| `app.Page`, `app.API` | Routes; pages redirect to sign-in, API calls get 401 and need `X-Greenroom: 1` on every non-GET |
| `app.Templates`, `app.Render` | A page on top of the shared layout with blocks `main`, `head`, `bar`, `bodyclass` |
| `app.AddStatic` | Files at `/static/<slug>/...`, use `{{asset "kudos/kudos.js"}}` |

`auth.User(r.Context())` is the name of the person making the request.

Migrations live in `internal/<tool>/migrations/0001_init.sql` and onwards,
are embedded and run with `app.DB.Migrate(ctx, slug, fsys)`. They are
forward-only. `{{ID}}`, `{{BOOL}}`, `{{TRUE}}` and `{{FALSE}}` expand to the
right types for SQLite and Postgres.

Rules: no inline scripts or styles (strict CSP), initial data in
`<script type="application/json">`, user text only through `textContent`,
colors only from the CSS variables in `internal/web/static/app.css`, the
server validates every intent, no outbound calls, no comments in code.
Tests use `storetest.Open(t)` and run with `go test -timeout 90s`.
