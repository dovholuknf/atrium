# Federation: one board, many machines

Design note. Nothing here is built. Read `docs/overlays.md` first, because the transport question is already
answered and this document only argues about what sits on top of it.

## Verdict

The goal is right and the usual implementation of it is wrong. Wanting one board that shows agents running in a
container, on a build box and on a laptop is a reasonable thing to want, and atrium is closer to it than it looks.
What must not be built is a central atrium that mirrors other atriums: an aggregating daemon has to copy state it
did not observe, arbitrate identity it did not issue, forward decisions into another process's memory, and accept
writes from a peer, which is authentication by another name and is ruled out in `CLAUDE.md` twice. **Federate in
the client, not in the server.** Every environment runs a whole daemon that owns its own store, its own gate and
its own terminals. The overlay makes each one reachable. One page opens several of them at once and merges the
lists on screen. Nothing is mirrored, so nothing can disagree.

That is not a consolation prize. It is a smaller change than the aggregator, it preserves every resilience
guarantee without arguing about any of them, and the parts that cannot cross a machine boundary stay on the
machine where they mean something.

## What the operator actually asked for

Two things, and only one of them is new.

1. Reach atrium from anywhere. **This already ships.** `docs/overlays.md` describes the board publishing itself
   through zrok or `ziti tunnel host`, driven from `internal/daemon/overlay.go`.
2. See and drive agents running in kubernetes, docker, linux, windows and macOS. This is the new part, and the
   phrasing "many atrium hubs installed in other environments" is already most of the design. A daemon per
   environment is not a compromise. It is the only arrangement in which the daemon's core operations mean
   anything, for the reasons in "What stays local, forever."

What is missing is therefore not a server. It is a way for one browser tab to hold more than one daemon at a time.

## What breaks

Each item below was checked against the code rather than assumed. Where a claim could not be verified it says so.

### Card identity: task ids are safe, wire names are not

Task ids are UUIDv7 (`internal/store/store.go:303`), so two daemons will never mint the same card id. That kills
the most obvious objection to any federation shape.

`wire_name` is a different story. It is declared `UNIQUE` on the task table (`internal/store/schema.go:38`) and it
is the first thing registration matches on (`internal/store/tasks.go:71`, and `GetByWireName` at line 192, which
is how every hook finds its card). Names are derived from the working directory plus a truncated timestamp for a
launched runner (`internal/daemon/launch.go:134`) and from whatever a session calls itself otherwise. Two
containers built from the same image, working in `/work/repo`, will produce colliding names, and in a single
table a collision does not error. It silently hands one session another session's card, its history, its gating
flag and its pending requests.

Under an aggregator this has to be solved, and solving it means qualifying every wire name by an origin the
aggregator assigns, which is the aggregator inventing identity. Under client federation it never arises, because
each table only ever holds names minted against itself.

There is a related smaller wrongness worth naming. `observedFor` fills `hostname` from the daemon's own
`os.Hostname()` (`internal/daemon/daemon.go:255`). Today that is right by accident, since the agent is always on
the daemon's machine. The moment a card can describe work elsewhere, that column records where the record was
written rather than where the work ran, which is a field that looks correct and is not.

### The halt is a fact about one store

`onHalt` (`internal/daemon/daemon.go:212`) closes the agent listener and leaves it closed, and the human listener
stays up to report the cause. The board renders that as a banner off `/v1/health`
(`internal/api/web/index.html:5336`).

Federation turns one boolean into N. A central aggregator makes this actively worse in both directions. Its own
store failing takes the whole view down for peers that are healthy, which contradicts the point of the halt,
which is that the surface explaining the failure must survive it. A peer's store failing is invisible to the
aggregator unless it polls, and the aggregator would then be storing "peer B is halted" in the database, which is
the same category of lie as storing activity.

Client federation gets the correct answer for free. `/v1/health` is per daemon, so the banner is per column, and
a peer that cannot be reached at all is a fetch that failed rather than a state that has to be modelled.

