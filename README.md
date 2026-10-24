# Greenroom

Small, self-hosted internal tools for teams of about ten people. One static
binary, your own server, your own data. The first tool is a live team
retrospective board.

![A retro in Greenroom, light theme](docs/img/retro-light.png)

<details>
<summary>Dark theme and phone</summary>

![The same retro in the dark theme](docs/img/retro-dark.png)

<img src="docs/img/retro-mobile.png" alt="The retro on a phone" width="320">

</details>

## Why

Most retro tools are SaaS. Many companies cannot paste internal problems into
a third-party board, and a team of ten does not need a platform. Greenroom
runs inside your network, stores everything in one SQLite file (or your
Postgres) and makes no outbound connections at all: no telemetry, no CDN, no
external fonts.

## Retro features

- **Live editing.** Everyone in the room sees cards appear and change as they
  are typed. A card someone is typing in is softly locked for a few seconds.
- **Warm-up question** to open the meeting, picked from 65 work-safe
  questions or written by hand.
- **19 column sets**: Start/Stop/Continue, Mad/Sad/Glad, 4Ls, Sailboat, KALM,
  Starfish, DAKI, Hot-air balloon, Rose/Thorn/Bud, What went well/What
  didn't/Ideas and more. Pick one for a new retro or roll the dice.
- **Reactions** on every card: 👍 ❤️ 😂 🎉 🤔 😮. Hover a chip to see who
  reacted.
- **Sort by 👍** to talk about the most voted cards first.
- **Shared timer** (1, 3, 5 or 10 minutes) that everyone sees in the top bar.
- **Private writing.** Blur everyone else's cards while people write, then
  reveal them together. This reduces anchoring; it is not secrecy, see the
  FAQ.
- **Action items with owners** that follow the team: unfinished actions from
  earlier retros show up in the next one, and `/retro/actions` lists every
  open action across all retros.
- **Markdown export** of the whole retro, reactions included, ready for a
  wiki page or a chat message.
- Light and dark theme, works on phones, keyboard friendly.

## Quick start

### Binary

