# A room on direct, ziti and zrok at once

Status: design spike for backlog item `f-multi-transport-rooms`. Nothing here is built. Written 2026-10-15 from the code
at `claude/main` f8886c2f and the docs named under "Read first". The interview the item asked for is replaced by "Open
questions for clint" at the end, each with a recommended answer.

Why now: clint is moving locations. m1mini and sg3 stay at his house and must reach the hub over OpenZiti. On
2026-10-15 sg4 (the hub) moved from 192.168.102.41 to 192.168.1.68. sgg had no ziti identity, so it dialled the dead
address for an hour. sg3 and m1mini did have ziti and still took about five minutes to fall back.

## The short version

The brief assumes the hub takes one transport and a join string names one. That is out of date. Most of the ladder is
already built (`docs/rnd/room-transport-order.md`, `internal/link/ladder.go`, `internal/link/transports.go`). What
tonight showed is that the ladder is a good answer to the wrong failure. It handles a dial that fails. It does not
handle a connection that was up and then silently stopped, a hub address that went stale, or a room that has no rung to
fall to.

So the design is five fixes on the existing ladder, plus two things that are new (zrok as a real rung, and the board on
several transports), in this order:

1. **A room notices a dead control connection in seconds.** Today it can take minutes. This is the five minutes.
2. **The hub advertises every address it has, and keeps doing so as they change.** This is the stale address.
3. **The board says which paths are up.** Cheap, and it makes every later phase visible.
4. **A room with no ziti identity can get one**, from an operator, without atrium minting anything. This is sgg.
5. **zrok becomes a rung.**
6. **The board listens on a list of transports.**
7. **Optionally, keep every path up at once.** The question the all-at-once note asked, and the one with the most
   cost. Recommended last, and recommended only if phases 1 to 4 do not turn out to be enough.

## What exists, reconciled

| Asked for | State | Where |
| --- | --- | --- |
| Hub listens on direct, ziti and zrok at once | Built. `--transport direct,ziti,zrok` opens one side per kind. | `internal/cli/transport.go` `openHubs`, `atrium_run.go:74` and `:180` |
| One accept path behind the listeners | Built. Every side hands a `net.Listener` to the same `Hub`. Overlay listeners are wrapped by `MixedListener`. | `overlaytls.go`, `transport.go` `overlayListen` |
| One hub-signed certificate works on every transport | Built and confirmed, see below. | `direct.go` `ServerTLS`, `overlaytls.go` `Proven` |
| A room carries an ordered list of transports | Built. `transports.json` holds `order`, local `paths` and the hub's `hub` list. | `transports.go` `Candidates` |
| Failover on a failed dial | Built. `Ladder.Climb` walks top down, ten seconds per rung. | `ladder.go` |
| Switch back to the preferred path | Built. Two good probes two minutes apart move a room up. | `room.go` `probe`, `probeNeed` |
| The hub tells a room its paths | Built, direct and ziti only. zrok is not handed out. | `hubSet.paths` |
| A room learns a new path list | Built, `learnHubPaths`. | `transport.go:441` |
| The board shows the transport in use | Built, one badge, warning colour on a fallback. | `docs/changes/room-transport-order.md` |
| Detect a dead path quickly | **Missing.** | see "The five minutes" |
| Learn a changed hub address | **Half built.** The hub warns loudly and rooms are told one static address. | `advertisecheck.go`, `openHub` |
| A per-path up and down state | **Missing.** One badge says which path is in use, not which are healthy. | |
| Get a ziti identity onto a room that has none | **Missing.** | |
| zrok as a room rung | **Missing.** Dialable only with a token the operator typed on the room. | `hubSet.paths` skips zrok |
| Board on several overlays | **Missing.** `--board-transport` is one value. | `atrium_run.go:583` |
| Add a transport to a running room | **Partly built.** The hub's list reaches a running room, a room-local path needs a restart. | |

One correction to the brief: the join string is not the carrier of the list any more. It names how a room first joined
and nothing else. The list comes from the hub's welcome and is written to `transports.json`, which is what lets a room
that is cut off from the hub still know its fallback on the next start.