### The permission chain runs where the request is blocked

`HandlePermission` (`internal/hub/hub.go:407`) records the request, then blocks on a Go channel held in a map in
that process, with no internal timeout, until something calls `decide`. Everything the chain consults is local to
that daemon: queued messages, the card's shelved status, `MatchRule` against that daemon's `perm_rule` table, and
auto mode (`internal/daemon/daemon.go:306` onward). The agent on the other end is parked inside one HTTP request
and will wait forever, which is the design.

So a decision made on a federated board must arrive at the process holding that channel. There is no shortcut.
An aggregator cannot answer on a peer's behalf, it can only forward, which means the aggregator's copy of the
permission queue exists purely to be a routing table. Client federation forwards by construction, because the
button posts to the daemon the request came from.

The real loss under either shape is standing rules. Importing Claude Code's allow and deny lists brought 134
rules across on one machine, and those rules live in that machine's database. A rule written on the board is a
rule where it was written. The honest answer is `/v1/rules/export` and `/v1/rules/import`, which already exist:
push a rule set to peers, rather than pretending there is one central set.

### The pty cannot leave the machine

`docs/supervision-design.md` and the ConPTY section of `docs/architecture-v2.md` establish that the daemon owns
each pseudo terminal, that closing one takes the attached process with it, and that ConPTY offers no reattach.
`docs/backlog.md` records the matching rule that atrium cannot attach to a session it did not start, because a
console cannot be handed to another process after the fact.

None of that is negotiable across a machine boundary, and it is the strongest argument that a daemon belongs in
every environment. What does travel is the WebSocket. `internal/daemon/attach.go:47` accepts with
`InsecureSkipVerify: true`, and the comment says explicitly that a stricter origin check would break reaching the
board over an overlay. So a page served from one origin can already attach to a terminal on another, which is
most of the client federation story done in advance.

Two costs remain. Round trip latency lands on every keystroke, because attach is character at a time. And a
dropped socket loses nothing durable, since the scrollback is a server side ring buffer, so reattaching repaints
from the peer rather than from the browser.

### `atrium stop` gets quietly more dangerous, and this is already true today

`handleShutdown` requires loopback unless a shutdown token is configured (`internal/daemon/shutdown.go:62`), and
`isLoopback` reads `r.RemoteAddr`. When the board is published through a tunneler running on the daemon's own
machine, the connection the daemon sees originates from that tunneler. If that source address is loopback, then
"loopback only" has silently become "anyone the overlay lets in", and the kill switch is exposed.

**I could not verify from this repo what source address `ziti tunnel host` or a zrok share presents to the
backend.** It depends on the tunneler, not on atrium. The safe rule does not require knowing: configure
`--shutdown-token` whenever an overlay is running, since a configured token replaces the loopback rule rather
than adding to it. Federation makes this urgent rather than theoretical, because federation means every daemon is
published.

### The reaper is a syscall and syscalls do not federate

`reapOnce` (`internal/daemon/reaper.go:37`) calls `processAlive(t.PID)` for any card carrying a pid. That is
correct exactly and only when the pid was minted on the machine running the check. A pid observed on another host
is not merely unknowable, it is worse than unknowable, because the local operating system will cheerfully report
that some unrelated process with that number is alive.

Cards with no pid fall back to `QuietAfter` at three hours of silence, measured against `LastActivityAt` on the
local clock. Under an aggregator that clock belongs to the wrong machine. Under client federation each daemon
reaps its own cards with its own pids and its own clock, which is the only arrangement where the check is a fact.

### SSE and the long poll over a link that comes and goes

Two properties of the current code make a lossy link survivable, and both are worth stating because they are easy
to break later.

The SSE stream sends a comment ping every 25 seconds (`internal/api/api.go:881`), which keeps an intermediary
from deciding the connection is idle. More importantly, the board treats an event as a nudge rather than as a
delta: every listener is wired straight to `refresh()` (`internal/api/web/index.html:5350`), and there is a five
second poll behind it (line 6636). A missed event costs one stale render, not a corrupted view. Nothing depends
on the stream being gapless, and no `Last-Event-ID` replay is needed because no state travels on the stream. A
federated board should keep that property rather than inventing per peer sequencing.