Download the binary for your platform from
[Releases](https://github.com/markbrutx/greenroom/releases), then:

```sh
chmod +x greenroom_*_linux_amd64 && mv greenroom_*_linux_amd64 greenroom
export GREENROOM_PASSWORD_HASH="$(printf '%s\n' 'choose a team password' | ./greenroom hash)"
export GREENROOM_SECRET="$(openssl rand -hex 32)"
./greenroom
```

Open <http://localhost:8080>, type your name and the team password, start a
retro and share the link.

### Docker

```sh
docker run --rm -i ghcr.io/markbrutx/greenroom hash <<< 'choose a team password'
```

Put the printed hash and a secret into the environment, then start it with
the bundled compose file. The hash contains `$`, so keep the single quotes:

```sh
export GREENROOM_PASSWORD_HASH='$argon2id$v=19$m=65536,t=2,p=1$...'
export GREENROOM_SECRET="$(openssl rand -hex 32)"
docker compose -f deploy/docker-compose.yml up -d
```

Data lives in the `greenroom-data` volume. For Postgres use
`deploy/docker-compose.postgres.yml`.

### From source

```sh
git clone https://github.com/markbrutx/greenroom && cd greenroom
make build
```

Go 1.26 or newer. No Node.js, no CGO, no build step for the frontend.

For production (systemd, Kubernetes, reverse proxies, TLS with an internal
CA, SSO, backups, upgrades) read [docs/deploy.md](docs/deploy.md).

## Configuration

Everything is an environment variable. `.env.example` lists them all.

| Variable | Default | Meaning |
| --- | --- | --- |
| `GREENROOM_ADDR` | `:8080` | Address to listen on, for example `127.0.0.1:8080` |
| `GREENROOM_DATABASE_URL` | `sqlite:greenroom.db` | `sqlite:/path/file.db` or `postgres://user:pass@host:5432/db?sslmode=require` |
| `GREENROOM_AUTH` | `password` | `password`, `header` or `open` |
| `GREENROOM_PASSWORD_HASH` | | Shared team password as an argon2id hash from `greenroom hash`. Required in `password` mode |
| `GREENROOM_USERS` | | Optional allow-list, comma separated, case-insensitive. When empty, anyone with the password picks a name |
| `GREENROOM_AUTH_HEADER` | `X-Forwarded-User` | Header with the signed-in user, set by your SSO proxy (`header` mode) |
| `GREENROOM_AUTH_NAME_HEADER` | | Optional header with a display name (`header` mode) |
| `GREENROOM_TRUSTED_PROXIES` | | Comma separated IPs or CIDRs of your proxies. Required in `header` mode; also used to read the client IP for the login rate limit |
| `GREENROOM_SECRET` | random | 32+ bytes that sign session cookies. Without it everyone signs in again after a restart |
| `GREENROOM_TLS_CERT`, `GREENROOM_TLS_KEY` | | Serve HTTPS directly, without a proxy |
| `GREENROOM_NAME` | `Greenroom` | Name in the top bar, the title and the sign-in page |
| `GREENROOM_LOGO` | built-in | Path to an SVG, PNG, WebP, JPEG or ICO logo, up to 512 KB |
| `GREENROOM_ACCENT` | `#3e8342` | Accent color as `#rrggbb`; tints and readable text colors are derived |
| `GREENROOM_TZ` | `UTC` | Time zone for dates, for example `Europe/Berlin` |
| `GREENROOM_TEMPLATES` | built-in | Path to your own `pool.toml` with warm-up questions and column sets |
| `GREENROOM_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `GREENROOM_LOG_FORMAT` | `text` | `text` or `json` |

Commands:

```text
greenroom [serve]      start the server
greenroom hash         read a password from stdin, print its argon2id hash
greenroom backup FILE  consistent copy of the SQLite database
greenroom health       exit 0 when the local server answers /healthz
greenroom version      print the version
```

`/healthz` answers when the process is up, `/readyz` when the database
answers too.

## Sign-in modes

- **password** (default): one shared team password, everyone types a display
  name. With `GREENROOM_USERS` only the listed names can sign in. Five failed
  attempts block that address for 15 minutes.
- **header**: put Greenroom behind your SSO proxy (oauth2-proxy, Authelia,
  Pomerium, Cloudflare Access and similar) and let it pass the user in a
  header. Greenroom trusts the header only from `GREENROOM_TRUSTED_PROXIES`
  and answers 401 to everybody else.
- **open**: name only, no password. For closed lab networks. Greenroom logs a
  warning on every start.

## Your own templates

Copy [internal/retro/pool.toml](internal/retro/pool.toml), edit it and point
`GREENROOM_TEMPLATES` at your copy:

```toml
warmups = [
  "Describe the last two weeks in one word.",
  "What small win are you quietly proud of?",
]

[[sets]]
name = "Start, Stop, Continue"
columns = ["Start", "Stop", "Continue"]
```

A set has 1 to 6 columns. The Actions column is always added.

## Branding

Set `GREENROOM_NAME`, `GREENROOM_LOGO` and `GREENROOM_ACCENT` to make it look
like your company's tool. Details in [docs/white-label.md](docs/white-label.md).

## FAQ

**Can I run two instances for high availability?**
No. Live updates go through an in-memory hub, so all users must be on the
same process. Run one instance and let systemd, Docker or Kubernetes restart
it. A restart takes about a second and browsers reconnect by themselves.

**SQLite or Postgres?**
SQLite unless you already operate Postgres. A team of ten writes a few
hundred rows per retro; one file is easy to back up with `greenroom backup`.

**Is private writing secret?**
No. It blurs other people's cards in the browser so that nobody anchors on
the first card, but the text is still sent to every browser in the room.
Anyone who opens the developer tools can read it. Do not use it for
anonymous feedback.

**Who can edit or delete cards?**
Everyone who is signed in. A retro is a shared whiteboard for a trusted
team. While someone types in a card, others cannot change it for a few
seconds.

**What personal data is stored?**
Display names, card texts with their authors, action owners and reactions.
No IP addresses, no emails unless your SSO header sends emails as names.
See "Data and privacy" in [docs/deploy.md](docs/deploy.md).

**Does it call home?**
Never. There are no outbound requests, no analytics and no external assets.
It works in an air-gapped network.

**Does it work behind a corporate proxy or VPN?**
Yes. Live updates use Server-Sent Events over plain HTTP. Turn off response
buffering in the proxy; examples for Caddy and nginx are in
[docs/deploy.md](docs/deploy.md).

**Can I add my own tool?**
Yes, that is the plan for Greenroom. See
[docs/adding-a-tool.md](docs/adding-a-tool.md).

## Development

```sh
GREENROOM_AUTH=open make run
make test
GREENROOM_TEST_POSTGRES_URL='postgres://user:pass@127.0.0.1:5432/greenroom_test?sslmode=disable' make test-postgres
make lint
```

`make test` runs on SQLite. `make lint` runs `go vet`, checks `gofmt` and
enforces the no-comments rule below.

The code has no comments on purpose; names carry the meaning and the docs
carry the explanations.

## License

[MIT](LICENSE)
