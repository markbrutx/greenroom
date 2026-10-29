# Security

Report vulnerabilities privately through GitHub: **Security** tab, **Report a
vulnerability**. Fixes go into the latest release.

## Model

Moored has no accounts. A room link is the key to a room, so treat it like
a meeting link:

- Room codes are 10 random lowercase letters; the host link carries a separate
  128-bit secret. Both come from `crypto/rand`.
- In an *Ask to join* room nobody sees the board until the host admits them.
  In an *Open* room anyone with the link joins.
- Membership is a signed, HttpOnly, SameSite=Lax cookie per room (Secure on
  HTTPS). Removing someone revokes it on the server and closes their live
  connection.
- Creating rooms, joining and looking up unknown codes are rate limited per
  client IP. Set `MOORED_TRUSTED_PROXIES` correctly behind a proxy.
- Everyone in a room can edit every card. Data is not encrypted at rest; rooms
  are deleted after `MOORED_ROOM_TTL` without activity.

Serve it over HTTPS, and keep it inside your network unless you are fine with
anyone who gets a link joining an open room.