### Room identity is one thing on every transport

Confirmed from the code. `Direct.ServerTLS` is the one place the hub's handshake is built. `Direct.Listen` wraps a TCP
listener in it and `overlayListen` wraps a ziti or zrok listener in the same config through `MixedListener`. The hub
reads the room's name from `peerCert(c).Subject.CommonName` and keys it by the public key (`peerKey`), never from the
hello. On the room side `Proven` wraps whatever dialler the transport gives in `tls.Client` over `Direct.clientTLS`,
which checks the issuer and ignores the address, so it needs no hostname and works over a ziti service name or a zrok
share. `roomDialer` applies `Proven` to every rung of a room that holds a certificate.

So one `room.crt` is the same room whichever path it uses, and a room moving between paths is a reconnect of the same
key. The hub's attach already treats it so: a same-key reattach replaces the old connection
(`old.close("replaced by a newer connection from the same room")`), a different key under a taken name is refused.

Two things this does not give, and the design must not pretend it does:

- A ziti identity is NOT a room's name. `docs/rnd/overlay-room-identity.md` section 2 shows the caller id is client set.
  The ziti identity proves reach and nothing else. That is why a ziti identity can be shared or rotated without touching
  who the room is.
- The legacy path (an overlay room with no certificate) still exists beside this until `atrium rooms legacy refuse`. A
  multi-transport room must not use it. Phase 4 below enrols the certificate before it adds a rung.

## What went wrong tonight, from the code

### The five minutes

A room's control connection is a plain TCP or TLS stream. `Room.beat` writes a beat every 5 seconds
(`Timings.Beat`). The hub echoes it (`hub.go:576`, `a.send(note{Beat: n.Beat})`). The hub gives up on a silent room
after 15 seconds (`Timings.Silence`), which is the hub's side and works.

The room's side has no equivalent. A grep of `room.go` finds no `SetReadDeadline`. The reader blocks in `readJSON`, and
the only way it learns the path died is a failed write in `beat`. A write to a black-holed path does not fail. It lands
in the kernel's send buffer and the stack retransmits until its own give-up, which on Windows runs to minutes. That is
the five minutes sg3 and m1mini spent before `Climb` was even called again. The ladder was fine. Nothing told it to run.

