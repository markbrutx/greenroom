# Deploying Greenroom

Run one instance inside your network. Live updates live in memory, so never
run two replicas. SQLite is enough; use Postgres only if you already run it.

## systemd

```sh
sudo install -m 0755 greenroom_v0.2.0_linux_amd64 /usr/local/bin/greenroom
sudo useradd --system --home /var/lib/greenroom --shell /usr/sbin/nologin greenroom
sudo install -d -o greenroom -g greenroom -m 0750 /var/lib/greenroom
sudo install -D -m 0640 -g greenroom deploy/greenroom.env.example /etc/greenroom/greenroom.env
sudoedit /etc/greenroom/greenroom.env
sudo cp deploy/greenroom.service /etc/systemd/system/
sudo systemctl enable --now greenroom
```

## Docker

```sh
export GREENROOM_PASSWORD='team password'
docker compose -f deploy/docker-compose.yml up -d
```

`deploy/docker-compose.postgres.yml` adds Postgres. Kubernetes manifests are
in `deploy/k8s`. Pin the image to a version tag.

## Reverse proxy

Live updates use Server-Sent Events: turn off response buffering, allow long
reads and keep the `Host` header. Examples: `deploy/Caddyfile.example`
(`flush_interval -1`) and `deploy/nginx.conf.example` (`proxy_buffering off`).
Set `GREENROOM_TRUSTED_PROXIES` to the proxy address if it is not on the same
host.

## TLS

Use a certificate from your company CA in the proxy, or let Greenroom serve
HTTPS with `GREENROOM_TLS_CERT` and `GREENROOM_TLS_KEY`. Keep the hostname in
internal DNS and the port closed to the internet.

## Backups

```sh
greenroom backup /var/backups/greenroom/greenroom-$(date +%F).db
```

Run it daily with the service environment and copy the files off the host.
Postgres: `pg_dump -Fc`.

## Upgrade

Back up, replace the binary or image tag, restart. Migrations run on start
and only go forward, so roll back by restoring the backup with the old
version.
