# Deploying Greenroom

Run one instance. Live updates live in memory, so never run two replicas.
Data is one SQLite file.

## systemd

```sh
sudo install -m 0755 greenroom_v0.3.0_linux_amd64 /usr/local/bin/greenroom
sudo useradd --system --home /var/lib/greenroom --shell /usr/sbin/nologin greenroom
sudo install -d -o greenroom -g greenroom -m 0750 /var/lib/greenroom
sudo install -D -m 0640 -g greenroom deploy/greenroom.env.example /etc/greenroom/greenroom.env
sudoedit /etc/greenroom/greenroom.env
sudo cp deploy/greenroom.service /etc/systemd/system/
sudo systemctl enable --now greenroom
```

## Docker

```sh
docker compose -f deploy/docker-compose.yml up -d
```

Kubernetes manifests are in `deploy/k8s`. Pin the image to a version tag.

## Reverse proxy

Live updates use Server-Sent Events: turn off response buffering, allow long
reads and keep the `Host` header, so shared links point at the right
hostname. Examples: `deploy/Caddyfile.example` (`flush_interval -1`) and
`deploy/nginx.conf.example` (`proxy_buffering off`). Set
`GREENROOM_TRUSTED_PROXIES` to the proxy address if it is not on the same
host; per-IP rate limits for creating, joining and looking up rooms rely on
it.

## TLS

Serve Greenroom over HTTPS: room links and host links are the only keys to a
room. Terminate TLS in the proxy, or let Greenroom serve HTTPS with
`GREENROOM_TLS_CERT` and `GREENROOM_TLS_KEY`. "Copy link" and "Copy .md"
use the clipboard API, which browsers only allow on HTTPS or localhost; on
plain HTTP they fall back to selecting the text or downloading the file.

## Room lifetime

A room is deleted, with all its cards, after `GREENROOM_ROOM_TTL` (default
`168h`, 7 days) without a join or a write. The check runs on start and every
hour. Hosts can delete a room at any time.

## Backups

```sh
greenroom backup /var/backups/greenroom/greenroom-$(date +%F).db
```

Run it daily with the service environment and copy the files off the host.
Rooms are short-lived, so a backup mostly matters for rooms in progress.

## Upgrade

Back up, replace the binary or image tag, restart. Migrations run on start
and only go forward, so roll back by restoring the backup with the old
version.

From 0.2 to 0.3: 0.3 starts with an empty database and refuses to open a 0.2
file. Keep the old file as a backup, point `GREENROOM_DATABASE_URL` at a new
file (or move the old one aside), remove `GREENROOM_PASSWORD` from the
environment, and drop any Postgres settings.
