# Adding a tool

Implement `web.Tool` (`Info()` with a slug and a name, `Mount(app)`) and
register it in `cmd/greenroom/main.go` after Retro. `/` opens the first tool;
with two or more, the top bar links to each. In `Mount` use `app.DB`,
`app.Hub`, `app.Page`, `app.API`, `app.Templates`, `app.Render` and
`app.AddStatic`. Put migrations in `internal/<tool>/migrations/`. No inline
scripts or styles, no comments in code. `internal/retro` is the example.
