# Greenroom

A live retro board for small teams, in one self-hosted binary.

![A retro in Greenroom](docs/img/retro.png)

## Why

A small team needs a retro tool, and later a few other small internal tools,
without extra features, accounts or a SaaS subscription. Greenroom is one
binary inside your own network with one shared password: type your name and
go.

## Run

```sh
docker run -d -p 8080:8080 -v greenroom:/data -e GREENROOM_PASSWORD='team password' ghcr.io/markbrutx/greenroom
```

```sh
GREENROOM_PASSWORD='team password' ./greenroom
```

Then open <http://localhost:8080>.

## Config

| Variable | Default | |
| --- | --- | --- |
| `GREENROOM_PASSWORD` | required | Shared team password |
| `GREENROOM_ADDR` | `:8080` | Listen address |
| `GREENROOM_DATABASE_URL` | `sqlite:greenroom.db` | Or `postgres://user:pass@host/db` |
| `GREENROOM_TRUSTED_PROXIES` | `127.0.0.1,::1` | Proxies allowed to set `X-Forwarded-For` |
| `GREENROOM_TLS_CERT`, `GREENROOM_TLS_KEY` | | Serve HTTPS directly |
| `GREENROOM_NAME` | `Greenroom` | Name in the top bar |
| `GREENROOM_LOGO` | built-in | Logo file: SVG, PNG, WebP, JPEG or ICO |
| `GREENROOM_ACCENT` | `#3e8342` | Accent color |
| `GREENROOM_TZ` | `UTC` | Time zone for dates |
| `GREENROOM_TEMPLATES` | built-in | Your own [pool.toml](internal/retro/pool.toml) |
| `GREENROOM_LOG_LEVEL`, `GREENROOM_LOG_FORMAT` | `info`, `text` | Logging |

Production setup: [docs/deploy.md](docs/deploy.md).

License: MIT