The agent listener is untouched by any of this, because agents talk to the daemon on their own machine over
loopback. The v1 guarantees that the model never sees a disconnect and never sees an empty prompt are guarantees
about a link that federation does not lengthen. **This is the single biggest reason the client federation shape is
correct.** Any design that carries agent traffic across the overlay puts a WAN in the path of the one thing the
project exists to keep quiet.

### Clock skew is already handled, by accident

`/v1/tasks` returns `idle_seconds` and `wait_seconds` computed on the daemon as `time.Since` of a stored instant
(`internal/api/api.go:230` and `:240`), and the board renders those numbers rather than parsing timestamps. So
ages are already computed against the clock that wrote them, and a skewed peer is off by its own skew rather than
by the difference between two machines.

The rule this implies for any federation work: **carry the seconds, never the instants.** An aggregator that
stored a peer's RFC3339 timestamps and re-derived ages locally would show negative waits on a peer whose clock
runs fast, and containers with no time sync are the normal case rather than the odd one.

### The board is hardcoded to its own origin

There are no `Access-Control-*` headers anywhere in `internal/` and every fetch in the board is a relative path.
This is the one place client federation has real work to do rather than inheriting a property. It is discussed
under the recommendation.

### Things that are local and would need routing anyway

`/v1/browse` lists the daemon's filesystem, for the reason its own comment gives: the browser's picker reads the
wrong machine (`internal/api/browse.go:13`). `Launch` stats the working directory on the daemon's machine
(`internal/daemon/launch.go:92`), resolves the runner against the daemon's PATH, and runs the harness `prepare`
step there. Runner discovery reports what is on that machine at startup.

Every one of those has to reach the daemon that owns the directory. An aggregator would have to route them
through untouched, which is worth noticing: the aggregator would be a pass through for launching, browsing,
attaching, stopping and deciding, and would be aggregating only the list. That is a lot of machinery for one
merged list.

## Topologies

### (a) One page, many daemons. The client federates.

```
  operator's browser                       each environment
  ┌────────────────────────┐            ┌──────────────────────────────┐
  │ the board              │─── json ──▶│ atriumd  :7778 board         │
  │  peer list             │◀── sse ────│ atriumd  :7777 agents (local)│
  │  merged stack          │─── ws  ───▶│ sqlite · gate · ptys         │
  │  origin badge per card │            └──────────────────────────────┘
  └────────────────────────┘            ┌──────────────────────────────┐
        │  each peer reached       ────▶│ atriumd  (docker)            │
        │  through the overlay          └──────────────────────────────┘
        ▼                               ┌──────────────────────────────┐
   ziti tunnel / zrok access       ────▶│ atriumd  (k8s pod)           │
                                        └──────────────────────────────┘
```

**Works.** No state is copied, so nothing can be stale or in conflict. The halt, the gate, the rules, the reaper,
the ptys and the ages all stay inside the daemon that owns them and keep meaning what they mean. Agent traffic
never leaves loopback. A peer being down is a failed fetch and nothing more. Attach already accepts cross origin
sockets. Every write goes to the owner with no routing layer to get wrong.

**Breaks.** Cross origin reads need CORS headers the daemon does not send today, and the board needs a base URL
per request where it currently has a hardcoded relative path. Rules do not travel, so a rule is per peer unless it
is exported and imported. Desktop notification permission and the alerting state become per origin unless the
page holds them centrally. Merged sorting has to tolerate one peer answering slowly, which means rendering what
arrived rather than waiting for all of them.

**Costs.** One board side refactor of the fetch layer, an allowlist of permitted origins on the daemon, and a
stored peer list. No new server, no new protocol, no new trust relationship.

