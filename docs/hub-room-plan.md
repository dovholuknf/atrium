# A hub that serves the board, and rooms that run the agents

**Status: the direct transport is BUILT and works end to end.** Hub, room, join, mutual TLS, and the proxy.
OpenZiti and zrok are designed for and not written. Tested by `internal/link/link_test.go` and by
`scripts/walkthrough/hubroom.spec.js`.

Branch `claude/hub-room`. Binary `atrium2`, deliberately separate from `atrium`, which is running right now
with agents attached to it.

------------

## What this is actually for

> my goal is to be able to change the UI without fucking up running claude instances. i'm trying to decouple
> my "atrium improvements" from my "running llms"

That sentence decides the whole design, and it decides it differently from `docs/federation-design-v2.md`.

Atrium today is one process holding three things that have wildly different lifetimes:

| | changes | costs to restart |
| --- | --- | --- |
| the board's HTML, CSS and JS | every few minutes while you work on it | nothing, if it were alone |
| the database | rarely | a reopen |
| the pseudo terminals and the agents in them | never, on purpose | **every session on the machine** |

They are in one process, so the cheapest thing to change is priced at the cost of the most expensive thing to
lose. Changing one line of CSS costs you every agent.

**So the split is along the lifetime line, not along a network line.**

```
  browser
     │
     │  http://localhost:7800          one origin, so the board's JS is untouched
     ▼
  ┌──────────────── HUB ─────────────────┐   restarted all evening
  │  serves index.html, /css, /js        │   holds NOTHING
  │  proxies everything else             │   owns no process
  └──────────────────────────────────────┘
     ▲
     │  the ROOM dials out, and keeps the connection
     │
  ┌──────────────── ROOM ────────────────┐   up for days
  │  sqlite, pty supervisor, the agents  │   serves JSON and byte streams
  │  its own board on loopback too       │   never restarted to change a colour
  └──────────────────────────────────────┘
```

### Why this is not what federation-design-v2 says

That document has the hub forward bytes to the leaf's existing `http.Handler`. That handler serves the board
**files** as well as the API, so under it, changing the UI means restarting the leaf. It delivers one origin
and not the decoupling, because it was answering "how do I see four machines on one screen" rather than "how
do I stop my CSS edits killing my agents".

The correction is one line long: **the hub serves the static files itself.** Everything else is the same
document, and its central claim still carries the whole design: the room serves a handler it already has, so
there is no second protocol and no second representation of a card.

### It is also the answer to a question asked earlier the same night

> would it be possible at all to 'orphan' a claude instance that we start up to let it keep running so we can
> restart?

The answer then was that it needs a process that owns the pseudo terminals and outlives the daemon. **The room
is that process.** It needs no new IPC, because the board API is already the interface between the two halves.
A room restart still takes its agents with it. This decouples UI work from agents, which is what was asked
for, and the finer-grained version stays a separate item.

------------

## The trick, which is smaller than it sounds

A room does not learn a new protocol. It serves the handler it already has, on a `net.Listener` whose `Accept`
returns connections it **dialled**:

```go
http.Serve(reverseListener{hub}, daemon.BoardHandler())
```

The hub keeps those connections in a pool and speaks ordinary HTTP/1.1 down them, through `http.Transport`
with a `DialContext` that pops one. Go's own client machinery does the keep-alive, the 100-continue and the
upgrade handling, and never finds out the connection was dialled from the far end.

So websockets upgrade, server-sent events stream, and a 400MB download is a download, because none of it is
being translated.

### Why the room dials, and not the hub

Reachability is the obvious reason and the second one matters more: **a room must not care whether the hub is
up.** If the hub dialled, a hub restart would be connection-refused on the room's side and a reconnect storm
on the hub's. Inverted, a hub restart is the room noticing its socket closed and redialling. The room is the
stable thing, the hub is the disposable thing, and the connection points the same way.

### The connection pool

The control connection is the only one that stays framed. Everything else is raw HTTP.

