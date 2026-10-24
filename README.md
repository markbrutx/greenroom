# Greenroom

A live team retrospective board for teams of about ten people, self-hosted in
one static binary. Everything stays on your server, in one SQLite file or
your Postgres, and Greenroom makes no outbound connections.

![A retro in Greenroom](docs/img/retro-light.png)

## Features

- Live editing: everyone sees cards appear as they are typed, with a soft
  "<name> is typing" lock.
- Warm-up question from 65 work-safe questions, or your own.
- 19 column sets (Start/Stop/Continue, Mad/Sad/Glad, 4Ls, Sailboat and more),
  swapped with a dice while the board is empty.
- Reactions on every card: 👍 ❤️ 😂 🎉 🤔 😮.
- Actions with owners; unfinished ones come back in the next retro under
  "From last time".
- Copy summary: the whole retro as Markdown, reactions included.

## Quick start

Binary, from [Releases](https://github.com/markbrutx/greenroom/releases):

```sh
chmod +x greenroom_*_linux_amd64 && mv greenroom_*_linux_amd64 greenroom
GREENROOM_PASSWORD='choose a team password' ./greenroom
```

Docker:

```sh
export GREENROOM_PASSWORD='choose a team password'
docker compose -f deploy/docker-compose.yml up -d
```

Open <http://localhost:8080>, type your name and the team password, press
"+ New retro" and share the link.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `GREENROOM_PASSWORD` | | Shared team password. Required. Changing it signs everyone out |
| `GREENROOM_ADDR` | `:8080` | Listen address |
| `GREENROOM_DATABASE_URL` | `sqlite:greenroom.db` | `sqlite:/path/file.db` or `postgres://user:pass@host:5432/db` |
| `GREENROOM_TRUSTED_PROXIES` | `127.0.0.1,::1` | IPs or CIDRs of your reverse proxy; only used to read the client IP from `X-Forwarded-For` for the login rate limit |
| `GREENROOM_TLS_CERT`, `GREENROOM_TLS_KEY` | | Serve HTTPS directly |
| `GREENROOM_NAME` | `Greenroom` | Name in the top bar and the tab title |
| `GREENROOM_LOGO` | built-in | SVG, PNG, WebP, JPEG or ICO, up to 512 KB, square |
| `GREENROOM_ACCENT` | `#3e8342` | Accent color `#rrggbb`; readable variants for light and dark are derived |
| `GREENROOM_TZ` | `UTC` | Time zone for dates, for example `Europe/Berlin` |
| `GREENROOM_TEMPLATES` | built-in | Your own copy of [pool.toml](internal/retro/pool.toml) with questions and column sets |
| `GREENROOM_LOG_LEVEL`, `GREENROOM_LOG_FORMAT` | `info`, `text` | Logging; `json` suits log shippers |

The light and dark palettes follow the system setting. The session signing
key is created on first start and kept in the database.

Commands: `greenroom serve` (default), `greenroom backup FILE` (SQLite copy),
`greenroom health` (asks the local `/healthz`), `greenroom version`.
`/healthz` answers when the process is up, `/readyz` when the database
answers too.

Production setup (systemd, Docker, Kubernetes, reverse proxy, TLS, backups):
[docs/deploy.md](docs/deploy.md). Adding another tool:
[docs/adding-a-tool.md](docs/adding-a-tool.md).

## Development

```sh
GREENROOM_PASSWORD=dev make run
make test
GREENROOM_TEST_POSTGRES_URL='postgres://user:pass@127.0.0.1:5432/greenroom_test?sslmode=disable' make test-postgres
make lint
```

Go 1.26, no CGO, no frontend build. The code has no comments on purpose;
`make lint` enforces it.

## License

[MIT](LICENSE)
