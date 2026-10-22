# The room link on direct TCP: one connection per room

Backlog item `f-link-one-conn-direct`. Status: built differently, see `one-session-per-room.md`. The mux sits in
`Direct.Dial` rather than on the attachment, so every transport keeps the one Dial interface.

Observed 2026-10-17: sg3 on OpenZiti holds one router socket, and the SDK carries every link connection as a stream
inside it. sgg on direct TCP holds 12 sockets to hub `:7779`, because the direct transport opens one TLS connection per
link connection: the control connection, every pooled data connection, and one per announce, relay, git, room spec,
upgrade and ssh stream. clint asked for the OpenZiti shape on direct: one mutual TLS connection per room, with the
streams multiplexed inside it.

## What exists

- `link.Dialer` is one method, `Dial(ctx) (net.Conn, error)`. The room calls it for the control connection
  (`room.go` `attach`, through `Ladder.Climb`), for every data connection (`dials.go` `dataDial`), and for each other
  kind (`announce.go`, `relay.go`, `git.go`, `stream.go`, `upgrade.go`, `roomspec.go` through the daemon's `/hubdial`).
- Every connection starts with a `hello` line whose `Kind` decides what it is. The hub's `take` (`hub.go`) reads it,
  names the room from the client certificate (`identify`, `peerCert` in `direct.go`), and hands the connection on.
- The data pool: the hub keeps `Warm` idle data connections per room and asks for more with a `Need` note. A data
  connection after its hello is plain HTTP/1.1, which is what lets websockets and event streams pass untranslated.
- The ladder (`ladder.go`, `pathstate.go`, `transportoff.go`) dials rungs other than the one the room stands on: the
  per-path probe every `PathsEvery` (30 s) on every rung, the climb probe every `ProbeEvery` on a fallback, and
  `WorkingElsewhere` when a transport is turned off. Each closes what it opened.

## Decisions

### 1. The multiplexer: `hashicorp/yamux`, between the TLS connection and the hello

yamux streams are `net.Conn`s with deadlines and half close, so everything above the dial (the hello, the pool, HTTP,
websockets, the ssh byte pipe) runs on a stream unchanged. It has per-stream flow control, which decision 2 needs. It
is the multiplexer Consul and Nomad run their RPC on. It is MPL-2.0, which is file-level and asks nothing of atrium as
a dependency.

Rejected: `xtaci/smux` does the same job with a smaller user base and nothing yamux lacks here. HTTP/2 as the link
protocol would carry the HTTP kinds but not the ssh stream or the line-framed control connection, and a websocket over
HTTP/2 needs extended CONNECT on both sides.

**Where it sits.** The mux belongs to an ATTACHMENT, not to `Direct`. `Direct.Dial` stays one TLS dial, so every
probe of the direct rung is still one dial that is closed at once and never builds a session (see 5).

On the room:

- `attach` climbs as today and gets a TLS connection. When the rung muxes (a new optional interface on the dialer,
  `Mux() bool`, true for `Direct` only), the room writes `hello{kind: "mux"}` and waits for the welcome. On `ok` both
  sides run yamux on that connection, the room as client. The room opens stream 1 and runs the existing control
  handshake on it, unchanged.
- The attachment holds the session. A new `r.linkDial(ctx)` opens a stream when a session is up and calls
  `r.Dial.Dial` otherwise. The seven call sites above call `r.linkDial` instead of `r.Dial.Dial`. The daemon's
  `/hubdial` (commands on this machine, such as `room spec pull` and `ssh-proxy`) is wired to `linkDial` too, so a
  command rides the room's session and opens no socket of its own.
- Each stream then says its own hello exactly as a dialled connection does today, kind and session included.

On the hub:

- `take` learns one more kind, `mux`. It is refused without a certificate, and on the old overlay path, exactly as
  `control` is. On `ok` the hub runs a yamux server on the connection and, for each stream it accepts, calls `take`
  again with a `muxStream` that wraps the stream and remembers its parent TLS connection and transport.
- `peerCert`, `DirectAuthenticated`, `legacyTransport` and `keepAlive` unwrap a `muxStream` to its parent, so the
  certificate names every stream, and a stream cannot claim another room. A stream that says `mux` or `enrol` is
  refused: no nesting, and joining never happens on an enrolled connection.
- The yamux server reads from the `buffered` connection that read the hello, never the bare one, for the reason
  `readJSON`'s comment gives.
- `Version` stays 1. The mux is a kind, negotiated per connection, so nothing older is refused for not having it.

The pool, `Need`, `Warm` and the dial slots in `dials.go` stay as they are. A data "dial" becomes a stream open plus
the hello round trip, with no TCP or TLS handshake. Letting the hub open data streams itself would remove the pool, but
it is a second design for the same result and it would leave the overlays on the old shape. Not now.

OpenZiti and zrok do not mux in atrium. Their SDK already carries each connection as a stream over one router channel,
and a second multiplexer inside one circuit would only add a layer.

### 2. Backpressure: per-stream windows, and nothing that waits on the whole session

- **A slow reader stalls its own stream only.** yamux gives each stream a receive window. A peer that does not read
  stops getting window updates, and the writer of THAT stream blocks in `Write`. The session's receive loop never
  blocks on a stream, since a sender never has more in flight than the window allows. An ssh stream whose far end is
  slow, or a git fetch the hub is writing slowly to disk, holds one goroutine and nothing else.
- **The window is 256 KiB** (`MaxStreamWindowSize`, the yamux default). It bounds two things. Throughput per stream is
  window over round trip: 256 MiB/s on a LAN, about 5 MiB/s at 50 ms. And the extra wait a control frame can see
  behind bulk data already queued is (bulk streams x window) over bandwidth: 8 bulk streams on a 10 Mbit/s uplink is
  1.6 s, a tenth of `Silence`. Raising the window trades the second for the first, so it is one named constant, and
  raised only when a git collect over a WAN is measured as slow.
- **`ConnectionWriteTimeout` equals `Silence`** (15 s). In yamux it is session wide: a write that cannot be queued
  for that long closes the session. A link that cannot move a frame in 15 s has already missed three beats, so the two
  give up at the same moment and never disagree.
- **yamux keepalive is OFF.** The beat on the control stream, echoed by the hub, stays the one liveness signal, read
  with the deadline in `readNote`. Two liveness checks with two timeouts would end the link for two different reasons
  and log the wrong one.
- **The hub caps open streams per session at 256.** A stream over the cap is closed on accept with a log line. The
  room's data dials are already capped at 8 (`maxDataDials`), so the cap only bites a room that misbehaves, and it
  bounds the hub's memory at 256 windows per room.

### 3. Reconnect: everything ends together, and that is the ziti behaviour today

Losing the one connection ends every stream in it at once. Every stream's `Read` and `Write` fail with the session's
shutdown error, which callers treat as a reset, as they do today on a broken socket.

- **The room.** `attach` returns when the control stream fails, closes the session, and `Run` backs off and climbs
  as it does now. Nothing new. The yamux shutdown makes the control stream fail at once, so a cut connection is
  noticed faster than a black hole, which still waits on `Silence`.
- **The hub.** The control handler ending closes the session as well as the stream. Today `a.close` closes the pool,
  and with a mux it also ends any stream the room opened. A room that attaches again while its old session is half
  open replaces it as today, and the old session is closed with the old attachment.
- **What each caller sees:**

| Stream | On a drop | Recovers by |
| --- | --- | --- |
| proxied board request | an error from the hub, as for any room that drops | the next request after the room attaches |
| terminal websocket | the socket closes | the board's existing reattach |
| event stream (hub fan in) | the upstream ends | the hub's subscription when the room attaches again |
| announce | the write fails | the room announces everything on its next attach |
| relay | the call fails | as today: a launch is `unconfirmed`, a message stays with the sender's room |
| git fetch or collect | the fetch fails | the next sync or collect |
| upgrade download | the download fails | the offer on the next attach. The hash check catches a short file |
| ssh stream | ssh says the connection closed | the person runs ssh again |

**Nothing outlives its attachment, on any transport.** `linkDial` records every stream or connection it hands out on
the attachment, and `attach` closes them all when it returns. On direct the session does that by itself. On an overlay
it is new: today an ssh stream or a git fetch opened before a ladder move keeps its old connection after the room
moved. The cost is that an ssh session open during a move now ends with the move. A move happens after two good
probes two minutes apart, and the hub holds the other end of every stream anyway, so a hub restart already ends them.

### 4. Rolling deploy: the room asks, an old hub says no, and the room carries on as today

- **New room, old hub.** The old hub answers `hello{kind: "mux"}` with its unknown-kind refusal, "a connection is
  control, data, ...". The room recognises it, the way `stream.go` already recognises an old hub refusing `stream`,
  closes that connection, dials again on the same rung, and attaches per connection exactly as today. It logs the
  fallback once per hub process, and tries the mux again on every attach, so the hub's upgrade is picked up at the
  next reconnect, which a hub upgrade causes. The cost is one extra TLS handshake per attach to an old hub.
- **Old room, new hub.** The old room never says `mux`. The new hub's `take` serves its per-connection kinds as it
  does today, because the overlays need that path anyway. Nothing to do.
- **New room, new hub, overlay rung.** The rung's dialer says it does not mux, so the room never asks.
- The board shows nothing new. A deploy needs no order between hub and rooms.

### 5. sg3's extra direct socket

sg3 stands on the ziti rung, and three things dial its direct rung while it does:

1. The per-path probe (`pathstate.go`), every 30 s, on every rung. One mutual TLS dial, closed as soon as it answers.
2. The climb probe (`Ladder.Probe`), every 2 minutes, only while on a fallback. The same dial, closed at once.
3. A connection opened on direct BEFORE the room moved to ziti that is still in use: an ssh stream, a git fetch or a
   download. `Ladder.Dial` picks the rung when the connection is dialled, and nothing closes a connection on a move.

The first two are not leaks. A sample catches one only during its handshake, and its remote port is different every
time. The third is a leak of the kind decision 3 removes: it lasts as long as its stream, on a transport the room has
left. The build's first step settles which: two samples on the hub 40 s apart. Each sample lists sockets on `:7779` from
sg3's address. The same remote port in both samples means case 3. A port that changes or disappears means a probe.
Under this design case 3 cannot happen, and the probes stay, because measuring a path means dialling it.

### 6. The test: count what the hub accepted, per room

- **Direct, one connection (`link` integration).** A real `Direct` on `127.0.0.1:0` with test certificates. The hub's
  listener is wrapped in a counter of connections accepted and still open. The room attaches, then runs, all at once:
  12 proxied requests, an event stream, an ssh stream to a fake sshd, a git fetch, an announce and a relay. Sampled
  throughout: accepted 1, open 1. Then the test closes the physical connection: every stream fails within a second,
  the room attaches again, accepted 2, open 1.
- **Overlay rung, zero direct sockets.** A ladder with a fake overlay rung on top (a dialer over `net.Pipe`, no mux)
  and the real `Direct` below, with `PathsEvery` cut to 100 ms. The room stands on the overlay for 2 s. On the direct
  listener, open is 0 at every sample, and every accepted connection closed within the probe's wait. This is sg3's
  shape. On an overlay the test asserts the part atrium controls: no connection of the room's left the overlay
  dialer. One router socket is the OpenZiti SDK's behaviour, and a CI test cannot count it.
- **Nothing outlives a move.** On the same ladder, open an ssh stream, force a move, and assert the stream was closed.
- **Skew.** A hub that refuses `mux` as an old one does: the room attaches per connection, everything above works,
  one log line. An old room against a new hub is the existing suite.
- **Backpressure.** The hub never reads an ssh stream while the room writes 10 MiB into it. Meanwhile a proxied
  request answers within a second, beats keep arriving, and the room stays attached for twice `Silence`.
- **Live, once, after the deploy.** On the hub, two samples 40 s apart of established sockets on `:7779`, by remote
  address: every direct room shows 1 with the same port, every overlay room shows 0.

## What does not change

- The room dials and the hub never does. Streams are opened inside a connection the room dialled.
- A room is named by its certificate, now once per connection and inherited by every stream in it.
- The hello, the kinds, the pool, the timings, the ladder and its probes.
- OpenZiti and zrok. The per-connection path on the hub stays for them and for older rooms.

## Build

One branch, in this order: the measurement in 5. Then `muxStream` and the unwrap in `peerCert` and its siblings. Then
the `mux` kind on the hub, with the stream cap. Then the room's `attach` and `linkDial`, with the seven call sites and
`/hubdial`. Then the old-hub fallback. Then the attachment closing what it handed out. The tests in 6 come with each
step. Update `internal/link/AGENTS.md` ("a transport supplies a `net.Listener` ...") and
`docs/fabric/hub-and-rooms.md` with one paragraph each: direct runs one connection per room, and the kinds are streams
inside it.
