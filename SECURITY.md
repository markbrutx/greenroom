# Security

## Supported versions

Security fixes go into the latest minor release. Upgrade to it to get them.
Older minor versions do not get backports.

## Reporting a vulnerability

Please report vulnerabilities privately, not in a public issue.

Use GitHub private vulnerability reporting: open
[github.com/markbrutx/greenroom](https://github.com/markbrutx/greenroom), go
to the **Security** tab and click **Report a vulnerability**. There is no
security email address.

Include the version (`greenroom version`), the database type and the steps
to reproduce.

What to expect:

- an acknowledgement within a few days;
- a fix in a new release once the issue is confirmed;
- a published security advisory that credits you, unless you prefer not to
  be named.

## Security model

Greenroom is a small tool for one team inside a company network. The design
keeps the attack surface small and the defaults strict.

**The server decides everything.** The browser sends intents only ("add a
card", "react with 👍"). The server checks the user, validates the input and
writes the result. Values computed in the browser are never trusted.

**Strict Content-Security-Policy.** Every response carries this policy. There
are no inline scripts or inline styles, and all assets come from the binary
itself:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'
```

Responses also set `X-Content-Type-Options: nosniff`,
`X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
`Cross-Origin-Opener-Policy: same-origin`, a restrictive
`Permissions-Policy`, and `Cache-Control: no-store` for pages and API calls.

**Cross-origin protection on every POST.** All requests pass through Go's
`net/http` `CrossOriginProtection`. It rejects state-changing requests from
another origin by checking `Sec-Fetch-Site`, or by comparing `Origin` with
`Host` when the browser does not send `Sec-Fetch-Site`. On top of that, every
non-GET JSON call must carry the custom header `X-Greenroom: 1`, which a
cross-site form cannot set.

**Signed session cookie.** The session cookie `greenroom_session` holds the
name and an expiry, signed with HMAC-SHA256. It is `HttpOnly`,
`SameSite=Lax`, `Secure` when the request came over HTTPS or with
`X-Forwarded-Proto: https`, and lasts 30 days. The signing key is
HMAC(stored key, team password): the stored key is 32 random bytes created
on first start and kept in the `settings` table. Changing
`GREENROOM_PASSWORD` signs everyone out; deleting the `session_key` row does
the same after a restart.

**Password check.** The team password comes from `GREENROOM_PASSWORD` and
is compared in constant time. It is never written to the database or the
logs.

**Login rate limit.** After 5 failed sign-ins from one client IP within 15
minutes, that IP is blocked for 15 minutes. The counter lives in memory and
resets on restart. The client IP is read from `X-Forwarded-For` only when
the peer is in `GREENROOM_TRUSTED_PROXIES` (loopback by default).

**Request limits.** Request bodies are capped (4 KB for forms, at most 16 KB
for JSON calls), request headers at 64 KB, and slow headers time out after 10
seconds.

**Not indexed.** Every response sends `X-Robots-Tag: noindex, nofollow`, and
`/robots.txt` disallows everything.

**No outbound connections.** Greenroom never calls out: no telemetry, no
update checks, no CDN, no external fonts.

## Known limitations

These are deliberate trade-offs. Plan your deployment around them.

- **One shared team password.** There are no per-user passwords. Anyone
  with the password can sign in under any name. Keep Greenroom inside your
  network; an SSO proxy in front can add a second gate.
- **Everyone can edit everything.** A retro is a team whiteboard. Anyone
  signed in can edit or delete any card.
- **No encryption at rest.** Data is stored unencrypted in the database. Use
  disk encryption on the server and protect backups the same way.
- **No audit log.** Greenroom does not record who changed or deleted what.
  Deleted cards are kept as soft-deleted rows, but without a history of
  edits.
