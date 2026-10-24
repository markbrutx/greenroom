# Adding a tool

Greenroom is a shell for small team tools. The shell gives every tool the
same sign-in, layout, themes, branding, database and live updates. A tool
brings its pages, its tables and its JavaScript. Retro (`internal/retro`) is
the reference: read it next to this page.

## The contract

A tool is any type that implements `web.Tool`:

```go
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
```

- `Slug` is the URL prefix and the migration component name, for example
  `kudos`. Lowercase, no slashes or spaces.
- `Name` and `Summary` appear on the home page tile.
- `Icon` names an icon from `internal/web/icons.go`. Register your own with
  `web.RegisterIcon("kudos", "<path d=\"...\"/>")` before `Register`; icons are
  24x24 stroke paths drawn with `currentColor`.
- `Badge` is optional. It returns a short status for the tile, such as
  "3 open actions". It runs on every home page view with a 2 second timeout,
  so keep it to one cheap query.

Register the tool in `cmd/greenroom/main.go`, after the retro:

```go
if err := app.Register(kudos.New()); err != nil {
	return err
}
```

Tiles appear on the home page in registration order. There are no placeholder
tiles: the grid simply grows.

## What Mount gets

`*web.App` exposes everything a tool needs:

| Field or method | Use |
| --- | --- |
| `app.DB` | `*store.DB`: `Exec`, `Query`, `QueryRow`, `Begin`, `Migrate`. Write SQL once with `?` placeholders; they are rebound to `$n` on Postgres |
| `app.Hub` | `*live.Hub`: Server-Sent Events rooms with presence. `Serve(w, r, room, name)`, `Send(room, v)`, `SendPrefix(prefix, v)`, `Here(room)` |
| `app.Auth` | `People(ctx)` for the team list, `Known(ctx, name)` to validate a name |
| `app.Cfg` | The parsed configuration, for example `Cfg.TZ` |
| `app.Log` | A `*slog.Logger` |
| `app.Page(pattern, h)` | An HTML route. Signed-out visitors are sent to the sign-in page |
| `app.API(pattern, h)` | A JSON or stream route. Signed-out callers get 401; every non-GET call must carry `X-Greenroom: 1` |
| `app.Templates(fsys, file)` | Parses one page template on top of the shared layout |
| `app.Render(w, r, t, view)` | Renders a page inside the layout |
| `app.AddStatic(slug, fsys)` | Serves files at `/static/<slug>/...` with content hashes for caching |

In a handler, `auth.User(r.Context())` returns the display name of the person
making the request.

Every POST, including plain HTML forms, also passes Go's cross-origin
protection, so a form on another site cannot post into Greenroom.

## Database

Put migrations in `internal/<tool>/migrations/` as `0001_init.sql`,
`0002_something.sql` and so on, embed them and run them from `Mount`:

```go
//go:embed migrations/*.sql
var migrationFS embed.FS
```

```go
sub, _ := fs.Sub(migrationFS, "migrations")
if err := app.DB.Migrate(ctx, "kudos", sub); err != nil {
	return err
}
```

Migrations are forward-only. Each file runs once in a transaction and is
recorded in `schema_migrations` under the tool's component name. Statements
end with `;` at the end of a line. Four macros cover the dialect differences:

| Macro | SQLite | Postgres |
| --- | --- | --- |
| `{{ID}}` | `integer primary key autoincrement` | `bigserial primary key` |
| `{{BOOL}}` | `integer` | `boolean` |
| `{{TRUE}}` / `{{FALSE}}` | `1` / `0` | `true` / `false` |

Store time as unix milliseconds in a `bigint` (`store.Now()`), money-like
numbers as integers and lists as JSON text. Use `returning` and
`on conflict do nothing`; both databases support them.

## Pages

A page template defines blocks that the layout fills:

| Block | Purpose |
| --- | --- |
| `main` | The page content. Required |
| `head` | Extra `<link>` and `<script defer>` tags |
| `bar` | Extra items in the top bar, right side |
| `bodyclass` | A class on `<body>` |

```html
{{define "head"}}<link rel="stylesheet" href="{{asset "kudos/kudos.css"}}"><script src="{{asset "kudos/kudos.js"}}" defer></script>{{end}}
{{define "main"}}
<script type="application/json" id="kudos-state">{{.Data}}</script>
<h1>Kudos</h1>
<ul id="wall"></ul>
{{end}}
```

```go
t.app.Render(w, r, t.pageT, web.View{
	Title:  "Kudos",
	Crumbs: []web.Crumb{{Label: "Kudos"}},
	Data:   state,
})
```

Template helpers: `asset` for static URLs, `icon` for inline icons and
`initials` for avatar letters.

## Rules every tool follows

The Content-Security-Policy forbids inline scripts, inline styles and any
other origin. So:

- JavaScript and CSS are static files served with `AddStatic`.
- Initial data goes into `<script type="application/json">` and is read with
  `JSON.parse(el.textContent)`.
- Never put user text into `innerHTML`. Use `textContent` and DOM methods.
- No CDNs, fonts or images from other hosts. No outbound calls from the
  server.
- Colors come only from the CSS variables in
  `internal/web/static/app.css`, so both themes and the accent color work.
  Animate only `transform` and `opacity`.
- The server decides everything. The browser sends intents such as
  `{"op": "add"}`; the handler validates them against the database.
- Live updates: one SSE room per document, for example `kudos:12`. Send small
  deltas and let clients ignore stale ones (Retro uses a `ver` column).
- Greenroom runs as a single instance, so in-memory state in a tool is fine
  as long as losing it on restart is harmless.
- No comments in code, templates, SQL or config. Explain in the docs.

## Tests

Use `storetest.Open(t)` for a fresh database. It is SQLite by default and
Postgres when `GREENROOM_TEST_POSTGRES_URL` is set, each test in its own
schema. Always run with a timeout:

```sh
go test -timeout 90s ./internal/kudos
```
