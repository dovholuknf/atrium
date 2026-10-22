# One session per room, on every transport

Status: built. Supersedes the placement in `link-one-connection-design.md` (the mux there belonged to an attachment).
Its reasoning about the window, the write timeout and the stream cap stands and is used here.

## The contract

A room holds TWO network sessions to the hub for each way it can reach it, one per lane: the control lane carries
the control connection and nothing else, and the data lane carries everything else. Each session is opened by the
first dial on its lane and opened again only when it dies. Every other connection the link needs (the warm data pool,
relay, announce, git, room spec, upgrade, ssh stream) is a stream or circuit ON the data session, the way the OpenZiti
tunneler carries every TCP flow over one ziti connection. All three transports keep the one interface,
`Dialer.Dial(ctx) (net.Conn, error)` (`internal/link/link.go`). The lane is a mark on the context (`lane.go`), set by
the room's attach. Nothing above a transport knows sessions exist.

| Transport | The sessions | A Dial | Reset |
| --- | --- | --- | --- |
| direct | two mutual TLS connections per hub address, yamux on each (`mux.go`) | opens a yamux stream on its lane's connection | none needed. a dead session is noticed and the next dial opens one |
| ziti | the process's two ziti contexts for the identity, data and control (`internal/zitictx`) | dials a circuit on its lane's context | logs both contexts in again |
| zrok | the process's two ziti contexts for the zrok environment's identity | dials the share token on its lane's context | logs both contexts in again |

A session is never built per dial, never built again while one is alive, and an overlay context is never closed
while the process runs. The count per room is two and stays two, whatever the room dials.

## Two lanes: why control rides alone

A beat is a few bytes every `Beat`, and the hub evicts a room it has not heard from in `Silence`. On one session the
beat is a frame in the same send queue as every bulk stream, so a git fetch, a relay or a download in flight sits in
front of it. On a slow uplink that queue is seconds deep, the beat arrives late, and the hub drops a room that is
healthy and serving. The control lane gives the beat a queue of its own all the way to the hub's socket.

- **Direct.** Control is a second muxed TLS connection, keyed by hub address, direction and lane (`muxFor`). The hub
  needs no change: each connection says `mux` and gets its own yamux session. An old hub refuses `mux` on both, and
  the per dial fallback applies to each lane as before.
- **Ziti and zrok.** Each lane is its own ziti context for the same identity (`zitictx.SharedFileLane`, keyed
  `path#lane=control`). A context holds its own TLS connection to each edge router, so the control circuit does not
  share a router connection with bulk circuits. `Relogin` and `Close` reach every lane.
- **The hub's side of an overlay is not split.** The hub binds the service once. Two binds would be two terminators
  for one service, and a room's dial would land on either, so there is nothing to gain. The room's side is where
  its uplink queues, and that is the side split here.
- **A mixed deploy needs nothing.** A lane is a room side choice. The hub sees one more connection or circuit from
  the room and serves it as it always has.

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

Settings (`muxConfig`): the window stays at 256 KiB, because a frame on the data lane can wait behind every bulk
stream's window and a larger one makes a small request wait behind a big download. `ConnectionWriteTimeout` is
`Silence`. Keepalive is on at 15 s, because the data session belongs to the dialer and is shared by the pool, the
probes and every other kind, so it has to notice its own death rather than wait for one of them to. The hub caps a
session at 256 open streams.

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

Every stream on it ends together, which is how a ziti context behaves already. When the control session dies the
control stream fails at once, so the room attaches again on its backoff. When the data session dies the next data
dial opens a new one. Callers see a reset, as they did on a broken socket.

## Probes

The per path probe and the climb probe dial every rung through the same `Dial`. On direct that is a stream on the
rung's session, so a room standing on an overlay holds one direct session per probed address as well. That is one
socket per rung, flat, and it ends 15 to 25 s after the path does.

## The test

`TestEveryTransportHoldsOneSessionAcrossDialsClosesAndReconnects` (`internal/link`, integration): for each transport,
1,200 dials and closes across three forced reconnects. Direct counts TCP connections the hub accepted (two per
reconnect, plus the first two) and sessions the room opened on each lane. Ziti and zrok count contexts built through
a fake SDK (two, data and control, never closed). `mux_integration_test.go` covers the mixed deploy both ways and the
certificate rules.

`TestABeatArrivesWithinSilenceWhileBulkSaturatesTheLink` (`lane_integration_test.go`) throttles the hub's side of
direct to 256 KiB/s each way and runs four 256 KiB downloads through the room. It times every beat's round trip from
the time the beat carries. With two lanes the slowest stays under half a second and the room stays attached. It runs
the same load again with control on the data lane and requires that to hurt: a round trip over a second, or the hub
dropping the room. That half is what proves the rig saturates the link. Measuring the gap between beats does not
prove it, because a queue of constant depth delays every beat alike and the hub still hears one every `Beat`.
