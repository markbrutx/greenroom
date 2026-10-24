# Changelog

All notable changes to Greenroom are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-10-24

A smaller product: one way to sign in and a retro room with only the
controls a team uses.

### Changed

- Sign-in is a free name plus one shared team password from
  `GREENROOM_PASSWORD`. The last used name is prefilled.
- The session key is generated on first start and stored in the database;
  changing the password signs everyone out.
- `GREENROOM_TRUSTED_PROXIES` defaults to loopback and is only used to read
  the client IP for the login rate limit.
- `/` opens the retro list. "+ New retro" starts with the first column set
  and a random warm-up; the dice swaps the set while the board is empty.
- The theme follows the system setting.
- The top bar shows who is here as names with a green dot.

### Removed

- `header` and `open` sign-in modes, `GREENROOM_AUTH`,
  `GREENROOM_AUTH_HEADER`, `GREENROOM_AUTH_NAME_HEADER`, `GREENROOM_USERS`,
  `GREENROOM_PASSWORD_HASH`, `GREENROOM_SECRET`, argon2 and the
  `greenroom hash` command.
- Launcher home page, theme toggle, avatar initials, column counts.
- Shared timer, private writing, sort by 👍, retro titles, the column set
  picker and the open actions page. A migration drops the title, timer and
  private writing columns.

### Upgrading from 0.1.0

Set `GREENROOM_PASSWORD` and remove the old auth variables. Everyone signs
in again once.

## [0.1.0] - 2026-10-24

First public release.

### Added

- Greenroom shell: tool launcher home page, sign-in, light and dark themes,
  white-label name, logo and accent color.
- Sign-in modes: shared team password (argon2id) with an optional allow-list,
  trusted header from an SSO proxy, and open mode for closed networks.
- SQLite (pure Go, default) and Postgres support with forward-only embedded
  migrations.
- Retro tool: live editing over Server-Sent Events with soft per-card locks,
  warm-up question, 19 column sets, 65 warm-up questions, reactions, sort by
  votes, shared timer, private writing mode, editable title, action items with
  owners, carry-over of unfinished actions, open actions page and Markdown
  export.
- `greenroom hash`, `greenroom backup`, `greenroom health` and
  `greenroom version` commands.
- `/healthz` and `/readyz` endpoints, structured logs, graceful shutdown.
- Deployment files for systemd, Docker Compose (SQLite and Postgres),
  Kubernetes, Caddy and nginx.