### (b) A central aggregating daemon, read and write, gate included

```
  browser ──▶ ┌───────────────────────┐  poll + forward  ┌──────────────┐
              │ atrium central        │◀────────────────▶│ atriumd  A   │
              │  mirrors peer cards   │                  └──────────────┘
              │  own sqlite (copy)    │  forward decide  ┌──────────────┐
              │  proxies attach ws    │◀────────────────▶│ atriumd  B   │
              └───────────────────────┘                  └──────────────┘
                        │
                        └── whose halt is this?  whose clock?  who may write?
```

**Works.** One URL, one origin, one notification surface, one place to write a rule if rules were pushed out from
it. It is what people picture when they say federation.

**Breaks.** It needs a second copy of truth and therefore a sync story, which the project has avoided everywhere
else. A peer accepting writes from the central daemon is a trust decision between two processes, which is
authentication, ruled out by `CLAUDE.md` and by `docs/backlog.md`. Wire names must be namespaced, which means the
centre issues identity. The halt becomes ambiguous, and the central store failing darkens healthy peers. Ages
must be recomputed or forwarded, and forwarding them means the mirror is wrong between polls. Permission
decisions are forwarded into a channel in another process, so the centre's queue is a routing table dressed as
state. Attach must be proxied, adding a hop to a per keystroke path.

**Costs.** The largest of the four by a wide margin, and every unit of that cost buys a merged list that (a) also
produces.

### (c) A switcher. Many daemons, one at a time.

```
  browser ──▶ [ laptop | build box | k8s ] ──▶ ┌──────────────┐
                  ▲ one selected                │ atriumd  sel │
                  └── stored peer list          └──────────────┘
```

**Works.** Almost free. It is a dropdown and a stored list of base URLs. Everything on screen is exactly the board
that ships today, pointed somewhere else, so no invariant is touched at all.

**Breaks.** It does not answer "which one needs me most", which is goal 2 of `docs/architecture-v2.md` and the
reason v2 exists. Waiting counts, the nag and the notifications only cover the peer currently selected, so a
permission request on an unselected peer is silent and an agent sits frozen. That is a real regression against the
one failure the project cares about.

**Costs.** A day. It is also the first half of (a), which is what makes it worth doing rather than skipping.

### (d) A stateless front door on the operator's machine

```
  browser ──▶ ┌─────────────────────────┐ ──▶ /peer/laptop/*  ──▶ atriumd A
              │ atrium front  (no store)│ ──▶ /peer/build/*   ──▶ atriumd B
              │  peer table only        │ ──▶ /peer/k8s/*     ──▶ atriumd C
              └─────────────────────────┘      (json, sse and ws forwarded)
```

A reverse proxy with a peer table and no database. The board is served from it, so everything is same origin, and
`/peer/{name}/v1/...` is rewritten to that peer's base URL over the overlay. It never parses a response, never
stores a card, and has no opinion about a halt.

**Works.** Solves CORS without touching the daemon's HTTP surface. One origin means one notification permission
and one alerting state. It can carry SSE and the attach WebSocket, both of which are streams a proxy handles
without understanding them.

**Breaks.** It is a new process to run and to keep alive, and if it is down the board is down even though every
peer is fine. It puts a hop in the attach path. It is a tempting place to start adding caching, at which point it
becomes (b) by drift.

**Costs.** Small, but strictly larger than the CORS allowlist in (a), and it introduces a component the deployment
story does not currently have.

## Recommendation

Build (c) then (a). Keep (d) in reserve. Never build (b).

The discriminator between (a) and (b) is not effort, it is where truth lives. Atrium's entire design rests on one
copy of each fact: activity is never written down because a stored activity is a lie after a restart, status is
inferred only for runners that cannot speak because two signals for one fact will disagree, and observed values
never overwrite overrides because that is one rule instead of two copies. A mirroring aggregator is the same
mistake at a larger scale. It creates a second copy of every card, and the second copy is wrong between polls by
construction. There is no version of (b) that does not eventually need a reconciliation rule, and there is no
version of (a) that ever needs one.