The fix is small and entirely inside the room: the hub already echoes every beat, so the room has a free liveness
signal. If no frame, echo included, has arrived for three beats (15 seconds, the hub's own number) the room closes the
control connection, which the reader then reports, and `attach` returns and the loop climbs. Detection drops from
minutes to 15 seconds, and the climb is bounded by `DialWait`, so a dead top rung costs at most ten more seconds before
the next. Worst case about 25 seconds from the cable being pulled to a working second path.

Cheaper still: turn on TCP keepalive on the dialled connection (`net.Dialer.KeepAlive`, 5 second idle and interval)
as a second layer. It will not help a ziti circuit, which is not a kernel socket, so the application-level deadline is
the one that matters and keepalive is optional.

### The stale address

`openHub` resolves `--link-advertise` once, mints it into every join string and puts it in the hub's welcome as the one
direct path. Nothing re-reads it. `watchAdvertise` checks every few minutes whether the address is still one of this
machine's and shouts in the log. It does not fix anything, and it is a human's job to restart the hub with a new flag
and run `atrium room hub <addr>` on every room.

Two things make this cheap to fix:

- The hub's certificate does not need to change. `clientTLS` ignores the address and pins the issuer, so a hub that
  moved IP is still the same hub to a room. Nothing in `EnsureCA(link.Hosts(adv))` is load bearing for a room.
- The room already treats every direct address as its own rung and never lays one over another (the 2026-10-14 sg4
  lesson). So a longer list of direct addresses is something the ladder already understands.

What is wrong is only that the hub says one address, once. The design: the hub computes its advertise set on a timer
from its interfaces (the same `interfaceIPs` `watchAdvertise` uses) plus the explicit `--link-advertise` values plus an
optional DNS name, and sends that set as `paths` in every welcome and in a new `paths` push frame when it changes. A
room already persists whatever it is told and rebuilds its ladder (`learnHubPaths`). The push is the new part, and it
only matters for a room that is attached on another rung when the hub's address changes.

Which brings the other half. A room that is attached to a hub **by DNS name** needs nothing special, because
`net.Dialer` resolves on every dial. A room joined by an IP literal is the problem. So:

- `--link-advertise` accepts a host name, and the hub's welcome sends it as `{transport: direct, addr: "hub.example:7779"}`.
  A room dials it, Go resolves it per dial, and a hub that moves is found as soon as DNS says so. This costs nothing to
  build and it is the whole answer for anyone with a name.
- For a house with no DNS name, the hub sends every interface address that is not loopback, link local, or a tunnel
  adapter (live-common.ps1 already has the rule for the ziti-edge-tunnel adapter, `100.64.0.0/10`). Each is a rung. A dead
  rung costs `DialWait` once per climb and no more, and the probe never touches it while the room is on a good rung.
- A room that cannot reach the hub on anything direct and has no overlay rung is the sgg case and gets phase 4.

Learning an address a room was never told (a hub that moved while the room was down, and the room only has the old
list): this is what the overlay is for. The ziti rung is the room's way back in, the welcome there carries the new
direct addresses, and the next climb has them. A room with direct only and a stale list cannot recover without a
human, and the design says so rather than inventing mDNS. See open question 4.

## The design

### 1. Hub: listen on all of them

Already built. Keep. Three small changes:

- `--transport` stays a comma list. Add the same setting in `hub_setting` (`hub_transports`) so a hub that restarts under
  a service manager keeps its set without a command line. The flag wins when given.
- A side that cannot listen is already left out and logged. Make it retry: a ziti controller that is down at boot should
  be bound when it comes back, not at the next restart. One goroutine per failed side, backoff to one minute, and the
  same `Hub` accepting from it. It is the first thing that matters when clint moves and the controller is briefly
  unreachable.
- The hub's `/_hub/health` gains `listeners: [{kind, up, since, last_error}]`, so "the hub is only on direct right now"
  is a thing the board and an operator can see rather than a line in `hub.err`.

### 2. Room: one control connection, ordered, fast to fail

Keep the ladder. The room has one control connection on one rung, and every other connection follows it (decision 8 of
the order doc). Fix what is broken:

| Rule | Value | Why |
| --- | --- | --- |
| Dead connection detected | no frame, echo included, for three beats (15 seconds) | the hub's own `Silence`, symmetric |
| Per rung dial bound | `DialWait` 10 seconds, but 4 seconds for a rung that failed last time | a known dead rung should not cost ten seconds on every climb |
| Order of a climb | last known good rung first, then the configured order | after a drop the path that was working is the best bet. The configured order resumes on the next planned climb |
| Attempts racing | try rung N+1 after 2 seconds without waiting for rung N to fail ("happy eyeballs") | a black-holed top rung costs two seconds, not ten. Only ever one connection survives, the loser is closed |
| Switching back to the preferred rung | keep the two-probe rule, but probe each better rung on its own schedule and probe a failed rung sooner (30 seconds, doubling to two minutes) | a rung that just came back should be found fast, a rung that has been dead for a day should not be dialled constantly |
| Both paths at once | no, not by default | see "Two paths at once" |

The racing rule is the biggest change in behaviour and the one most worth a flag (`transport_race`, default on).
Without it, three dead rungs ahead of a live one cost thirty seconds per climb.

### 3. Switching: what drops and what resumes

A switch is a reattach on a new rung (`errMovingUp` for a planned one, a read error for a forced one). The room process
and everything it owns stay up. What the hub and the board see:

| Thing | On a switch | Why |
| --- | --- | --- |
| Terminals, agents, cards, the permission long-poll's queue | Untouched. They are in the room process (or the pty host), not on the link. | `AGENTS.md`: cards outlive the process, the link is only a route |
| The control connection | Dropped and redialled on the new rung. The hub replaces the old attached entry (same key). | `hub.go` attach |
| The warm data pool | Dropped and refilled. The hub asked for N on the new welcome, and a data connection is a pooled borrow, so nothing is mid-request when it is idle. | `Room.open` |
| A request in flight over a data connection (a board call, a file download) | Fails. The board retries, a download restarts. | a data connection is one HTTP connection, it cannot move rungs |
| A terminal attach websocket from the board | Drops. xterm.js reconnects and the room replays the screen it holds. The shell is untouched because it lives in the room. | the websocket is proxied over a data connection. It is the most visible blip, so it is the one to test |
| The agent permission long-poll (`/v1/permission`) | The agent's hook talks to the ROOM on 127.0.0.1:7777, not the hub. A switch does not touch it. The hub only learns of the question through the room. | the hook posture: a switch must never fail a session |
| `git fetch` through the forwarder | The forwarder dials a new `git` link connection per command (`DialGit`). A fetch running at the switch fails and git retries on the next call. The next call uses the new rung automatically. | `git.go` |
| Hub-side announce, relay, upgrade connections | Redialled by the new `Dial`, which follows the ladder. Nothing wanders because everything goes through the one dialler. | `ladder.go` |
| Says queued for another room | The hub holds them for up to a day and resends. A say mid-switch is `held` or `unconfirmed` as it is today. | `atrium_say` contract |

So nothing is lost and a user sees at worst a terminal reconnect and a board refresh. The design target: a path dying
mid session costs 25 seconds of blank board for that room (detection plus a climb) and zero cards, zero commands, zero
agent state. That is the bar for the failover test.

The board during the blip: the room's tile shows "reconnecting" in the warning colour and keeps its last cached card
list (the hub already caches announce data so the board can show a room that is not answering, `Caches`). It does not
say "offline" until a room has been gone for a minute. When it returns, the tile names the rung and, if that is not the
first, says so (decision 11 of the order doc already does that).

### 4. Two paths at once

The all-at-once note (`docs/rnd/room-transport-all-at-once.md`) asked for one badge per path, each live. There are three
ways to read it, and they cost very different amounts:

| Reading | Cost | Gets you |
| --- | --- | --- |
| A. One control connection, plus a cheap probe of every other path on a timer, shown as up or down | one dial and a handshake per path per probe, nothing held | the per path badge, which is what the note actually wanted, and it is already half there since `Ladder.Probe` dials every better rung |
| B. One control connection plus a held idle control connection on every other path ("hot standby") | a held circuit per path per room (the note's OCI router cost) | instant failover, no climb |
| C. Spread the data pool across paths | all of B, and a hub that must key one room by several connections | throughput, which nobody asked for |

**Recommendation: A, and extend the probe to rungs below the current one as well.** Today only better rungs are probed. A
room on direct with a dead ziti rung learns about it only when direct fails. Probe all rungs on the schedule above and
the board has the true per path state. Racing (section 2) gives most of what B buys, since a switch is a two second
race and not a ten second wait, and it needs no change to how the hub keys a room.

B and C need the hub to hold several control connections per room, which breaks the rule in `hub.go` that a second
attach with the same key replaces the first. That rule is a real protection (it is how a stuck half open connection gets
cleaned up) and it should not be loosened for a feature nobody has shown a need for. Phase 7 below is B, and is marked
conditional.

### 5. The board: which paths, which is live

A room's record gains `paths: [{kind, addr, state, since, last_error, rtt_ms, source}]` where `state` is `on` (the
control connection is here), `up` (probe answered), `down` (probe failed), `none` (this room has no way to dial it, such
as no ziti identity) and `unlistened` (the hub is not listening on that kind).

- The room reports it in its beat, so a hub that cannot dial a rung still learns the room's view. A rung is `down` as
  seen from the room, which is the only view that matters.
- The hub adds `listeners` (its own side), so `none` and `unlistened` are different things and the tile can say which.
- The rooms tab shows one small badge per kind in a fixed order (direct, ziti, zrok). `on` is solid, `up` is outlined,
  `down` is the warning colour with the error in the tooltip, `none` and `unlistened` are greyed with a reason. A room on
  a fallback keeps the existing warning colour on the active one.
- `none` for ziti is the sgg state, and its tooltip carries the one command that fixes it (phase 4).

### 6. Credentials per transport, and revoking one

| Transport | Credential | Lives where | Revoked how | Touches the others |
| --- | --- | --- | --- | --- |
| direct | the room's hub-signed certificate (`room.crt`, `room.key`, `ca.crt` in the room's key directory) | the room's `~/.atrium/room/`, the hub's CA in its `--atrium-dir` | delete the room on the hub (the `rooms` commands remove a room), which makes its name unknown, and the hub refuses its key. A certificate revocation list is not built, see open question 6 | revoking the certificate revokes the room on EVERY transport, which is the point of one identity |
| ziti | an enrolled ziti identity per room (`atrium-room-<name>.json`), issued by the network's admin | the room, path in `transports.json` (`paths[].identity`), file 0600 | `ziti edge delete identity` or remove it from the `#atrium-rooms` role | none. The room keeps direct and zrok and the same name |
| zrok | the room machine's own zrok environment (`zrok enable`) plus the hub's private share token | the machine's zrok environment, and the token in `transports.json` (`paths[].share`) | disable that environment, or delete the share, or `zrok2 modify` the share's access grants | none. Revoking an environment is one machine, deleting the share is every room |

