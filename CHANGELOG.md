# Changelog

## 0.3.0

Breaking: Greenroom is now a Meet-style retro room tool. 0.3 starts with an
empty database and refuses to open a 0.2 file; keep the old file as a backup
and start with a new one.

- Start page: create a room with your name, *Ask to join* (default) or
  *Open*, or join with a code or link. Rooms live at `/abc-defg-hij`.
- Ask to join: guests wait on a live screen; the host admits or denies each
  person, or admits everyone, without reloads.
- Host controls: switch open and closed, remove people, delete the room for
  everyone, and a private host link to manage the room from another device.
- Copy .md and Download .md export the retro as Markdown with
  participants, warm-up, columns, actions with owners and reaction counts.
- Rooms are deleted after `GREENROOM_ROOM_TTL` (default 7 days) without
  activity.
- Per-IP rate limits for creating rooms, joining and unknown room codes.
- Removed: `GREENROOM_PASSWORD`, the sign-in page, the retro list, carry-over
  of open actions, Postgres support and the multi-tool scaffolding.

## 0.2.0

- One sign-in: a name and the shared `GREENROOM_PASSWORD`.
- Simpler retro room; the theme follows the system.
- Removed: header and open sign-in, `GREENROOM_SECRET`, `greenroom hash`, the
  home page, timer, private writing, sort, titles and the open actions page.

## 0.1.0

- First release: live retro board, SQLite and Postgres, Docker, systemd and
  Kubernetes files.
