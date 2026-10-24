# Deploying Greenroom

This guide is for the person who runs Greenroom for a team inside a company
network. It assumes a team of about ten people, one server or one container,
and an existing way to reach internal services (VPN, office network or SSO
proxy).

All settings are `GREENROOM_*` environment variables. The full list is in the
README. The files referenced below live in the `deploy/` folder of the
repository.

## 1. Before you start

Greenroom is one static binary that runs as one process. It serves the pages,
the API and the live updates itself. There is no worker, no cache server and
no build step.

Run exactly one instance. Live updates go through an in-memory hub inside the
process. Two replicas, or two instances behind a load balancer, would each
have their own hub, and people on different instances would stop seeing each
other's changes. Restart is fine; scaling out is not.

Pick a database:

- **SQLite** is the default. Zero setup: it is one file on disk
  (`GREENROOM_DATABASE_URL=sqlite:/var/lib/greenroom/greenroom.db`). Back up
  that file with `greenroom backup`. This is the right choice for almost every
  team.
- **Postgres** makes sense if you already run a Postgres server and already
  back it up. Use
  `GREENROOM_DATABASE_URL=postgres://user:pass@host:5432/greenroom?sslmode=require`.

Pick an auth mode with `GREENROOM_AUTH`:

| Mode | Who signs in | Use it when |
| --- | --- | --- |
| `password` (default) | Anyone who knows the shared team password, under the name they type | You have no SSO proxy, or you want the simplest setup |
| `header` | Whoever your SSO proxy says the user is | You already run an SSO proxy that passes the user in a header |
| `open` | Anyone who can reach the server, under any name | A closed lab network with no other users |

Short guide: if your company has an SSO proxy in front of internal apps, use
`header`. Otherwise use `password`. Use `open` only when the network itself
is the access control and nobody outside the team is on it.

In `password` and `open` mode you can also set `GREENROOM_USERS` to a comma
list of names. Then only those names can sign in.

## 2. Generate secrets

`GREENROOM_SECRET` signs the session cookies. It must be at least 32 bytes.
Generate one:

```sh
openssl rand -hex 32
```

If you leave it unset, Greenroom uses a random key and everybody has to sign
in again after each restart. Store the value like a password. In `header`
mode there are no session cookies and the secret is not needed.

`GREENROOM_PASSWORD_HASH` is required in `password` mode. Greenroom never
stores the team password itself, only an argon2id hash. The password must be
at least 8 characters. Create the hash with the binary:

```sh
printf '%s\n' 'the team password' | greenroom hash
```

Or with the image, without installing anything:

```sh
printf '%s\n' 'the team password' | docker run --rm -i ghcr.io/markbrutx/greenroom hash
```

The output looks like `$argon2id$v=19$m=65536,t=2,p=1$...`. It contains `$`
signs, so quote it correctly:

- In a shell, use single quotes: `export GREENROOM_PASSWORD_HASH='$argon2id$v=19$...'`.
  Double quotes would expand the `$` parts and break the hash.
- In a docker compose file, if you write the hash literally in the YAML,
  double every `$` as `$$`. Compose interpolates single `$` signs.
- A systemd `EnvironmentFile` takes the value as is. No quotes, no escaping.

Changing the password hash signs everybody out, because the session signing
key is derived from it. That is also how you revoke all sessions at once:
set a new team password.

## 3. Binary and systemd

Download the binary for your platform from GitHub Releases at
`https://github.com/markbrutx/greenroom/releases`. Files are named
`greenroom_<version>_linux_amd64` (and the same for other platforms), with a
`checksums.txt` next to them. The version in the file name is the release
tag, for example `v1.0.0`. Replace `VERSION` with the release you want:

```sh
VERSION=v1.0.0
curl -fLO https://github.com/markbrutx/greenroom/releases/download/${VERSION}/greenroom_${VERSION}_linux_amd64
curl -fLO https://github.com/markbrutx/greenroom/releases/download/${VERSION}/checksums.txt
sha256sum --ignore-missing -c checksums.txt
sudo install -m 0755 greenroom_${VERSION}_linux_amd64 /usr/local/bin/greenroom
greenroom version
```

Create a system user with no login shell. Its home is the data directory:

