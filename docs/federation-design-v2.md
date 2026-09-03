# Federation v2: a forum, and the atria that report to it

This supersedes `docs/federation-design.md` in its recommendation and keeps most of its analysis. That document is
left in place because its inventory of what breaks is still the best list of the hard parts, and because being able
to read the earlier reasoning next to the correction is worth more than a clean file. Where the two disagree, this
one is current.

Nothing here is built. Read `docs/overlays.md` first, because part of the transport question is already answered
and shipping.

## 1. Does the previous verdict survive

**Half of it. The ban survives and the recommendation does not.** The previous document ruled out a central atrium
that mirrors other atriums, and that ruling stands untouched: a second durable copy of a card is wrong for the same
reason a stored activity badge is wrong, and Agora has nothing to say about it. But the previous document then
recommended "federate in the client", one browser page opening several daemons directly, and that is the wrong
default. It is the wrong default because it read "hub" and heard "mirror", and answered a question the operator was
not asking. The operator asked for a hub that **reaches out to leaves**, which is a routing question, not a storage
question. Those are separable and the previous document did not separate them.

Three things changed the conclusion.

**A hub that holds nothing is a real option and was not on the previous list.** Topology (d) in the earlier document,
"a stateless front door", was the right idea pointed the wrong way: it dialled outward to peers, so every peer still
had to be reachable. Invert the connection and the same component solves the reachability problem too.

**The leaf's board is already one `http.Handler`.** `internal/daemon/daemon.go:478` builds the human server as
`&http.Server{Addr: d.opts.HumanAddr, Handler: d.ap.Handler()}`. That handler is the entire board API. Serving that
same handler on a connection the leaf dialled out is `http.Serve(l, d.ap.Handler())` and nothing else. There is no
new protocol to design, no new endpoint, and no second representation of a card, because the forum forwards bytes to
a handler that already exists and already answers correctly.

**Agora, read properly, dissolves the transport half of the authentication objection.** Not the whole objection. The
detail is in section 4, and it comes with a cost the previous document did not have to price.

**Verdict.** Build a storeless forum that leaves dial out to. Keep client-side federation as the fallback for the
case where the operator's browser is already an overlay endpoint and one hop of latency on attach matters. Never
build the mirroring aggregator.

## 2. What the operator asked for, restated

> "i am not convinced that an 'atrium proxy' isn't a good idea ... i wanted a single pane of glass. i don't want 85
> panes of glass ... it seems like it could fit by just federating and aggregating other atrium instances."

Two requirements, and they are independent.

1. **One address.** One bookmark, one origin, one notification permission, one place the phone points at.
2. **One list.** Cards from every machine in one stack, ordered by who has waited longest, with the alerting loop
   covering all of them.

Client-side federation delivers (2) and fails (1). A forum delivers both. That is the whole difference, and it is
larger than it sounds, because (1) is what makes the thing usable from a device that is not the workstation.

## 3. Hub-out versus client-side, concretely

```
  (A) client-side federation                   (B) hub-out federation

  browser ────── must reach ─────┐             browser ──── must reach ────┐
     │                           ▼                                         ▼
     ├─▶ atriumd  laptop   :7778                                  ┌────────────────┐
     ├─▶ atriumd  buildbox :7778                                  │ forum (no db)  │
     ├─▶ atriumd  docker   :7778                                  └────────────────┘
     └─▶ atriumd  k8s pod  :7778                                     ▲    ▲    ▲
                                                                     │    │    │  leaves DIAL
     every leaf must be published                          atriumd ──┘    │    └── atriumd
     every leaf must be reachable                            laptop  atriumd      k8s pod
     from the browser specifically                                   docker
                                                          nothing on a leaf is published
```

The difference is entirely about which direction the first packet travels. Here is what that means in each of the
three environments the operator named.

### A laptop behind NAT, or on a coffee shop network

Client-side works only if the laptop publishes itself, which today means running `zrok share` or `ziti tunnel host`
on the laptop. That already ships (`docs/overlays.md`) and it works. So for this case client-side is not blocked,
it is just one published service per machine, and each one is a separate origin the browser has to be allowed to
read. Hub-out replaces N shares with one outbound TCP connection from the laptop and no share at all.

### A container on a bridge network

The container has an address on a docker network that the browser has no route to. Publishing means `-p` mapping
the board port out of the container to the host, which works for one container and starts colliding at three. It
also means every container image ships with a tunneler or an exposed port. Hub-out needs neither: the container
dials the forum on the host, because outbound from a bridge network is the default and needs no configuration at
all. **This is the case where the two shapes stop being equivalent.**

### A pod in kubernetes

Client-side means a Service, and if the browser is outside the cluster, an Ingress or a LoadBalancer per pod, with
a hostname or a path prefix per agent. Pods are also mortal and rescheduled, so the published address is a moving
target that something has to keep pointed at the right pod. Hub-out means the pod dials `atrium-forum.default.svc`
or an address outside the cluster, and a rescheduled pod simply reconnects. Nothing in kubernetes has to be told
that the pod exists.

