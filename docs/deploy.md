# Deploying Moored

Run one instance. Live updates live in memory, so never run two replicas.
Data is one SQLite file.

## systemd

```sh
sudo install -m 0755 moored_v0.4.0_linux_amd64 /usr/local/bin/moored
sudo useradd --system --home /var/lib/moored --shell /usr/sbin/nologin moored
sudo install -d -o moored -g moored -m 0750 /var/lib/moored
sudo install -D -m 0640 -g moored deploy/moored.env.example /etc/moored/moored.env
sudoedit /etc/moored/moored.env
sudo cp deploy/moored.service /etc/systemd/system/
sudo systemctl enable --now moored
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
`MOORED_TRUSTED_PROXIES` to the proxy address if it is not on the same
host; per-IP rate limits for creating, joining and looking up rooms rely on
it.

## TLS

Serve Moored over HTTPS: room links and host links are the only keys to a
room. Terminate TLS in the proxy, or let Moored serve HTTPS with
`MOORED_TLS_CERT` and `MOORED_TLS_KEY`. "Copy link" and "Copy .md"
use the clipboard API, which browsers only allow on HTTPS or localhost; on
plain HTTP they fall back to selecting the text or downloading the file.

## Room lifetime

A room is deleted, with all its cards, after `MOORED_ROOM_TTL` (default
`168h`, 7 days) without a join or a write. The check runs on start and every
hour. Hosts can delete a room at any time.

## Backups

```sh
moored backup /var/backups/moored/moored-$(date +%F).db
```

Run it daily with the service environment and copy the files off the host.
Rooms are short-lived, so a backup mostly matters for rooms in progress.

## Upgrade

Back up, replace the binary or image tag, restart. Migrations run on start
and only go forward, so roll back by restoring the backup with the old
version.

From 0.3 to 0.4: Greenroom is now Moored. Rename every `GREENROOM_*`
variable to `MOORED_*`; 0.4 refuses to start while an old one is set. The
binary is `moored`, the image is `ghcr.io/markbrutx/moored`, and the default
database file is `moored.db`. With the Docker image, stop the container and
rename `/data/greenroom.db` to `/data/moored.db` in the volume, or set
`MOORED_DATABASE_URL=sqlite:/data/greenroom.db`. For systemd, move
`/etc/greenroom` and `/var/lib/greenroom` to the `moored` paths and install
the new unit file.

From 0.2 to 0.3: 0.3 starts with an empty database and refuses to open a 0.2
file. Keep the old file as a backup, point the database URL at a new file
(or move the old one aside), remove `GREENROOM_PASSWORD` from the
environment, and drop any Postgres settings.