```sh
sudo useradd --system --home /var/lib/greenroom --shell /usr/sbin/nologin greenroom
sudo install -d -o greenroom -g greenroom -m 0750 /var/lib/greenroom
```

Create the config from the example. The file holds the secret and the
password hash, so only root and the service can read it:

```sh
sudo install -d -m 0755 /etc/greenroom
sudo install -m 0640 -o root -g greenroom deploy/greenroom.env.example /etc/greenroom/greenroom.env
sudoedit /etc/greenroom/greenroom.env
```

A minimal file for password mode with SQLite:

```ini
GREENROOM_ADDR=127.0.0.1:8080
GREENROOM_DATABASE_URL=sqlite:/var/lib/greenroom/greenroom.db
GREENROOM_AUTH=password
GREENROOM_PASSWORD_HASH=$argon2id$v=19$m=65536,t=2,p=1$...
GREENROOM_SECRET=paste-the-openssl-output-here
GREENROOM_TZ=Europe/Berlin
```

Install the unit and start it:

```sh
sudo cp deploy/greenroom.service /etc/systemd/system/greenroom.service
sudo systemctl daemon-reload && sudo systemctl enable --now greenroom
journalctl -u greenroom -f
```

The log shows `greenroom started` with the address, database type and auth
mode. If the config is wrong, the service stops at start with one line that
names the variable.

The unit is sandboxed. The system is mounted read-only for the service, only
`/var/lib/greenroom` is writable, and the process runs as the `greenroom` user
with no capabilities. If you move the database, update the writable path in
the unit too.

## 4. Docker

The image is `ghcr.io/markbrutx/greenroom`. It is distroless, runs as the
nonroot user (uid 65532) with a read-only root filesystem, and keeps its data
in `/data`. There is no shell inside. The container healthcheck runs
`greenroom health`, which asks `/healthz` on the local port.

`deploy/docker-compose.yml` runs Greenroom with SQLite in the volume
`greenroom-data` and publishes the port on `127.0.0.1:8080` only, so it is
reachable through a reverse proxy on the same host and not from the network.
Put your secret and password hash into it (remember `$$` in the hash), then:

```sh
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml logs -f
```

`deploy/docker-compose.postgres.yml` runs Greenroom together with a Postgres
container:

```sh
docker compose -f deploy/docker-compose.postgres.yml up -d
```

The compose files use the `latest` tag. Change it to a version tag such as
`ghcr.io/markbrutx/greenroom:1.0.0` (image tags have no `v`), so an upgrade
happens only when you decide.

## 5. Kubernetes

The manifests in `deploy/k8s` are a kustomization. Create the secret first.
Copy the example, fill in `GREENROOM_SECRET` and `GREENROOM_PASSWORD_HASH`,
and apply it. In YAML the hash can be written as is inside single quotes.

```sh
cp deploy/k8s/secret.example.yaml greenroom-secret.yaml
"${EDITOR:-vi}" greenroom-secret.yaml
kubectl apply -f greenroom-secret.yaml
kubectl apply -k deploy/k8s
kubectl rollout status deployment/greenroom
```

Do not commit the filled-in secret file.

The deployment runs 1 replica with the `Recreate` strategy. One replica,
because the live hub is in memory (see section 1). `Recreate`, because the
SQLite file sits on a ReadWriteOnce volume: a rolling update would start the
new pod while the old one still holds the volume, and the new pod would hang
waiting for it. With `Recreate` the old pod stops first, then the new one
starts. Expect a few seconds of downtime on each rollout; browsers reconnect
on their own.

Probes: liveness on `/healthz` (the process answers), readiness on `/readyz`
(the database answers a ping).

`deploy/k8s/ingress.example.yaml` exposes the service through an ingress
with settings for Server-Sent Events: response buffering off and a long read
timeout. The annotations are for the ingress-nginx controller; with another
controller, set the equivalent options. Section 6 explains why these settings matter.

## 6. Reverse proxy

Put a reverse proxy in front of Greenroom. It gives you:

- TLS with your company certificate;
- a place to plug in SSO (section 8);
- one hostname on port 443 for everybody.

Two things need care.