The posture rule stands and the design keeps it (`ziti-zrok-flow-design.md`, "The credential posture, corrected"):
atrium administers no network and mints no overlay credential. It holds the name of a credential and opens a file
somebody else produced. A ziti identity and a zrok environment are the operator's, and the room certificate is the one
credential atrium does mint, because it is the only one that names the room.

Two clean separations follow and are worth stating, because they are what makes "remove one without touching the
others" true: the certificate is about WHO (one per room, every transport), the overlay credential is about REACH (one
per room per overlay). A compromised ziti identity can reach the hub but cannot attach as a room without a certificate,
and a leaked certificate is useless to a machine the overlay does not admit.

#### A room with no ziti identity (sgg)

sgg is the case the design has to answer, since it dialled a dead address for an hour with no rung to fall to and was
not told it could never fall anywhere.

First, say so. A room that has fewer than two usable rungs is running without redundancy. `Candidates` already returns
`skipped` ("ziti: no identity on this room"), and today that is a log line. It should be a state on the board
(`paths[].state = none`, a tile warning "no fallback") and a line in the hub's room list. A room that is attached and has
one rung is working today and about to be an outage tomorrow. That is the thing to surface.

Second, give the operator a path that is one command and creates no credential:

```
atrium rooms ziti-invite sgg --jwt atrium-room-sgg.jwt
```

