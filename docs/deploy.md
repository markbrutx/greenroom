# Deploying Greenroom

For the person who runs Greenroom for a team inside a company network. All
settings are environment variables, listed in the README. Files mentioned
here live in `deploy/`.

Run exactly one instance. Live updates go through an in-memory hub, so two
replicas would not see each other's changes. Restarts are fine; browsers
reconnect on their own.

Use SQLite unless you already run and back up Postgres.

## Binary and systemd

```sh
VERSION=v0.2.0
curl -fLO https://github.com/markbrutx/greenroom/releases/download/${VERSION}/greenroom_${VERSION}_linux_amd64
curl -fLO https://github.com/markbrutx/greenroom/releases/download/${VERSION}/checksums.txt
sha256sum --ignore-missing -c checksums.txt
sudo install -m 0755 greenroom_${VERSION}_linux_amd64 /usr/local/bin/greenroom

sudo useradd --system --home /var/lib/greenroom --shell /usr/sbin/nologin greenroom
sudo install -d -o greenroom -g greenroom -m 0750 /var/lib/greenroom
sudo install -d -m 0755 /etc/greenroom
sudo install -m 0640 -o root -g greenroom deploy/greenroom.env.example /etc/greenroom/greenroom.env
sudoedit /etc/greenroom/greenroom.env
sudo cp deploy/greenroom.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now greenroom
journalctl -u greenroom -f
```

Set `GREENROOM_PASSWORD` in the env file. The unit is sandboxed: only
`/var/lib/greenroom` is writable and the process has no capabilities.

## Docker Compose

The image `ghcr.io/markbrutx/greenroom` is distroless, runs as uid 65532
with a read-only root filesystem and keeps data in `/data`.

```sh
export GREENROOM_PASSWORD='the team password'
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.postgres.yml up -d
```

The first file uses SQLite in the `greenroom-data` volume, the second adds a
Postgres container (set `POSTGRES_PASSWORD` too). Both publish the port on
`127.0.0.1:8080` for a reverse proxy on the same host and trust the Docker
networks as proxies. Pin the image to a version tag such as `0.2.0`.

## Kubernetes

`deploy/k8s` is a kustomization: one replica with the `Recreate` strategy
(the SQLite volume is ReadWriteOnce), probes on `/healthz` and `/readyz`, and
an example ingress with buffering off.

```sh
cp deploy/k8s/secret.example.yaml greenroom-secret.yaml
"${EDITOR:-vi}" greenroom-secret.yaml
kubectl apply -f greenroom-secret.yaml
kubectl apply -k deploy/k8s
```

Set `GREENROOM_TRUSTED_PROXIES` to the ingress controller's pod range.

## Reverse proxy

Live updates use Server-Sent Events: one long response per open retro. The
proxy must not buffer it and must not cut it after a short read timeout.
Greenroom sends `X-Accel-Buffering: no` and a ping every 20 seconds. The
proxy must also keep the original `Host` header, or every POST fails the
same-origin check with 403.

Caddy (`deploy/Caddyfile.example`):

```caddy
retro.example.internal {
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
	}
}
```

nginx (`deploy/nginx.conf.example`):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_read_timeout 3600s;
}
```

When the proxy is not on the same host, set `GREENROOM_TRUSTED_PROXIES` to
its address. Otherwise all users look like the proxy, and five wrong
passwords from one person block the whole team for 15 minutes.

## TLS with an internal CA

Internal hostnames cannot get public certificates. Ask your company CA for
one, use Caddy's `tls internal`, run step-ca, or use cert-manager with a CA
issuer in Kubernetes. Laptops must trust that root, which device management
usually handles. Without a proxy, Greenroom serves HTTPS itself with
`GREENROOM_TLS_CERT` and `GREENROOM_TLS_KEY` (PEM, full chain; restart after
renewal). The session cookie is marked `Secure` automatically over HTTPS.

## Keep it internal

Bind to `127.0.0.1` behind a local proxy or to a private interface, allow
the port only from office and VPN ranges, and publish the hostname only in
internal DNS. If your company has an SSO proxy in front of internal apps,
you can put Greenroom behind it as an extra gate; people still type their
name and the team password inside.

## Backups

SQLite: `greenroom backup FILE` writes a consistent copy while the server
runs and never overwrites a file. Run it with the same environment as the
service, for example from a daily systemd timer or cron, and copy the files
off the host.

```sh
sudo -u greenroom env GREENROOM_DATABASE_URL=sqlite:/var/lib/greenroom/greenroom.db \
  greenroom backup /var/backups/greenroom/greenroom-$(date +%F).db
```

To restore, stop the service, replace the database file, delete the old
`-wal` and `-shm` files next to it and start again. In Docker, run the backup
with `docker compose exec greenroom greenroom backup /data/backup.db` and
copy it out with `docker compose cp`.

Postgres: use `pg_dump -Fc` and `pg_restore --clean`.

## Upgrades and rollback

Migrations run at start, once each, forward only. Back up right before an
upgrade, replace the binary or the image tag, restart and check the log. To
roll back, restore that backup together with the old version; never run an
old version against an upgraded database.

## Security checklist

- [ ] HTTPS only, through the proxy or built-in TLS.
- [ ] A strong team password, shared only with the team.
- [ ] Not reachable from the internet, no public DNS name.
- [ ] `GREENROOM_TRUSTED_PROXIES` lists only your proxy.
- [ ] Daily backups copied off the host, a restore tested once.
- [ ] A backup before every upgrade.

## Personal data

Greenroom stores display names with the time each person was last seen, card
texts with their authors, action owners, reactions with who reacted, and a
random session key in the `settings` table. No emails, no IP addresses (they
live in memory for the rate limit and appear in the sign-in log line). Two
cookies: `greenroom_session` (signed name and expiry) and `greenroom_name`
(the last name used, to prefill the sign-in form). Deleted cards stay as
rows with `deleted` set; to erase a person, rename them in `cards`, delete
their `reactions` and their row in `people` with SQL while the service is
stopped.