### The browser itself

This is the argument that decides it. Under client-side federation the **browser** has to be an endpoint of whatever
network the leaves are published on. Over zrok private shares that means `zrok access private` running next to the
browser, one per share. Over OpenZiti it means a tunneler on the machine holding the browser, or a mobile ziti
client on the phone. Under hub-out only the forum is on that network. The browser needs one ordinary HTTP address,
which is the difference between "the board works on my phone" and "the board works on my phone once I have enrolled
my phone".

### Summary

| | client-side | hub-out |
| --- | --- | --- |
| Leaves that must be published | every one | none |
| Origins the browser talks to | one per leaf | one |
| Works from a bridge-network container | needs a port map per container | yes, unchanged |
| Works from a kubernetes pod | needs a Service plus Ingress per pod | yes, unchanged |
| Browser must be an overlay endpoint | yes | no |
| Attach latency | browser to leaf, direct | one extra hop through the forum |
| New component to run | none | the forum |
| CORS work on the daemon | required | none |

Client-side is not dead. It is better on exactly one axis, attach latency, and that axis matters when the operator
is on the same LAN as the leaf and typing rather than reading. Both shapes can coexist, because the board only ever
needs a base URL per peer: pointing that at `https://forum/atrium/laptop` or at `http://laptop:7778` is a
configuration difference, not a code difference. Write the base URL indirection once and the choice stays open.

## 4. Does Agora dissolve the authentication objection

The previous objection, quoted from `docs/federation-design.md`: "A peer accepting writes from the central daemon is
a trust decision between two processes, which is authentication, ruled out in `CLAUDE.md`."

Re-tested, the objection **splits into two questions and only one of them was ever real.**

### The leaf side: there is nothing to authenticate

The leaf dials out. It was configured with a forum address the way it is configured with a database path. It accepts
requests on a connection it opened to a destination it chose. There is no inbound acceptance decision, so there is
no principal to identify and no credential to check. This is the same category of trust as `--agent-addr` or a zrok
reserved token: a statement of configuration, not an authentication system. **The previous objection does not apply
to this direction at all**, and it applied to the previous shape only because the previous shape had the forum
dialling in.

### The forum side: this is where the question is real

When a connection arrives at the forum claiming to be `laptop`, may it be? Three possible answers.

**(i) The overlay already answered it.** The forum is bound to loopback, or published as a ziti service or a private
zrok share whose policy already names who may dial. Atrium checks nothing, holds no key, and issues no token,
because reachability is the authorisation and it is administered somewhere else. This is exactly the posture
`docs/overlays.md` already takes, and it needs no code. Its limit is honest and must be written down: any leaf that
can reach the forum can claim any peer name, so the leaves in one forum trust each other. For one operator's
machines that is acceptable. For anything else it is not, and (ii) is the upgrade path.

**(ii) The forum consumes an identity the overlay issued.** This is the one Agora is relevant to, and it is real
rather than hypothetical. Agora's SDK exposes `tunnel.Listen(ctx, agent, nameOrID) (net.Listener, error)`
(`D:/git/github/openziti/agora/sdk/agent/tunnel/primitive.go:154`) and `tunnel.Dial(...) (net.Conn, error)` (same
file, `:299`). Both shipped in v0.1.5's line as "New SDK primitives supporting low-level Dial and Listen access to
layer 1 tunnels" (`CHANGELOG.md:37`). `tunnel.Listen` returns the raw overlay listener, and the overlay is OpenZiti:
`D:/git/github/openziti/agora/internal/network/tunnelruntime/overlay.go:39-48` calls

```go
return c.ctx.ListenWithOptions(serviceName, &ziti.ListenOptions{
    DoNotSaveDialerIdentity:      false,
    ...
})
```

`DoNotSaveDialerIdentity: false` is the fabric being told to retain who dialled, and Agora already reads it back.
`overlayPeerInfoFromConn` (`internal/network/tunnelruntime/logging.go:45-66`) type-asserts an accepted conn to
`edge.ServiceConn` and calls `GetDialerIdentityId()` and `GetDialerIdentityName()`, marking the result
`router_attested` when either is present. So the mechanism is not a theory. It is running code in the repo, proven
against a real fabric, and the identity is established by the edge router's mTLS session before a byte of atrium's
protocol would run. Atrium would not be inventing authentication. It would be reading a name off a connection,
which is precisely what `CLAUDE.md` says to do: "If we ever needed auth, it would go through Agora, not homegrown."

Four caveats, all load-bearing.

- **Agora reads the identity and then throws it away.** `overlayPeerInfo` is an unexported struct in an `internal/`
  package and its only consumers are `dl.Infof` log lines (`logging.go:73-97`, `:187-198`). No exported API returns
  it. An embedder reading the dialer name from a `tunnel.Listen` listener does the type assertion itself against
  `openziti/sdk-golang`, so atrium would depend on sdk-golang directly, not only on agora.