The operator creates the identity on the controller as today (`ziti edge create identity atrium-room-sgg -a atrium-rooms
-o atrium-room-sgg.jwt`) and hands the one-time JWT to the hub. The hub holds it in its store, deletes it once the room
reports success, and offers it to sgg in the welcome of its next attach, over the room's mutually authenticated link:
`{invite: {transport: ziti, jwt: ...}}`. The room enrols it in process (`ziti.Enroll`), writes the identity beside
`transports.json`, adds the rung and reports `paths[ziti].state`.

What this does and does not break:

- It does not mint a credential. The JWT is the network admin's and the identity it becomes is the network's.
- It does hold a one-time secret in the hub's store for a short while. A JWT has a lifetime and is single use, so the
  exposure is the hub's store for minutes, behind the same authentication as the hub's other data. The alternative is
  carrying the file by hand to each machine, which is what m1mini and sg3 needed and is the reason sgg took an hour.
- It only works when the room can already reach the hub. A room that is cut off completely cannot be invited. For that
  one, the existing `atrium room transports ziti --identity <jwt>` stays the way, and the board's tile tooltip names it.

This is open question 2, and the recommended answer is to build it, because it is the difference between "a room needs a
person standing at it" and "a room needs a person at the controller".

### 7. The board over several transports

`--board-transport` takes one value and `atrium_run.go:583` switches on it. It already serves a board on an overlay with
`serveBoardOn(ctx, ln, handler, name)`, which takes any listener. Making it a list is a loop over that switch, with one
rule that changes: today a zrok board and a ziti board share no state, and they can keep sharing none. Each is a
listener on the same handler, so nothing is proxied and the "atrium drives an overlay, it does not become one" rule from
`AGENTS.md` still holds. Two points:

