# Greenroom

Meet-style retro rooms in one self-hosted binary.

![A retro room in Greenroom](docs/img/retro.png)

## How it works

1. **Create a room.** Type your name and pick who can come in: *Ask to join*
   (default) or *Open*. You get a link like `https://retro.example.com/abc-defg-hij`.
2. **Share the link.** People type their name. In an *Ask to join* room you see
   "Maya wants to join" and click Admit or Deny; they get in without
   reloading.
3. **Run the retro.** Warm-up question, board columns from a template set,
   cards, reactions and actions with an owner, all live.
4. **Export for your agent.** *Copy for agent* puts the whole retro on the
   clipboard as Markdown, ready for a prompt; *Download .md* saves it.
5. **Delete it.** The host deletes the room for everyone. Rooms also delete
   themselves after 7 days without activity.

No accounts and no passwords. The host can switch the room between open and
closed, remove people, and open the room as host on another device with a
private host link.

## Run

```sh
docker run -d -p 8080:8080 -v greenroom:/data ghcr.io/markbrutx/greenroom
```

```sh
./greenroom
```

Then open <http://localhost:8080>. Version 0.3 starts with an empty database;
see the [changelog](CHANGELOG.md).

## Config

| Variable | Default | |
| --- | --- | --- |
| `GREENROOM_ADDR` | `:8080` | Listen address |
| `GREENROOM_DATABASE_URL` | `sqlite:greenroom.db` | SQLite file |
| `GREENROOM_ROOM_TTL` | `168h` | Delete rooms after this long without activity |
| `GREENROOM_TRUSTED_PROXIES` | `127.0.0.1,::1` | Proxies allowed to set `X-Forwarded-For` |
| `GREENROOM_TLS_CERT`, `GREENROOM_TLS_KEY` | | Serve HTTPS directly |
| `GREENROOM_NAME` | `Greenroom` | Name in the top bar |
| `GREENROOM_LOGO` | built-in | Logo file: SVG, PNG, WebP, JPEG or ICO |
| `GREENROOM_ACCENT` | `#3e8342` | Accent color |
| `GREENROOM_TZ` | `UTC` | Time zone for dates |
| `GREENROOM_TEMPLATES` | built-in | Your own [pool.toml](internal/retro/pool.toml) |
| `GREENROOM_LOG_LEVEL`, `GREENROOM_LOG_FORMAT` | `info`, `text` | Logging |

Production setup: [docs/deploy.md](docs/deploy.md). Security notes:
[SECURITY.md](SECURITY.md).

License: MIT