```
room                                   hub
  │── dial, hello{control} ───────────▶│
  │◀── welcome{session, warm:4} ───────│
  │── dial, hello{data, session} ×4 ──▶│   pooled, ready before anything asks
  │── beat every 5s ──────────────────▶│
  │◀── echoed ─────────────────────────│   an echo, so a half-open socket is caught
  │◀── need{n} ────────────────────────│   when the pool is drawn down
```

- **The hub asks for connections; the room does not guess.** The hub knows how many it holds and how many it
  wants spare. A room dialling on its own schedule would starve the pool under load or hold sockets nothing
  will use.
- **The beat is echoed.** A write that succeeds into a dead connection is the failure a heartbeat exists to
  catch, and only a round trip catches it.
- **A data connection carries the session its control connection was given.** Two rooms reconnecting at once
  cannot cross their pools and serve one room's board out of another's process.

------------

## What ships in the hub's answer

**One rule: if the hub has the file, the hub serves it. Everything else goes to the room.**

Resolved against the directory rather than a list of prefixes, so adding a stylesheet needs no code change.
The failure mode of a list is a 404 in production for a file sitting on disk.

Three exceptions, each for a reason:

### 1. `/v1/health`'s `build` is rewritten to the hub's

**Without this the board reload-loops and the whole idea looks broken.**

The page remembers the `build` it first saw and calls `location.reload()` when it changes, which is how a
popped-out terminal left open for a day notices a rebuild. Split in two, the room hashes ITS copy of the board,
which is not the copy the browser is running, so every poll would report a different build, every tab would
reload, and every reload would do it again.

The hub answers for the board because the hub **is** the board. The room's hash is kept as `room_build`, so a
version skew between the halves is still visible.

### 2. `POST /v1/shutdown` is refused at the hub

The room guards it with a loopback check on `r.RemoteAddr`. Through a link, the room sees a connection that
terminates inside its own process, so that check passes for anybody who can reach the hub. This is the rule
`shutdown.go` already states for shares, word for word: once something else can reach it, loopback stops
meaning "this machine".

### 3. `/_hub/*` is the hub's own

`rooms` and `health`. Under a reserved prefix so it can never collide with a board route the room grows later.

------------

## Identity, and one line to paste

```
$ atrium2 hub

  the board is at   http://localhost:7800
  rooms dial in on  127.0.0.1:7801

  Nothing is on it yet, because a hub holds nothing. On the machine your
  agents run on, paste this:

      atrium2 join atr1_eyJhIjoiMTI3LjAuMC4xOjc4MDEi...

  It is good once and for an hour.
```

That is the entire setup. Nobody types a path, copies a file, or learns what a CSR is.

**What it does underneath.** The hub mints itself a certificate authority on first run. The join string carries
the hub's address, the SHA-256 of that authority, and a one-time secret. `atrium2 join`:

1. dials with the authority **pinned** from the string, so a man in the middle needs the CA's key
2. makes a key **on the room** and sends only a signing request, so the hub never sees the private key
3. spends the secret, which is stored hashed and works once
4. saves the certificate and starts the room

Thereafter it is mutual TLS, and **the name in the certificate is the room's identity**. The name in the hello
frame is a label; a room that disagrees with its certificate gets the certificate's name and a log line.

One port for all three kinds of connection. The listener asks for a client certificate without insisting, and
the first frame decides: `enrol` may arrive without one, `control` and `data` may not. A second port would be a
second thing to open in a firewall, put in the join string, and explain.

**This mints its own CA, which a program should be slightly embarrassed to do.** It is here because it is the
shortest path to a mutually authenticated connection with no dependency and nothing to stand up, and because
the design is arranged so that replacing it deletes a file rather than rewriting anything above it.

------------

## What a transport has to provide

Exactly two things, which is the point:

```
hub:   a net.Listener
room:  something that returns a net.Conn
```

| transport | hub side | room side | status |
| --- | --- | --- | --- |
| **direct** | `tls.NewListener` | `tls.Dialer` | **built** |
| **OpenZiti** | `ziti.Listen(service)` | `ziti.Dial(service)` | designed, not written |
| **zrok** | the share's listener | an access dialer | designed, not written |