- Flag becomes `--board-transport zrok,ziti`, and the existing single value is the list of one. `--board-share` and
  `--board-service` stay as the per kind options.
- A listener that fails (a zrok account returning 500, see `zrok-share-500.md`) is logged and skipped, and retried on the
  same backoff as the room link sides in phase 1. Today it is `serveBoardOn` skipped and never retried.

The board is a browser. It has no certificate and no fallback list, so "multiple transports" means several addresses
the operator can open, and the phone page (`web/m/`) should offer them. The settings screen already shows the zrok
address. Showing a list of live addresses with each one's state is the board's half and is phase 6.

## Migrating without one big restart

The aim is that no room is touched by the hub changing and no hub is restarted for a room changing.

1. **Hub first, once.** Ship the hub with phase 1 and 2. It already listens on whatever `--transport` says. A restart of
   the hub is the one big restart. Rooms reattach on their own, as they do for every hub restart today.
2. **A room learns its new rungs from the hub.** The welcome already carries them and `learnHubPaths` already applies
   them to a running room. So adding ziti to the hub's `paths` reaches every attached direct room with no action on the
   room, and the room's ladder rebuilds in place (`Ladder.Set` keeps the rung it stands on).
3. **A room that lacks the credential for a rung** says so (`none`) until phase 4's invite gives it one. It does not
   fail and it does not restart.
4. **Removing a transport** is the reverse: take it out of the hub's `room_transport_order` or stop listening, the next
   welcome omits it, and a room standing on that rung keeps it until the next climb (`Ladder.Set` keeps the rung it is
   on). A room that is on a rung the hub dropped climbs at its next drop, or when the room restarts. Nothing
   is cut. Removing a transport the room is currently on is the one case worth a warning, and the board's per path state
   from phase 3 is what lets the operator see that nobody is on it before removing it.
5. **The first target is m1mini and sg3**, because their direct path is the one that broke. They need phase 1 (so the
   next move is 25 seconds, not five minutes) and nothing else, since they already hold ziti identities.

## Testing

Never against the live hub or rooms. All three throwaway setups are cheap and the first two already have a place in the
repo's scripts.