Live updates use Server-Sent Events: one long HTTP response per open retro
that the server keeps writing to. The proxy must not buffer that response,
and must not cut it off after its default read timeout. Greenroom helps on
both sides: it sends `X-Accel-Buffering: no` on the stream and writes a ping
every 20 seconds, so an idle stream never looks dead. If live updates arrive
late or in bursts, buffering is on somewhere.

The proxy must pass the original `Host` header. Every POST goes through
cross-origin protection that compares the browser's `Origin` with `Host`.
If the proxy rewrites `Host` to `127.0.0.1:8080`, every form and every card
edit fails with 403.

Also set `GREENROOM_TRUSTED_PROXIES` to the proxy's address, even in password
mode. Greenroom then reads the client IP from `X-Forwarded-For` for the login
rate limit. Without it, all users look like the proxy, and five wrong
passwords from one person block the whole team for 15 minutes.

### Caddy

`deploy/Caddyfile.example` shows the full setup. The core of it:

```caddy
retro.example.internal {
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
	}
}
```

Caddy keeps `Host` and sets `X-Forwarded-For` and `X-Forwarded-Proto` by
default. `flush_interval -1` sends every write to the browser immediately.

### nginx

`deploy/nginx.conf.example` shows the full setup. The core of it:

```nginx
server {
    listen 443 ssl;
    server_name retro.example.internal;
    ssl_certificate /etc/ssl/greenroom/fullchain.pem;
    ssl_certificate_key /etc/ssl/greenroom/privkey.pem;

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
}
```

### Direct TLS without a proxy

If you do not want a proxy, Greenroom can serve HTTPS itself. Point it at a
PEM certificate (full chain) and key that the service user can read:

```ini
GREENROOM_ADDR=:8443
GREENROOM_TLS_CERT=/etc/greenroom/tls/fullchain.pem
GREENROOM_TLS_KEY=/etc/greenroom/tls/privkey.pem
```

Set both or neither. The files are read at start, so restart the service
after you renew the certificate. `greenroom health` switches to HTTPS on its
own when `GREENROOM_TLS_CERT` is set.

## 7. TLS with an internal CA

Internal hostnames usually cannot get public certificates. Use your company
CA instead:

- ask your corporate CA for a certificate for the internal hostname, for
  example `retro.example.internal`;
- or let Caddy issue one from its own local CA with `tls internal`;
- or run step-ca as a small internal ACME server;
- in Kubernetes, use cert-manager with a CA issuer that holds your internal
  CA.

Browsers must trust the CA. In most companies the root CA is already pushed
to laptops by device management. If you use Caddy's local CA or a new
step-ca, you have to distribute its root certificate yourself.

You do not need to configure the cookie. The session cookie gets the
`Secure` flag automatically when the request arrived over HTTPS, or when the
proxy sends `X-Forwarded-Proto: https`.

## 8. Single sign-on with header auth

In header mode Greenroom shows no login form. Your SSO proxy authenticates
the user and passes the identity in a request header. Greenroom believes that
header only from the proxy.

```ini
GREENROOM_AUTH=header
GREENROOM_AUTH_HEADER=X-Forwarded-User
GREENROOM_AUTH_NAME_HEADER=
GREENROOM_TRUSTED_PROXIES=127.0.0.1
GREENROOM_ADDR=127.0.0.1:8080
```

- `GREENROOM_AUTH_HEADER` is the identity header, `X-Forwarded-User` by
  default. It is usually an email or a user name.
- `GREENROOM_AUTH_NAME_HEADER` is optional. If set and the proxy sends it,
  its value becomes the name shown on cards, for example "Dana Kim" instead of
  `dana.kim@example.com`.
- `GREENROOM_TRUSTED_PROXIES` is required in header mode: a comma list of
  IPs or CIDRs. A request from any other peer gets 401, whatever headers it
  carries.

The header is only as trustworthy as the network path. Anyone who can reach
Greenroom directly from a trusted address can send any name. So bind
Greenroom to localhost (proxy on the same host) or to a private interface
that only the proxy can reach, and keep the trusted list as narrow as
possible. In Docker or Kubernetes, trust the proxy container's network or the
ingress controller's pod range, not the whole company network.