- **There is a way to avoid needing it at all, and it is better.** Agora writes one dial policy per attachment,
  naming exactly one environment identity: `IdentityRoles: []string{"@" + spec.EnvironmentIdentityID}`
  (`internal/fabric/openziti/automation/tunnel.go:160`). If the forum creates **one tunnel per atrium** rather than
  one shared tunnel, then only that atrium's identity can dial each one, enforced by the router. Which listener a
  connection arrived on is then the identity, and no conn-level assertion is needed anywhere. Fan-in is the only
  case that needs the per-connection read, and this design does not have to be fan-in.
- **The SDK path breaks the "atrium never handles an identity" rule in `docs/overlays.md`.** That rule holds today
  because atrium shells out to a tunneler and the tunneler owns the key file. The SDK path opens the identity
  in-process: `ziti.NewConfigFromFile(identityPath)` at `overlay.go:28`. That is a genuine change of posture and has
  to be written down rather than slipped in. Note that `docs/architecture-v2.md:518` already contemplates it: "Bind
  the daemon to an OpenZiti service instead of, or alongside, loopback."
- **Agora's managed tunnel path does the opposite.** `agora tunnel serve` forwards to a `BackendTarget`, described
  as "the local target the runtime forwards to" (`sdk/agent/tunnel/types.go:69`), so a backend behind the managed
  runtime sees a local connection with no identity on it, exactly like `ziti tunnel host`. Only the SDK-native
  `tunnel.Listen` path preserves it. Choosing agora for the transport does not by itself get you the identity.