`github.com/openziti/sdk-golang` is **already an indirect dependency** of this repository, and
`overlay_native.go` already serves the board on a listener an SDK handed back. So the ziti transport is a file,
not a project.

Under ziti, `Hub.Enrol` is left nil and the whole certificate file is dead: the network has already decided who
may connect before a byte arrives, and `Hub.Authenticated` answers from the ziti identity. That is the sense in
which the mTLS here is scaffolding rather than architecture.

------------

## Two bugs found while building it, both worth writing down

**1. A warm connection is old by the time it is used.** `http.Server.ReadHeaderTimeout` starts when a
connection is **accepted**, not when bytes arrive. The pool exists precisely so connections wait, so any
non-zero value silently closes exactly the connections it holds. The symptom: a link reporting itself healthy
with four idle connections while every request answered `EOF`. Every timeout on the room's server is now zero,
with a comment saying why, and `TestAConnectionThatWaitedInThePoolStillWorks` is the regression test.

**2. The backoff never reset.** One long outage walked the redial delay to its thirty second ceiling and left
it there, so every later hub restart cost thirty seconds of blank board. On the half being restarted all
evening, that reads as the link being flaky. It now resets after a link that stayed up longer than the silence
threshold, measured in time attached rather than in the absence of an error.

------------

## What is deliberately not done

- **Two rooms.** The hub proxies to `Only()`, the single attached room, and answers empty when there are two.
  The board grows a room picker when there is something to pick and not before. Everything under
  `docs/transparent-rooms.md` question 2, card identity across two stores, is that item and not this one.
- **Authentication in front of the hub.** Loopback, no login, anonymous, exactly as atrium is today. The
  guard lives in the daemon and wraps only published listeners, so moving it is its own piece of work.
- **`/auth/*`.** It exists only inside that guard, so nothing serves it and nothing asks for it while there is
  no login.
- **Migrating anything.** The room starts on a fresh database. That was explicit: "i want to start clean and i
  want to 'not give a fuck' about the agents in this new atrium UNTIL i'm ready to migrate."
- **Surviving a ROOM restart.** Still takes its agents. That is the pty-host item, and the room is where it
  would eventually live.

------------

## Not breaking the atrium that is already running

This runs as the same Windows account as an atrium with live agents, so the ways it could interfere were
enumerated before anything was written.

| | how it is kept apart |
| --- | --- |
| **the address file** | `%LocalAppData%\atrium\daemon.json` is how every hook finds its daemon. Two daemons writing it means the second silently takes every hook on the machine, and the symptom is activity arriving at a board nobody is looking at. `ATRIUM_LOCATION` now overrides it and the room sets it before opening anything. |
| **the shared address file** | Same story via `%WORKTREE_ROOT%`. `ATRIUM_SHARED_LOCATION=-` means "publish nowhere". |
| **the database** | `atrium2/room/atrium2.db`. Never the running one: two daemons on one sqlite file is the thing the store is not built for. |
| **ports** | 7800 and 7801 for the hub, 7810 and 7811 for the room. The running atrium has 7777 and 7778. |
| **claude's hooks** | Only ever written by the board's install button, never at startup. Verified rather than assumed. |
| **the binary** | `atrium2`, its own `cmd/`. `atrium` is not rebuilt, not replaced, not restarted. |

Three files in shared packages were touched, all additively: `daemon.BoardHandler()` (new accessor),
`api.EmbeddedBoard` and `api.BoardID` (exporting what already existed), and `LocationPath`'s override above.
Nothing changes behaviour when the new variables are unset.

------------

## Trying it

```
atrium2 hub                        # prints a join string
atrium2 join atr1_...              # on the machine with the agents
```

Then `http://localhost:7800`.

Restart the hub as often as you like. `scripts/walkthrough/hubroom.spec.js` is the same thing recorded: it
launches an agent through the hub, kills the hub mid-session, brings it back, and checks the agent's process id
never changed.