`GREENROOM_USERS` turns on an allow-list, for example
`GREENROOM_USERS=dana.kim@example.com,Lee Park`. Matching is
case-insensitive. A user not on the list gets 403. When the allow-list is
set, the spelling in the list is the display name, and
`GREENROOM_AUTH_NAME_HEADER` is ignored.

The recipes below give the header names each proxy uses. Configuration
details change between versions, so check your proxy's own documentation for
the exact option names.

### oauth2-proxy

Behind nginx with `auth_request`, start oauth2-proxy with
`--set-xauthrequest`. It then returns `X-Auth-Request-User` and
`X-Auth-Request-Email` to nginx, and nginx must copy them onto the request
with `auth_request_set` and `proxy_set_header`.

```ini
GREENROOM_AUTH=header
GREENROOM_AUTH_HEADER=X-Auth-Request-Email
GREENROOM_AUTH_NAME_HEADER=X-Auth-Request-User
GREENROOM_TRUSTED_PROXIES=127.0.0.1
```

When oauth2-proxy itself is the reverse proxy (`--upstream`), it passes
`X-Forwarded-User` and `X-Forwarded-Email` with `--pass-user-headers`.
Use `GREENROOM_AUTH_HEADER=X-Forwarded-Email` and trust oauth2-proxy's
address.

### Authelia

Authelia works through forward auth in Caddy (`forward_auth`), nginx
(`auth_request`) or Traefik (`forwardAuth`). It returns `Remote-User`,
`Remote-Name`, `Remote-Email` and `Remote-Groups`; configure the proxy to copy
`Remote-User` and `Remote-Name` onto the upstream request. The forward auth
endpoint path differs between Authelia versions.

```ini
GREENROOM_AUTH=header
GREENROOM_AUTH_HEADER=Remote-User
GREENROOM_AUTH_NAME_HEADER=Remote-Name
GREENROOM_TRUSTED_PROXIES=127.0.0.1
```

### Pomerium

Pomerium is the proxy itself. On the route to Greenroom, turn on
`pass_identity_headers`, and map the email claim to a header with
`jwt_claims_headers` (for example `X-Pomerium-Claim-Email: email`).

```ini
GREENROOM_AUTH=header
GREENROOM_AUTH_HEADER=X-Pomerium-Claim-Email
GREENROOM_TRUSTED_PROXIES=10.0.0.5
```

Replace `10.0.0.5` with the address Pomerium connects from.

### Cloudflare Access

Cloudflare Access adds `Cf-Access-Authenticated-User-Email` to each request.
That header is only trustworthy if the origin accepts traffic from nowhere
except the Cloudflare tunnel. Run `cloudflared` on the same host, bind
Greenroom to localhost, and trust only localhost:

```ini
GREENROOM_AUTH=header
GREENROOM_AUTH_HEADER=Cf-Access-Authenticated-User-Email
GREENROOM_TRUSTED_PROXIES=127.0.0.1
GREENROOM_ADDR=127.0.0.1:8080
```

### Microsoft Entra application proxy

Entra application proxy does not inject identity headers by default. Two
options:

- configure header-based single sign-on for the application in Entra and
  point `GREENROOM_AUTH_HEADER` at the header you map the user's email to,
  with `GREENROOM_TRUSTED_PROXIES` set to the connector host;
- or keep `GREENROOM_AUTH=password` and use the application proxy only for
  access control and TLS.

## 9. Intranet or VPN only

Greenroom is built for internal use. Keep it off the public internet:

- bind it to a private interface, for example `GREENROOM_ADDR=10.20.0.15:8080`,
  or to `127.0.0.1` behind a local proxy;
- allow the port in the host firewall only from the office or VPN ranges;
- publish the hostname only in internal DNS, never in public DNS.

`open` mode has no password at all: anyone who can reach the server can sign
in under any name, including someone else's. Use it only on a closed lab
network. Greenroom logs a warning at every start in this mode.

## 10. Backups

### SQLite

`greenroom backup FILE` writes a consistent copy of the database with
SQLite's `VACUUM INTO`. It is safe while the server is running. It refuses to
overwrite an existing file, so use a new name each time. It reads
`GREENROOM_DATABASE_URL` to find the database, so run it with the same
environment as the service:

