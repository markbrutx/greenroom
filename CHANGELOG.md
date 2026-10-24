# Changelog

All notable changes to Greenroom are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

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
