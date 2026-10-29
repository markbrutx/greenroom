# Changelog

## 0.5.0

- Happiness check: everyone picks one of five faces, anonymously. Results
  open by themselves a few seconds after everyone in the room has voted, or
  when the host clicks Show results. The host can start a new vote.
  *Copy .md* includes the average and the spread.
- Hover a reaction to see who left it.

## 0.4.0

Breaking: Greenroom is now Moored, live at <https://moored.dev>. Rooms and
data carry over; see the [upgrade notes](docs/deploy.md#upgrade).

- The top bar says moored.dev; `MOORED_NAME` changes it.
- Environment variables are `MOORED_*`. Moored refuses to start while a
  `GREENROOM_*` variable is set and names the one to rename.
- The binary is `moored`, the image is `ghcr.io/markbrutx/moored`, the
  repository is `github.com/markbrutx/moored`, and the default database file
  is `moored.db`.
- Remembered rooms and the theme choice move over in the browser.

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