```sh
sudo install -d -o greenroom -g greenroom -m 0700 /var/backups/greenroom
sudo -u greenroom env GREENROOM_DATABASE_URL=sqlite:/var/lib/greenroom/greenroom.db greenroom backup /var/backups/greenroom/greenroom-$(date +%F).db
```

A daily systemd timer. In unit files `$$` stands for `$` and `%%` for `%`.
The second command keeps the last 30 days of backups.

`/etc/systemd/system/greenroom-backup.service`:

```ini
[Unit]
Description=Greenroom database backup

[Service]
Type=oneshot
User=greenroom
Group=greenroom
EnvironmentFile=/etc/greenroom/greenroom.env
ExecStart=/bin/sh -c 'exec /usr/local/bin/greenroom backup /var/backups/greenroom/greenroom-$$(date +%%F).db'
ExecStartPost=/usr/bin/find /var/backups/greenroom -name 'greenroom-*.db' -mtime +30 -delete
```

`/etc/systemd/system/greenroom-backup.timer`:

```ini
[Unit]
Description=Daily Greenroom database backup

[Timer]
OnCalendar=*-*-* 02:30:00
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now greenroom-backup.timer
systemctl list-timers greenroom-backup.timer
```

If you prefer cron, a line in `/etc/cron.d/greenroom` does the same. In cron
files `%` must be written as `\%`:

```
30 2 * * * greenroom GREENROOM_DATABASE_URL=sqlite:/var/lib/greenroom/greenroom.db /usr/local/bin/greenroom backup /var/backups/greenroom/greenroom-$(date +\%F).db
```

Copy the backups off the host with your usual backup tool.

To restore, stop the service, put the backup in place of the database file,
remove the old `-wal` and `-shm` files next to it, and start again:

```sh
sudo systemctl stop greenroom
sudo mv /var/lib/greenroom/greenroom.db /var/lib/greenroom/greenroom.db.broken
sudo rm -f /var/lib/greenroom/greenroom.db-wal /var/lib/greenroom/greenroom.db-shm
sudo install -o greenroom -g greenroom -m 0640 /var/backups/greenroom/greenroom-2026-01-15.db /var/lib/greenroom/greenroom.db
sudo systemctl start greenroom
```

Test a restore once on a spare machine, so you know the backups work.

### Postgres

`greenroom backup` refuses to run on Postgres and tells you to use
`pg_dump`. Use the custom format:

```sh
pg_dump -Fc -f greenroom-$(date +%F).dump 'postgres://user:pass@host:5432/greenroom'
```

Restore into the same database with Greenroom stopped:

```sh
pg_restore --clean --if-exists -d 'postgres://user:pass@host:5432/greenroom' greenroom-2026-01-15.dump
```

### Docker

Run the backup inside the container, then copy the file out:

```sh
docker compose -f deploy/docker-compose.yml exec greenroom greenroom backup /data/backup-$(date +%F).db
docker compose -f deploy/docker-compose.yml cp greenroom:/data/backup-$(date +%F).db ./greenroom-$(date +%F).db
```

`docker cp <container>:/data/... .` works the same. The image has no shell,
so the copy inside the volume stays there. Remove old copies from time to
time with a throwaway container that mounts the same volume (find its name
with `docker volume ls`).

## 11. Upgrades and rollback

Database migrations run automatically at start. Each one runs in a
transaction and is recorded in the `schema_migrations` table, so it runs
once. Migrations are forward-only: there are no down migrations, and an older
binary does not undo a newer schema.

So always back up right before an upgrade:

```sh
sudo -u greenroom env GREENROOM_DATABASE_URL=sqlite:/var/lib/greenroom/greenroom.db greenroom backup /var/backups/greenroom/pre-upgrade-$(date +%F).db
sudo install -m 0755 greenroom_v1.1.0_linux_amd64 /usr/local/bin/greenroom
sudo systemctl restart greenroom
journalctl -u greenroom -n 20
```

To roll back, restore the backup taken before the upgrade (section 10)
together with the old binary or the old image tag. Do not run the old
version against the upgraded database.

With Docker and Kubernetes, pin image tags to versions and change the tag on
purpose. Read the release notes before each upgrade.

## 12. Resource footprint