The discriminator between (a) and (d) is smaller and could go the other way later. (d) is the right answer if
cross origin turns out to be more painful than expected in practice, particularly if desktop notifications or some
future browser storage need turns out to be per origin in a way that hurts. It is worth writing (a) so that (d)
remains a drop in change: if the board only ever knows a base URL per peer, then pointing those at
`/peer/{name}` instead of at a remote host is a configuration change and nothing else.

The discriminator between (c) and (a) is the nag. (c) is a legitimate stopping point for a week, and an
illegitimate one forever, because a permission request that nobody is shown freezes an agent, and the alerting
loop is the mechanism that stops that happening. (a) is (c) plus fanning the waiting and permission polls across
every peer rather than only the selected one.

## Identity and trust

Nothing here authenticates anything, and that is deliberate rather than deferred.

Every daemon publishes its board the way `docs/overlays.md` already describes. The identity is a file the
tunneler owns, and atrium passes the path through without opening it (`internal/daemon/overlay_config.go:47`).
Who may reach a given board is a service policy on the ziti network, or the grant on a zrok private share. Atrium
has no opinion, holds no key, issues no token and has no user model. That is the whole answer to "who
authenticates whom": the overlay does, before a packet reaches the daemon.

The operator's machine runs a tunneler or `zrok access private`, which presents each remote board as a local
address. So the browser sees several ordinary HTTP endpoints and each daemon still sees a connection from its own
loopback. There is no atrium to atrium trust relationship anywhere in this design, because no daemon ever talks to
another daemon. This is exactly why (a) can honour "no auth invented here" and (b) cannot.

Three rules follow, and they are not optional.

**Publish the board, never the agent listener.** `:7777` accepts registrations, submits, permission answers and
session events with no authentication of any kind, because it was built for loopback. Exposing it on an overlay
would let anyone permitted by the policy create cards, answer permission requests and drive gating. Only `:7778`
is ever a candidate for a service, and a ziti service definition or a zrok backend that points at 7777 is a
misconfiguration worth writing down where someone will read it.

**Configure a shutdown token whenever a share is running.** See the `atrium stop` section above. A token replaces
the loopback rule, which is the intended behaviour and the only one that survives a tunneler.

**A private zrok share is the floor.** A public share hands the board to whoever has the link, and the board can
approve any command an agent asks to run. The code already defaults to private and already asks before going
public.

## What stays local, forever

These are not "not yet". They are properties of the thing itself, and a design that tries to move them is wrong
rather than ambitious.

**The pseudo terminal, and therefore attach.** A pty is a kernel object owned by the process that created it, on
the machine that created it. ConPTY has no reattach, closing one kills the child, and a console cannot be handed
to a process after the fact. Only a daemon on the same machine can own a runner's terminal. The bytes can cross
the world. The terminal cannot.

**Liveness.** `processAlive(pid)` is a syscall against the local process table. A pid from elsewhere is not a
weaker signal, it is a wrong one, because the number will often match something real and unrelated.

**Launching, browsing, PATH and prepare.** The directory must exist on the machine that will run in it, the
runner must be on that machine's PATH, and the harness prepare step exists to modify that machine's environment.

**The store, and its halt.** One daemon, one database, one halt. The halt closes the listener that machine's
agents are parked on, and that only works if they are that machine's agents.

**Activity.** Already in memory and already never written down (`docs/activity-design.md`). It stays per daemon,
which also means the badge is as fresh as the last fetch and nothing pretends otherwise.

**Agent traffic.** Hooks find their daemon through a file in local runtime state (`internal/daemon/whereami.go`),
and `atrium hook`, `join` and `session` all default to `localhost:7777`. That is already the right design for a
daemon per environment, and it means installing atrium in a container is the same job as installing it on the
laptop rather than a new mode.

## Implementation plan

Each stage ships on its own and leaves the single machine case exactly as it is.