Two things to stay away from. `tunnel.Listen` refuses UDP and refuses any tunnel that is not `direct`
(`sdk/agent/tunnel/primitive.go:416-433`), which is fine because the forum link is TCP. And Layer 2 is the wrong
layer entirely: an envelope's `SenderAccountID` is written by the sender and verified by nobody
(`sdk/agent/session/envelope.go:31`, and `docs/current/layer-2/envelopes.md:83` says so outright, "envelope-level
signing is post-MVP"). The forum wants Layer 1 tunnels and nothing above them.

**(iii) Atrium mints its own tokens.** Still ruled out. Nothing here argues for it.

### So: does the objection survive

**It survives as a rule and stops being an obstacle.** The rule is "never mint a credential", and both (i) and (ii)
obey it. The previous document treated the objection as fatal to every hub shape. It is not. It is fatal only to a
hub that would have to invent a trust relationship, and a forum that leaves dial into does not have to invent one.

### What Agora costs, and why it is not stage one

Agora is not a library you link. Four things must be running before two processes can speak: PostgreSQL, an
OpenZiti controller, at least one enrolled edge router, and the agora controller itself
(`D:/git/github/openziti/agora/README.md:141-147`, `:117-118`, and `docs/current/architecture/overview.md:63-68`,
"A controller plus PostgreSQL is the core deployment model"). Before that, an organisation and an account have to
exist, because the unit of cryptographic identity is an *environment* enrolled under an account under an
organisation, and the ziti identity it gets is literally named `<orgId>-<accountId>-<envId>`
(`internal/controller/enableEnvironment.go:102`). Its authorisation primitive for a tunnel is a list of granted
account emails (`sdk/agent/tunnel/types.go:72-74`).

So adopting agora means adopting the noun `account`, which `docs/architecture-v2.md:37` rules out as a non-goal, and
it means standing up a database and a fabric to let two of one person's machines share a list. Agora is also pre-1.0
at v0.1.5 and says so (`README.md:11-12`), with Layer 1 described as "minimum-working" (`README.md:50`). That said,
the specific parts this design would use are the finished ones: `docs/current/layer-1/status.md:13` reads "Minimum
working Layer 1 is achieved", and its checklist explicitly ticks "an embedded process can provision a direct tunnel
and serve its own protocol on a raw overlay listener" and the matching dialer item (`status.md:50-51`). The maturity
risk here is lower than the version number suggests. The deployment weight is the real cost.

That is a lot of stack to stand up before two atriums can share a list. The honest sequencing is: build the forum
with the transport it needs and no more, take answer (i), and treat (ii) as a transport swap for the day the forum
stops being single-operator. The forum should therefore accept its inbound leaf connections through an interface
narrow enough that a `net.Listener` from agora, from sdk-golang, or from `net.Listen` are interchangeable. That is
one interface and it costs nothing to write now.

## 5. Which direction the connection goes, and what it costs

```
  leaf (atriumd)                                   forum (no store)
  ┌──────────────────────────┐                     ┌────────────────────────────┐
  │ :7777 agents  (loopback) │                     │ :7779 leaf listener        │
  │ :7778 board   (loopback) │ ── dials out ─────▶ │ :7780 board, one origin    │
  │ sqlite · gate · ptys     │                     │ routing table, in memory   │
  │                          │ ◀── requests ────── │ no sqlite, no halt         │
  │ serves d.ap.Handler()    │     back down       │ never parses a card        │
  │ on the dialled conn      │     the same conn   └────────────────────────────┘
  └──────────────────────────┘                                  ▲
      still fully usable at                                     │
      localhost:7778 alone                              browser, one address
```

### What the leaf gives up

**Nothing it had, and one thing it did not have to think about.** The leaf keeps its store, its gate, its ptys, its
reaper, its agent listener and its own board. It gains an outbound connection, a reconnect loop, and one new way to
be reached.

The one it did not have to think about is the widening of "loopback". `handleShutdown`
(`internal/daemon/shutdown.go:67-83`) trusts a loopback source address unless a token is configured, and it already
refuses that trust while a share is running, because a tunneler terminating on this machine makes every request look
local. The check is `d.sharing()` (`internal/daemon/overlay_api.go:136`), which today looks only at the zrok and
ziti overlay states. **An open forum link is the same hazard and `sharing()` does not know about it.** A leaf with a
forum link must count as sharing. That is a small change and it must land in the same stage that first lets the
forum reach the leaf, not after.

### What happens when the forum is down

**The leaf keeps working alone, completely, and this is not negotiable.** Checked against the resilience guarantees
in `CLAUDE.md`:

- *Guarantee: storage failure halts, it does not degrade.* Unaffected. The forum has no store, so it cannot halt.
  The leaf's halt (`internal/daemon/daemon.go:210-226`) closes the leaf's agent listener and leaves the leaf's board
  up to report the cause, exactly as now. The forum renders that leaf's banner from the leaf's own `/v1/health`,
  forwarded, and a leaf it cannot reach at all is a failed fetch rather than a state to model.
- *Guarantee: a hook must never fail a session.* Unaffected, because hooks never touch the forum. They find their
  daemon through `internal/daemon/whereami.go` and default to `localhost:7777`. Agent traffic never leaves the
  machine. That property was the previous document's strongest argument and it is preserved here rather than traded.
- *Guarantee: `/activity` is fire and forget.* Unaffected for the same reason.
- *Guarantee: a kill is not a stop.* Unaffected, and see the `sharing()` note above.

Two rules follow and they should be written into the code as comments, not just here.

**The forum link runs in its own goroutine and its failure is logged, never returned.** A daemon that refused to
start because a forum was unreachable would have inverted the entire dependency. The link retries on the backoff
shape the v1 agent already uses (`CLAUDE.md`, Mode A resilience guarantee 1) and says so once, then rarely.

**The forum is never the only way to answer a permission request.** The leaf's own board stays bound and stays
usable. If the forum is down, the operator opens the leaf directly and the gate still answers. This is what makes
the forum additive: at worst it removes a convenience, never a capability.

### What it costs

One extra hop on every request, including every keystroke on attach. One process to run and keep alive, whose death
darkens the pane of glass even though every leaf is healthy. A reconnect loop per leaf. And a temptation, discussed
next.

## 6. What the forum holds

Three options. The previous document said a hub needs "a second copy of every card and a namespace for identity it
did not issue". That claim is **true of a hub that stores cards and false of a hub that routes**, and the previous
document did not distinguish them.

### (a) Nothing: a routing table

`{peer name -> the live connection that leaf dialled in on}`, in memory, plus liveness. Every request from the
browser is forwarded down that connection to the leaf's existing handler and the response is passed back unread.
The forum never decodes a card, never learns what a status is, and has no opinion about a halt.

This works because of the fact in section 1: the leaf's whole board API is one `http.Handler`. Concretely, the leaf
runs `http.Serve(dialedListener, d.ap.Handler())` and the forum runs an `http.Client` whose `Transport.DialContext`
returns a connection from that leaf's pool. The forum's own handler is a path rewrite:
`/atrium/{peer}/v1/...` becomes `/v1/...` against that client. SSE and the attach WebSocket both survive this
untouched, because neither is parsed.

The cost is fan-out. Rendering a merged stack of N leaves is N forwarded requests, and the board polls every five
seconds. That is the same request count client-side federation would make, arriving at one origin instead of N.

### (b) A cache, in memory only

The forum subscribes to each leaf's SSE stream and holds a last-known task list per peer, so a page load is one
request instead of N and the alerting loop does not have to fan out. Every entry is tagged with when it arrived, and
a stale entry is rendered as stale rather than as current.

This is defensible and it has a precedent that constrains it exactly: activity is held in memory and never written
down, because a stored activity is a lie the moment the process restarts (`docs/activity-design.md`). A forum cache
obeys the same rule for the same reason and dies with the forum. It is a legitimate stage-six optimisation. It is
not stage one, because (a) is correct without it and it can be added without changing anything a leaf does.

### (c) A copy, on disk

Rejected, and now for a sharper reason than the previous document had. **A forum with a store must halt when that
store fails.** That is guarantee 1 and there is no version of atrium where it does not apply. But a forum halting
takes the only pane of glass down while every leaf is healthy, which is the exact inversion of what the halt exists
to do: the surface that explains the failure must survive it. So a durable forum is either a daemon that breaks the
halt rule or a daemon the halt rule breaks. Neither is shippable.

**Therefore the forum is storeless, and that is a derived property rather than a preference.** It follows that the
forum is not an `atrium daemon` with a flag. It is a different thing with no sqlite, no migrations, no cards, and no
`WORKTREE_ROOT`.

## 7. The hard parts, re-tested

Each row compares hub-out against client-side federation, not against today.

| Hard part | Hub-out | Why |
| --- | --- | --- |
| `wire_name` collisions | unchanged | Each leaf keeps its own table. The forum never joins them. |
| Permission gate in leaf memory | slightly better | Decisions still forward to the owner. Alerting gets one home. |
| Pty attach | worse by one hop | The forum must proxy the socket. Client-side goes direct. |
| The halt | unchanged | Per leaf, per store, rendered per column. A storeless forum adds no second halt. |
| `atrium stop` | worse, needs a guard | A forum link widens "loopback" as a share does. `sharing()` must say so. |
| Reaper and pids | unchanged | Every leaf reaps its own pids with its own syscalls. |
| Ages and clock skew | unchanged | Seconds are computed by the leaf and forwarded as numbers. |
| CORS | better | One origin. The daemon's HTTP surface is not touched at all. |
| `hostname` on a card | unchanged, still wrong | Pre-existing. See below. |

### `wire_name` collisions

`wire_name` is `UNIQUE` on the task table (`internal/store/schema.go:35,38`), and `Register` matches on it first
(`internal/store/tasks.go:68-74`), so a collision does not error, it silently hands one session another session's
card. Two naming paths feed it and they are not equally dangerous.

- A launched runner gets `fmt.Sprintf("%s-%d", filepath.Base(cwd), time.Now().UnixNano()%100000)`
  (`internal/daemon/launch.go:134`). The five-digit suffix makes a same-directory collision roughly one in a hundred
  thousand.
- A joined session gets `filepath.Base(cwd)` with no suffix at all (`internal/cli/join.go:64-77`), and the
  permission hook must derive the same name or a session would join under one name and be gated under another. Two
  containers built from one image, both working in `/work/repo`, both running `atrium join`, both reporting to the
  same daemon, are a **certain** collision.

Federation neither causes this nor fixes it. It is a per-leaf bug today and it stays one. It is worth stating that
the forum makes it *visible* rather than worse: with several machines on one board, two cards that should be
distinct being one card is something the operator will now actually notice. The fix is orthogonal and small, and it
belongs in the backlog rather than in this design: give the join path the same suffix the launch path has, or seed
the name from the hostname.

### The permission gate

`HandlePermission` (`internal/hub/hub.go:407`) records the request and then blocks on a channel held in a map in
that process, with no internal timeout (`hub.go:461-470`), until something calls `decide`. Everything the chain
consults before that is local to the leaf: queued messages, the card's shelved status, `MatchRule` against that
leaf's `perm_rule` table, and auto mode (`internal/daemon/daemon.go:340-405`).

So a decision has to arrive at the process holding that channel. The forum cannot answer on a leaf's behalf and must
not try. It forwards the POST and the leaf's own handler unblocks its own channel. Because the channel has no
timeout, the extra hop's latency is irrelevant.

Hub-out is marginally **better** than client-side here for one reason: the nag. The whole point of the alerting loop
is that a permission request nobody is shown freezes an agent. Under client-side federation the browser must poll
every peer, which means the browser must be able to reach every peer, which is the reachability problem again. Under
hub-out the forum can hold the fan-out and the browser polls one address. If the forum later grows the in-memory
cache of option (b), it can also drive the nag while no tab is open at all, which nothing else in this design can.

One failure mode to name: if the forum dies while a decision is in flight, the leaf's handler still holds the
channel and the agent stays blocked. That is exactly what happens today when a browser tab closes mid-decision, so
it is not a new class of failure, and the answer is the same: reopen the surface and answer again.

### Pty attach

`docs/supervision-design.md` and the ConPTY section of `docs/architecture-v2.md:430` establish that the daemon owns
each pseudo terminal, that closing one takes the attached process with it, and that ConPTY offers no reattach. None
of that changes and none of it can. A pty stays on the machine that created it, which is the strongest argument for
a whole daemon per environment and it is unaffected by which way the connection goes.

What travels is the WebSocket. `internal/daemon/attach.go:46-52` accepts with `InsecureSkipVerify: true` and says
why: a stricter origin check would break reaching the board over an overlay. So the socket already tolerates being
reached from elsewhere.

Hub-out is **worse** here than client-side, by one hop, on a path that is character-at-a-time. Client-side goes
browser to leaf. Hub-out goes browser to forum to leaf, and the second leg rides the leaf's dialled connection.
There is no fix and local echo would be a lie about a terminal. The honest position is that attach across a long
link is for reading, and that an operator who needs to type into a runner on a machine they can reach directly
should point the board at that leaf directly. Since the board only ever holds a base URL per peer, that is a
dropdown, not a rewrite.

### The halt

`onHalt` (`internal/daemon/daemon.go:210-226`) closes the agent listener, leaves it closed, and keeps the human
listener up to report the cause. The store halts on any non-transient error (`internal/store/store.go:275-301`).
That is a fact about one store on one machine, and a storeless forum introduces no second one. The board's banner
becomes per peer, driven by that peer's forwarded `/v1/health`, and a peer that cannot be reached at all is a failed
fetch rather than a state anybody stores. This is the same answer client-side federation gets, arrived at the same
way.

### `atrium stop`

Covered in section 5 and repeated here because it is the one item that gets actively worse. Today the shutdown
endpoint refuses a loopback claim while a share is running (`internal/daemon/shutdown.go:67-83`, gated on
`internal/daemon/overlay_api.go:136`). A forum link has the identical property and is not counted. Two rules:

- **A leaf with an open forum link counts as sharing.** The shutdown endpoint then demands its token, which is the
  correct and already-implemented behaviour.
- **The forum never fans out a stop.** A stop button on the board names one peer and forwards to that peer only.
  There is no "stop everything" and there should not be one.

### The `hostname` column

`observedFor` fills `hostname` from the daemon's own `os.Hostname()` (`internal/daemon/daemon.go:250-256`). That is
right by accident today, since the agent is always on the daemon's machine, and it stays right under this design for
the same reason: a leaf only ever records cards for agents on its own machine. It would have become wrong under the
mirroring aggregator, where a card can describe work elsewhere. One more small confirmation that the copy is the
part that hurts, not the routing.

## 8. Naming

The operator floated roots and trunks. Atrium's metaphor is architectural: an atrium is the open central court of a
Roman house, the space everything else opens onto. A tree grafted onto a building fights the vocabulary rather than
extending it, so the arboreal scheme is listed and rejected. There is also one hard constraint: **`atrium hub` is
taken.** It is the Mode A server (`internal/cli/cli.go`, `internal/hub/`), still shipping and still working, and
reusing the word would collide with the one surface future maintainers are most likely to confuse.

### The candidates

**1. Roman civic: one forum, many atria.** The aggregator is `atrium forum`, each machine runs an atrium, and a leaf
is "an atrium". A forum is the shared open space that private houses face onto, which is exactly the relationship:
the operator goes to the forum, the work stays in the houses. The plural is free and correct, "atria", and it reads
naturally in prose and in a log line. The cost is that "forum" carries message-board baggage, and that a forum is
somewhere people gather while this component is somewhere requests pass through.

**2. Roman threshold: vestibule, or portico.** The *vestibulum* is the passage from the street into the atrium, and
it holds nothing. That is a startlingly exact description of a storeless forwarding proxy. `atrium portico` is more
pronounceable and means the covered front you pass under. Both are precise about the mechanism and useless at
conveying the payoff, which is aggregation. Nobody reading "portico" guesses "one board for every machine".

**3. Water: compluvium and impluvium.** The *compluvium* is the opening in the roof and the *impluvium* is the basin
everything drains into. As a metaphor for aggregation it is the best on the list. As a command anyone can spell or
remember it is the worst. Rejected on ergonomics alone.

**4. Plain mechanical: `atrium relay`, or `atrium front`.** Says what it does, holds no metaphor, and cannot be
misread. `relay` is the most accurate word in this document for what the component actually is. It is also flat, and
it gives the leaves no name at all, which matters because "the atria" is a phrase the docs will need constantly.

**5. Arboreal: roots, trunks, canopy.** Rejected. It requires the reader to hold two metaphors, and it inverts the
one atrium already has: in a tree the root is the source and the leaves are the extremities, but here the leaves own
every fact and the root owns nothing.

### The pick

**Scheme 1. `atrium forum`, and a leaf is an atrium.**

It extends the existing vocabulary instead of adding a second one, it gives both halves a name so the documentation
can say "three atria reporting to one forum" without inventing a word, it does not collide with `atrium hub`, and it
is spellable. The objection that a forum is a gathering place and this is a passage is real but points the right
way: the forum is where the **operator** gathers everything, which is the feature, and the fact that it is
implemented as a passage is an implementation detail the name should not leak. `relay` is the runner-up and is the
right word to use inside the code, in package and type names, where accuracy about the mechanism beats metaphor.

The vocabulary that follows:

| Term | Meaning |
| --- | --- |
| forum | The storeless aggregator. `atrium forum`. |
| atrium / atria | A leaf daemon and its machine. What `atrium daemon` already is. |
| report to | What an atrium does to a forum. `atrium daemon --forum <addr> --as laptop`. |
| the floor | Optional, later: the merged card stack rendered by the forum. Only if a word is needed. |

## 9. Implementation plan

Each stage ships on its own and leaves the single-machine case exactly as it is today. Stage 1 is a day.

### Stage 1: an atrium reports, a forum lists

`atrium forum --listen :7779 --board :7780`. No database, no migrations, no `WORKTREE_ROOT`. It holds a map of peer
name to a live connection and nothing else. `atrium daemon --forum http://host:7779 --as laptop` opens one outbound
connection at startup and keeps it, retrying forever on the v1 backoff shape, logging once and then rarely. The
forum answers `GET /peers` with `{name, connected_since, last_seen}`.

Nothing is forwarded yet. This stage exists to prove the direction and the reconnect loop in isolation.

*How you know it works.* Two daemons on one machine with different ports and different databases, one forum, and
`curl :7780/peers` lists both. Kill one daemon and it drops off within a poll. Kill the **forum** and confirm both
daemons keep serving their own boards and their own agent listeners with no error path taken, then restart the forum
and watch both reconnect with no daemon restart. That last check is the guarantee in section 5 and it is the whole
point of the stage.

### Stage 2: the forum forwards, and the leaf serves the handler it already has

The leaf keeps a small pool of outbound connections to the forum. The forum builds one `http.Client` per peer whose
`Transport.DialContext` takes a connection from that peer's pool. The leaf serves `d.ap.Handler()`
(`internal/daemon/daemon.go:478`) on those connections. The forum's handler rewrites `/atrium/{peer}/v1/...` to
`/v1/...` and forwards, streaming the body without buffering so SSE works.

A connection pool avoids a multiplexer and therefore a new dependency. `go.mod` today has `coder/websocket` and no
stream multiplexer. If the connection count becomes a problem, yamux over one dialled connection is the swap and it
does not change anything above it.

**Land the `sharing()` fix in this stage.** An open forum link is a share for the purposes of
`internal/daemon/shutdown.go`. This is the stage where the forum can first reach the leaf, so it is the stage where
the loopback rule stops meaning what it says.

*How you know it works.* `curl :7780/atrium/laptop/v1/tasks` returns the same JSON as `curl :7778/v1/tasks`, byte for
byte. `curl -N :7780/atrium/laptop/v1/events` receives the `: ping` comment the SSE loop emits every 25 seconds
(`internal/api/api.go:881,898`). `POST :7780/atrium/laptop/v1/shutdown` with no token is refused with the share
message, and refused **because of the forum link**, with no zrok or ziti share running.

### Stage 3: the board, served by the forum, one peer at a time

The forum serves the existing board asset. Route every fetch in the page through a helper that prefixes a base URL
rather than emitting a relative path, and add a peer picker. Selecting one peer gives exactly today's board pointed
somewhere else, so no invariant is touched.

This is the stage that makes the base URL a property of a request rather than a constant, which is what keeps
client-side federation available as an alternative later at no extra cost.

*How you know it works.* With one atrium reporting, every existing board function works through the forum: the
stack, the detail pane, prompting, shelving, launching, browsing, rules, and the permission queue. Attach is stage 5
and is expected to fail here.

### Stage 4: many peers, merged, read then write

Fan the list fetches across every reporting atrium. Tag each card with its atrium name and render it. Merge the
stack by `wait_seconds`, which works unchanged because those seconds were computed by the daemon that owns the clock
(`internal/api/api.go:226-241`). Render whatever has arrived rather than waiting for the slowest peer, and mark a
peer that failed rather than dropping its cards silently.

Then audit every mutation. Prompt, message, status, kill, launch, browse, shutdown and a permission decision must
each post to the atrium the card came from. Any call site that still assumes the local daemon is a bug that only
appears against a remote peer.

*How you know it works.* Two machines, one card each, both in one stack, correctly ordered by who has waited longer.
Pull the network on one and its cards grey within a poll while the other keeps updating. Approve a permission
request raised on the remote atrium and watch the blocked agent continue on that machine. Shelve a remote card and
see its pending request answered with the shelved reason.

### Stage 5: attach through the forum

Forward the WebSocket. `internal/daemon/attach.go:46-52` already accepts without an origin check and says why, so
nothing on the leaf changes. The forum copies frames in both directions and understands none of them.

*How you know it works.* A supervised runner on a remote atrium, attached from the forum's board, showing its ring
buffer backlog on connect and echoing typed input. Measure the round trip and write the number into
`docs/backlog.md`, because the answer to "is remote attach typeable" is a number, not an opinion.

### Stage 6: alerting across every atrium

The waiting and permission polls, the nag schedule, the title counter and the desktop notifications cover every
reporting atrium rather than the selected one. This is the stage that makes the peer picker safe to leave behind,
because a permission request nobody is shown freezes an agent, and that is the one failure the project exists to
prevent. It must land before the forum is described anywhere as a federated board.

If the fan-out cost is unpleasant, this is where the in-memory cache of section 6(b) earns its place: the forum
subscribes to each atrium's SSE stream and holds a last-known list, tagged with its age, in memory, never on disk.

*How you know it works.* A permission request raised on an atrium the operator is not looking at produces the same
sound, toast and desktop notification as a local one, and clicking through selects that atrium and shows the
request.

### Stage 7: only if the trust boundary changes, an overlay-native leaf listener

Keep the forum's inbound side behind an interface that only needs `Accept() (net.Conn, error)`. Then swapping
`net.Listen` for `tunnel.Listen` from agora (`sdk/agent/tunnel/primitive.go:154`), or for an sdk-golang listener
directly, is one constructor.

Prefer **one tunnel per atrium** over one shared tunnel. Agora writes a dial policy naming exactly one environment
identity per attachment (`internal/fabric/openziti/automation/tunnel.go:160`), so a tunnel per atrium means the
router already guarantees that only that atrium can dial that listener. The routing table is then keyed by which
listener accepted the connection, which is a verified fact, and `--as` becomes a display label with no authority.
The alternative, one shared tunnel plus a per-connection identity read, works too but needs a type assertion to
`edge.ServiceConn` that agora performs internally and does not export
(`internal/network/tunnelruntime/logging.go:45-66`).

Do this when the atria in one forum stop being one operator's machines, and not before. Write the posture change
into `docs/overlays.md` in the same commit, because "atrium never handles an identity" stops being true the moment
`ziti.NewConfigFromFile` runs in this process.

*How you know it works.* An atrium whose identity is not permitted to dial the forum's service cannot reach it at
all, and the failure is on the fabric rather than in atrium's logs. An atrium that is permitted appears under the
name its identity carries, and passing a different `--as` does not change which routing slot it lands in.

## 10. Rules this design must not break

Stated separately so they can be checked against a diff rather than inferred from prose.

1. **The forum has no database.** Not sqlite, not a file, not a cache on disk. Derived in section 6(c).
2. **An atrium works alone.** Its board, its gate, its store and its ptys are unaffected by whether a forum exists,
   is reachable, or has ever been configured.
3. **Agent traffic never leaves the machine.** Hooks and runners keep talking to `localhost:7777` through
   `internal/daemon/whereami.go`. No federation feature may put a link in that path.
4. **Never publish `:7777`.** It carries the permission gate with no authentication of any kind, because it was
   built for loopback. This was already the rule and federation makes it more tempting to break.
5. **A forum link counts as a share** for the shutdown endpoint. Section 5.
6. **The forum forwards, it does not decide.** It never answers a permission request, never computes an age, never
   invents an id, and never writes a card.
7. **Never mint a credential.** Reachability is the authorisation, or the overlay supplies a verified identity.
   Nothing in between.

## 11. Open risks and what could not be verified

- **The per-connection identity read was verified in agora, not exercised.** `overlayPeerInfoFromConn`
  (`internal/network/tunnelruntime/logging.go:45-66`) asserts to `edge.ServiceConn` and reads
  `GetDialerIdentityId()` and `GetDialerIdentityName()`, so the API exists and agora calls it. Nothing was run
  against a live fabric, and the name format `<orgId>-<accountId>-<envId>` is produced by
  `internal/controller/enableEnvironment.go:102` but parsed nowhere and validated by nothing. Stage 7's
  one-tunnel-per-atrium variant avoids depending on any of it, which is why it is the recommended variant.
- **Nothing was run.** Every code claim here was read at the current working tree, not executed. The working tree
  also has uncommitted changes in `internal/daemon`, `internal/api` and `internal/store`, so line numbers may drift.
- **Attach latency through two hops is unmeasured.** Stage 5 says to measure it. Until then "for reading, not
  typing" is a prediction.
- **The connection pool is asserted, not prototyped.** Serving `d.ap.Handler()` over a dialled connection is
  standard Go and the handler is verified to exist at `internal/daemon/daemon.go:478`, but no prototype was built.
  The unknowns are connection lifetime under an idle SSE stream and how many spare connections a leaf should hold.
- **Poll cost multiplies by peer count.** The board polls every five seconds. Ten atria is ten times the requests
  arriving at one forum. Backing off per peer on failure is the obvious answer and is not designed here.
- **A forum makes a public zrok share far worse.** One link would hand over every environment rather than one. The
  existing confirmation dialog should say so in those terms once a forum exists.
- **`atrium forum` shares a binary with `atrium hub`.** Two aggregating-sounding subcommands, one from v1 and one
  from v2, with nothing in common. `CLAUDE.md` already warns that future agents must know which surface they are
  touching. This adds one more thing to get wrong and the subcommand table needs a line saying so.