- One static binary of about 20 MB. No runtime dependencies, no CGO.
- Memory: typically under 50 MB for a team of 10.
- CPU: negligible. The only noticeable work is the password check at sign-in
  (argon2id, 64 MiB for a moment; at most 2 run at a time).
- Disk: the SQLite file grows slowly, because it holds only text.
- Network: Greenroom makes no outbound connections. No telemetry, no update
  checks, no CDN, no external fonts. Every asset is inside the binary.

## 13. Data and privacy

These notes help with a GDPR review. Greenroom stores:

- **people**: the display name and the last time each person was seen;
- **retros**: title, creation time, warm-up question, column set, timer and
  the private writing setting;
- **cards**: the text, the author's name, the action owner's name, done
  state and timestamps;
- **reactions**: card, the name of who reacted, the emoji
  (👍 ❤️ 😂 🎉 🤔 😮) and the time.

Greenroom stores no emails, unless your SSO header sends an email as the
identity and you set no display-name header. It stores no IP addresses. IPs
are kept in memory only, for the login rate limit, and the client IP appears
in the log line written at each form sign-in.

Cookies: one session cookie, `greenroom_session`, HttpOnly, holding the name
and an expiry. No tracking, no analytics, no third-party cookies.

Deleting a card in the app does not erase it. The row stays with the
`deleted` flag set, so to erase a person's data an admin runs SQL. Stop the
service and take a backup first. With SQLite, use the `sqlite3` shell and
turn on foreign keys in that session, otherwise the cascades below do not
run:

```sh
sudo systemctl stop greenroom
sudo -u greenroom sqlite3 /var/lib/greenroom/greenroom.db
```

```sql
pragma foreign_keys = on;
delete from reactions where who = 'Dana Kim';
update cards set author = 'former member' where author = 'Dana Kim';
update cards set owner = 'former member' where owner = 'Dana Kim';
delete from people where key = lower('Dana Kim');
```

The `people` key is the lowercased name. SQLite's `lower()` only lowercases
ASCII letters, so for names with other letters look the key up with
`select key, name from people;` and delete by that value.

To purge cards that were deleted in the app (their reactions go with them):

```sql
delete from cards where deleted;
```

Retention: data is kept until you delete it. If your policy needs a limit,
purge old retros periodically. Deleting a retro cascades to its cards and
their reactions. `created_at` is Unix time in milliseconds:

```sh
date -d '1 year ago' +%s000
```

```sql
delete from retros where created_at < 1735689600000;
```

On Postgres the same statements work in `psql`, and foreign keys are always
enforced.

## 14. Air-gapped builds

Greenroom never downloads anything at runtime. To get it into a network
without internet access, pick one way:

Build from source with vendored modules. On a connected machine:

```sh
git clone https://github.com/markbrutx/greenroom.git
cd greenroom
go mod vendor
```

Copy the whole folder in, then on the offline machine with the same Go
version:

```sh
CGO_ENABLED=0 go build -mod=vendor -trimpath -o greenroom ./cmd/greenroom
```

Or copy the prebuilt static binary together with `checksums.txt` and verify
it with `sha256sum -c` inside.

Or move the image:

```sh
docker pull ghcr.io/markbrutx/greenroom:1.0.0
docker save -o greenroom-1.0.0.tar ghcr.io/markbrutx/greenroom:1.0.0
docker load -i greenroom-1.0.0.tar
```

## 15. Security checklist

- [ ] Users reach Greenroom over HTTPS only.
- [ ] Password mode uses a strong team password, or header mode uses your SSO.
- [ ] `GREENROOM_SECRET` is set and stored like a password (not needed in header mode).
- [ ] Header mode: `GREENROOM_TRUSTED_PROXIES` lists only the proxy, and Greenroom is not reachable directly.
- [ ] Greenroom is not exposed to the internet and has no public DNS name.
- [ ] Backups run daily, are copied off the host, and a restore was tested.
- [ ] Logs are collected (`GREENROOM_LOG_FORMAT=json` helps log shippers).
- [ ] Someone watches the releases and upgrades, with a backup before each upgrade.
- [ ] The service runs as a non-root user (the systemd unit and the image already do).
- [ ] `open` mode is never used on a shared network.