### 1. A peer list and a switcher

Store a list of `{name, base_url}` in the board's own settings, and route every fetch in the page through a
helper that prefixes the selected peer's base rather than emitting a relative path. Selecting the local daemon is
the empty prefix, which is today's behaviour.

*How you know it works.* With one peer configured pointing at `http://localhost:7778`, the board behaves
identically to now, including attach and SSE. Nothing on the daemon changed.

### 2. Cross origin reads

Add an origin allowlist to the human listener: a configured list of origins that get `Access-Control-Allow-Origin`
on `/v1/*`, with no credentials, because there are none. Leave the agent listener alone. Leave `attach.go` alone,
since it already accepts cross origin and says why.

*How you know it works.* A board served by daemon A, with daemon B in its peer list, lists B's cards, opens B's
event stream, and attaches to a terminal on B. With B's origin removed from the allowlist, the reads fail and the
board shows B as unreachable rather than as empty.

### 3. Peers on screen, read only

Fan the list fetches across every configured peer. Tag each card with its peer name and render that on the card.
Merge the stack by `wait_seconds` across peers, which works unchanged because the seconds were computed by the
peer that owns the clock. Render whatever has arrived rather than waiting for the slowest peer, and mark a peer
that failed rather than dropping its cards silently.

*How you know it works.* Two daemons on two machines, one card each, both visible in one stack, correctly ordered
by who has waited longer. Pull the network on one and its section greys within a poll while the other keeps
updating.

### 4. Writes go home

Every mutation posts to the base URL the card came from. That is prompt, message, status, kill, launch, browse,
shutdown, and a permission decision. This is mostly free once stage 1 has made the base URL a property of the
request rather than a constant, but it needs an audit: any call site that still assumes the local daemon is a bug
that will only appear against a remote peer.

*How you know it works.* Approve a permission request raised on a remote daemon and watch the blocked agent
continue on that machine. Shelve a remote card and see the pending request answered with the shelved reason.

### 5. Alerting across peers

The waiting and permission polls, the nag schedule, the title counter and the notifications cover every peer
rather than the selected one. This is the stage that makes (c) safe to leave behind, and it must land before the
switcher is presented as a federated board.

*How you know it works.* A permission request raised on a peer the operator is not looking at produces the same
sound, toast and desktop notification as a local one, and clicking through selects that peer and shows the
request.

### 6. Rules that travel, on purpose

No central rule store. A button that exports this daemon's rules and imports them into a named peer, using
`/v1/rules/export` and `/v1/rules/import` as they stand. Explicit, one direction, visible in the history of both
sides.

*How you know it works.* 134 rules imported on the laptop, pushed to the container, and a command that was
answered by a rule locally is answered by a rule there too, with the same pattern named as the decider.

### 7. Only if needed: the front door

If cross origin proves painful, add `atrium front`: serve the board, hold the peer table, forward `/peer/{name}/*`
including SSE and the attach socket, store nothing. Because stage 1 made the base URL configurable, this is a
change to the peer list and nothing else.

*How you know it works.* Same board, same behaviour, one origin, and stopping the front door leaves every peer
running and reachable directly.

## Open risks

- **Attach over a long link is per keystroke.** Nothing here fixes that. Local echo would be a lie about a
  terminal, so the answer is probably that attach across a slow link is for reading rather than typing.
- **The tunneler's source address is unverified.** The `atrium stop` hazard above rests on it. Test it before
  relying on the loopback rule with any share running, and configure a token in the meantime.
- **Peer count is a poll multiplier.** The board polls every five seconds. Ten peers is ten times the requests,
  which is nothing locally and is not nothing over a metered or high latency link. Backing off per peer on
  failure is the obvious answer and is not designed here.
- **A federated board makes a public zrok share far worse.** One link now hands over every environment rather
  than one. The existing confirmation should say so in those terms once peers exist.
- **Nothing has been run against this document.** It is design only, and the code claims in it were read at the
  commit this was written against rather than tested.
