# One session per room, on every transport

Status: built. Supersedes the placement in `link-one-connection-design.md` (the mux there belonged to an attachment).
Its reasoning about the window, the write timeout and the stream cap stands and is used here.

## The contract

A room holds ONE network session to the hub for each way it can reach it. The session is opened by the first dial
and opened again only when it dies. Every connection the link needs (control, the warm data pool, relay, announce, git,
room spec, upgrade, ssh stream) is a stream or circuit ON that session, the way the OpenZiti tunneler carries every TCP
flow over one ziti connection. All three transports keep the one interface, `Dialer.Dial(ctx) (net.Conn, error)`
(`internal/link/link.go`). Nothing above a transport knows sessions exist.

| Transport | The session | A Dial | Reset |
| --- | --- | --- | --- |
| direct | one mutual TLS connection per hub address, yamux on it (`mux.go`) | opens a yamux stream | none needed. a dead session is noticed and the next dial opens one |
| ziti | the process's one ziti context for the identity (`internal/zitictx`) | dials a circuit on it | logs the context in again |
| zrok | the process's one ziti context for the zrok environment's identity | dials the share token on it | logs the context in again |

A session is never built per dial, never built again while one is alive, and an overlay context is never closed
while the process runs.

## Why: the incident

On sgg the zrok room held about 16,290 established `:443` connections, which used up Windows' 16,384 ephemeral ports
and took the other room on the machine down with it. `zroksdk.NewDialer` (zrok v2.0.4) loads the identity, builds a
ziti context and dials on it, and that context is never closed, whether the dial worked or not. Each context logs in
to the controller and keeps a TLS connection to every edge router. A room redials on a backoff, so each retry was a
handful of sockets that never went away. The ziti transport had the same bug and was fixed by `internal/zitictx`.
zrok now uses it too: the share token is a ziti service, dialled with `DialWithOptions` on the shared context.

## Direct: why yamux

- A yamux stream is a `net.Conn` with deadlines and half close, so the hello, the pool, HTTP/1.1, websockets and the
  raw ssh pipe run on a stream unchanged.
- HTTP/2 was considered and does not fit. The link is not only HTTP: the control connection is line framed for its
  whole life and the ssh stream is raw bytes. Carrying those over HTTP/2 needs extended CONNECT on both sides and a
  second representation of every kind, which the package comment rules out.
- `xtaci/smux` does the same job with a smaller user base and nothing yamux lacks here.

Settings (`muxConfig`): the window stays at 256 KiB, because a control frame can wait behind every bulk stream's
window and a larger one turns a big download into a missed beat. `ConnectionWriteTimeout` is `Silence`. Keepalive is
on at 15 s, because the session belongs to the dialer and is shared by the control link, the probes and every other
kind, so it has to notice its own death rather than wait for one of them to. The hub caps a session at 256 open
streams.

## Direct: the gate for a mixed deploy

After the TLS handshake the room sends `hello{kind: "mux"}`. A new hub answers OK and runs yamux on the rest of the
connection. Each stream it accepts goes to the hub's accept loop as a connection of its own and starts with its own
hello, read by the code that always read one.

- **New room, old hub.** The old hub refuses an unknown kind, as it always has. The room logs it once, dials one
  connection per Dial as before, and asks again after `muxRetry` (10 minutes).
- **Old room, new hub.** It never says `mux`. The hub peeks the first line, sees another kind, and hands the connection
  on unread.
- **A room without a certificate** (joining) never asks, and a hub refuses the mux to a connection without one.
- `Version` stays 1. The mux is a kind, negotiated per connection, so nothing older is refused for not having it.

## Direct: who the room is

A stream carries the TLS state of the connection under it (`muxStream.ConnectionState`), and `peerCert` and
`DirectAuthenticated` read any connection that has one. So every stream is named by the hub-signed certificate the
session presented, and a stream cannot claim another room. A stream that says `mux` or `enrol` is refused.

## What a session's death costs

Every stream on it ends together, which is how a ziti context behaves already. The control stream fails at once, so
the room attaches again on its backoff, and the first dial opens a new session. Callers see a reset, as they did on a
broken socket.

## Probes

The per path probe and the climb probe dial every rung through the same `Dial`. On direct that is a stream on the
rung's session, so a room standing on an overlay holds one direct session per probed address as well. That is one
socket per rung, flat, and it ends 15 to 25 s after the path does.

## The test

`TestEveryTransportHoldsOneSessionAcrossDialsClosesAndReconnects` (`internal/link`, integration): for each transport,
1,200 dials and closes across three forced reconnects. Direct counts TCP connections the hub accepted (one per
reconnect, plus the first) and sessions the room opened. Ziti and zrok count contexts built through a fake SDK (one,
never closed). `mux_integration_test.go` covers the mixed deploy both ways and the certificate rules.