- **Ziti**: `ziti edge quickstart` runs a controller and a router in one process on localhost, with its own admin and
  ports. Create the `atrium-hub` service, a bind policy for a hub identity and a dial policy for two room identities,
  enrol them to a temp directory, run a hub and two rooms on temp key directories and temp ports. The live sg4 values
  (`129.153.47.232:443`, the identities under `C:\Users\claude\.ziti\`) are never used.
- **zrok**: a private share on a throwaway zrok account or the zrok quickstart (`zrok2 test canary`), with its own
  environment directory (`ZROK_ROOT`, or the SDK's `environment.Root`). Not the account clint uses for the board, which
  has returned 500 on share creation (`zrok-share-500.md`), so the test must treat a failed share create as skipped and
  not failed.
- **Direct**: loopback and a second loopback address (127.0.0.2) stand in for two network paths.

The failover proof, as an integration test in `internal/link` plus a scripted run:

1. Hub listens on direct (127.0.0.1) and ziti (quickstart). Room has both rungs, ziti identity enrolled to a temp dir.
2. Room attaches on its top rung. Assert the board data and one open terminal attach websocket.
3. **Kill the top path without a RST**, which is the case that bit tonight. A TCP proxy in the test sits in front of the
   hub's direct port and is told to stop forwarding and stop reading (a black hole), not to close. Closing is the easy
   case that already passes. A black hole is the five minutes.
4. Assert within 30 seconds: the room is attached on the other rung, `fallback` is true, `/_hub/rooms` shows the new
   `via`, the room's cards and terminals are unchanged, the terminal websocket reconnects and shows the same screen, and
   a queued `say` is delivered.
5. Restore the proxy. Assert the room moves back after the two probes and the probe never ran during step 4's black
   hole in a way that blocked traffic.
6. Repeat with the ziti service killed (stop the bind), then with a stale advertise (the hub tells an address that does
   not answer first), then with the room having no ziti identity (expect `none` and an honest warning, not a hang).

The cheap unit layer: `Ladder` and the new liveness check over `net.Pipe` with a fake clock, as `ladder_test.go` does.
The black hole is easy to make deterministic with a conn that swallows writes and blocks reads.

## Phased build

Smallest first. Each phase is one worker and leaves the board working. Each ships its own `changelog/` and
`docs/changes/` notes per `AGENTS.md`.

| Phase | What | Size | Fixes tonight's | Leaves the board |
| --- | --- | --- | --- | --- |
| 1 | The room's liveness deadline (15s of silence closes the control connection). Optional TCP keepalive. A black hole test over `net.Pipe`. | small, `internal/link/room.go` only | the five minutes | unchanged, rooms just notice sooner |
| 2 | Advertise a set, not a string: a host name in `--link-advertise`, interface addresses by default, re-computed on a timer, and a `paths` push when it changes. | small to medium, `transport.go`, `hub.go`, `protocol.go` | the stale address | unchanged, welcome carries more paths |
| 3 | Per path state: probe all rungs, the room reports `paths` in its beat, the hub adds `listeners` to health, the rooms tab draws one badge per kind. | medium, `ladder.go`, `room.go`, `hub.go`, `web/` | nothing by itself, it makes the rest visible | gains badges |
| 4 | `atrium rooms ziti-invite`: a one-time JWT handed to an attached room, enrolled in process. A board tile for `no fallback`. | medium, `transports.go`, `protocol.go`, `cli/`, `web/` | sgg | gains a warning on a one rung room |
| 5 | zrok as a rung: the hub hands the share token to an authenticated room in the welcome and the room dials it. | small once 4's invite frame exists | none, completes the three | zrok badge goes live |
| 6 | The board over a list: `--board-transport zrok,ziti`, retry of a failed listener, the phone page offers the addresses. | small to medium, `atrium_run.go`, `web/m/` | none | gains a list of addresses |
| 7 | Conditional: hot standby, a held control connection per path. Only if 1 to 4 still leave failover too slow. Needs the hub to hold more than one control connection per room. | large, `hub.go` | none | gains an instant switch |

Order notes: 1 first because it fixes the actual failure with the least code. 2 and 3 are independent and can run in
parallel workers. 4 depends on 3's state to show `none` honestly. 5 reuses 4's frame. 6 is independent of everything.
Racing the rungs (section 2) is a part of phase 1 or its own small phase between 1 and 2, and is the one decision in
the table that wants an A/B on the ziti path before it is the default.

### Risks worth naming

- **The ziti SDK dial takes no context.** `dialWithin` already bounds it from outside and closes a late winner. Racing
  needs care that a losing ziti dial is closed, not leaked, because a held circuit is a held router resource.
- **A ziti control connection that sits idle** on a router may be reaped sooner than a TCP one. The beat every 5 seconds
  keeps it warm, but this has not been measured. The test above should run a room for an hour on ziti.
- **TLS inside ziti inside TLS.** Already accepted (`overlay-room-identity.md`). The cost is a handshake per pooled
  connection and a switch makes a new pool, so a racing climb that opens two handshakes costs two.
- **zrok has been returning 500 on share create** (`zrok-share-500.md`). Phase 5 is unprovable while that stands and the
  design must degrade to "zrok: unlistened" and not to a hub that fails to start. It already does.
- **A black hole test is timing sensitive.** Use a fake clock for the unit and generous bounds for the integration run.

## Decisions

The orchestrator decided these on 2026-10-15, about 18:40: **every recommended answer below is taken.** Two of them in
specific terms. For 4, there is no domain for the hub today, so every room gets a ziti identity (phase 4 does it for
sgg). For 5, ziti stays first globally and `loopbackFirst` gains the 5 millisecond direct rule. clint approves the
design as a whole before any phase is built. The questions are kept as they were asked, so the reasoning stays.

1. **Is 15 seconds to notice a dead path acceptable?** Faster is possible (three beats at a 2 second beat is 6 seconds)
   but a ziti circuit over a slow link produces false positives, and a false positive costs a full reattach and a data
   pool refill. Recommended: 15 seconds, the hub's own number, with the beat unchanged.
2. **May the hub hold a one-time ziti JWT for a room and deliver it?** It is what makes sgg a one command fix. The cost
   is a short lived secret in the hub's store. Recommended: yes, behind `atrium rooms ziti-invite`, deleted on success,
   expired after the JWT's own lifetime, and never shown on the board.
3. **One path or two at once?** Recommended: one control connection, all paths probed and shown (reading A), with racing
   on a climb. Hot standby (phase 7) only if a measured failover is still too slow. Two paths carrying traffic at once
   (reading C) is not recommended at all: it needs a hub that keys a room by several connections and buys throughput
   nobody asked for.
4. **A room with direct only and a stale address.** The only recovery without a human is a name or an overlay. Recommended:
   put `hub.<your domain>` in `--link-advertise` if you have a domain, because it covers the moving house case with no
   code. If not, give every room a ziti identity before the move, because the overlay is the way back in. Not
   recommended: broadcast or mDNS discovery of the hub, which is a new attack surface for a problem DNS or an overlay
   already solves.
5. **Order after the move.** The default is ziti, direct, zrok (your 2026-10-14 choice). For m1mini and sg3 at the house
   with the hub elsewhere, direct is not reachable, so ziti first costs nothing. For a room on the hub's LAN, ziti first
   adds an 85 millisecond round trip (`loopbackFirst` exists for this). Recommended: keep ziti first globally and add
   "a direct rung that answers inside 5 milliseconds goes first" to `loopbackFirst`, so a room on the hub's LAN needs no
   configuration. Alternatively set `atrium room transports order direct,ziti` on the LAN rooms.
6. **Revoking a room's certificate without deleting the room.** Deleting the room works and is blunt. A revocation list
   for the hub's CA is not built. Recommended: not now. Deleting a room and re-issuing a join secret is one command and a
   re-join, and the ziti and zrok credentials already revoke on their own. Revisit if a room's certificate is ever
   suspected stolen and the room cannot be deleted.
7. **Should the board's listener also retry forever?** Recommended: yes, same backoff as the link sides. A board on a zrok
   share is how the phone reaches the hub, and a share that was down at boot must come back without a restart.
8. **Does the hub need an explicit list of transports a room is allowed on?** A room that holds a ziti identity can
   always dial ziti. If the operator wants "this room is direct only" or "never zrok", that is a hub side per room
   allowlist. Recommended: not now. Nothing asked for it, and `rooms legacy refuse` is the only control that exists today.
9. **The zrok environment for rooms.** Every room machine needs its own `zrok enable`, which is a zrok account action on
   clint's account. Recommended: build phase 5 last and only enrol rooms that cannot have ziti (sgg is the example), since
   a ziti identity and a zrok environment both give reach and ziti is the one with a real policy model.
10. **Two hubs?** A second hub is the other redundancy ask and is out of scope here. Recorded so it is a decision and not
    an omission: this design is about a room reaching ONE hub by several roads, not a room reaching several hubs.

## Read first (what this was written from)

`docs/fabric/overlays.md`, `docs/fabric/ziti-zrok-flow-design.md`, `docs/rnd/overlay-room-identity.md`,
`docs/rnd/room-transport-order.md`, `docs/rnd/room-transport-all-at-once.md`, `docs/changes/room-transport-order.md`,
`internal/link/{ziti,direct,overlaytls,ladder,transports,room,hub}.go`, `internal/cli/{transport,atrium_run,roomjoinflags,advertisecheck}.go`,
`scripts/sgg/bringup-sgg.md`. The brief also named `docs/fabric/hub-room-plan.md`, which does not exist in this tree.
