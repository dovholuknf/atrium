# internal/link

The hub, and the link a room dials to reach it. A room serves the `http.Handler` it already has on a listener whose
`Accept` returns connections it DIALLED, and the hub speaks plain HTTP/1.1 down them. So websockets, server-sent
events and large downloads pass through untranslated. On top of that pipe the hub serves the board's files, merges the
ALL view, runs the control MCP, and owns the hub-only surfaces under `/_hub/`. Read the package comment in `link.go`
first, then `docs/fabric/hub-and-rooms.md`.

## The shape that must not change

- **The room dials, never the hub.** A hub restart is the room noticing a closed socket and redialling on a backoff it
  already has. The room is the stable thing and the hub is the disposable thing. The relay, the git kind and the room
  spec kind are all connections the room dialled too. The hub never reaches into a room of its own accord.
- **The hub holds no work.** No session, no terminal, no agent. Its durable state is `internal/hubstore`, and this
  package reaches it through interfaces (`Inventory`, `AuditLog`, `DepsStore` and others) where it can.
- **If the hub has the file, the hub serves it. Everything else goes to the room** (`proxy.go`). Resolved against the
  directory, not a list of prefixes.
- **A transport supplies a `net.Listener` on the hub and something that returns a `net.Conn` on the room.** Nothing
  more. `direct.go` is mutual TLS over TCP on one port, and the first frame decides enrol, control or data. `ziti.go`
  and `zrok.go` are the overlays, and zrok is private shares only. There is no public mode on purpose.
- **A room's name comes from the hub-signed certificate**, on direct and inside an overlay alike (`overlaytls.go`).
  The overlay decides who may reach the hub, not which room is calling. Rooms that joined an overlay before this keep
  attaching beside it, marked unproven, until `atrium rooms legacy refuse`.

## Scoped versus aggregate

`rooms.go` has the two modes. **Scoped** names a room and the hub is a byte pipe to it: nothing parsed, nothing merged.
**Aggregate** (the ALL view) names none, and the hub fans out and merges, which the archived federation design warned
against and the operator chose on purpose.

- Only the LIST endpoints are merged, and only for reading. A write in the ALL view that is about a machine is answered
  by asking which room, never by guessing.
- **A request that names a card goes to the room that holds it** (`cardroute.go`). The card decides, never the board's
  room header. A card no attached room holds is a 404 naming the card and the rooms asked.
- A handful of writes fan out to every room on purpose (pinned order, prune from ALL, the width floor, context limits,
  input lag, worker policy). Each answers 200 while at least one room took it and names the rest in `unreached`. A
  room that does not answer never stalls the reply.
- The hub holds ONE upstream event stream per room for as long as anybody watches, and deals events out per client.

## Rules a change here can break

- **A hub cannot stop a room.** `/v1/shutdown` and `/v1/restart-wrap` are refused in `proxy.go`, because through a link
  the room's loopback check would pass for anybody who could reach the hub. Once something else can reach a room,
  loopback stops meaning "this machine".
- **`/v1/git/` is never proxied from the board.** A room's git is reached by the hub's own sync and collect only.
- **The hub OFFERS a binary, the room decides, fetches and checks** (`upgrade.go`). Shipping an executable is more power
  than stopping one, so there is no push of a binary down the link.
- **The control MCP's class is NOT a boundary** (`ctlclass.go`). It comes from `X-Atrium-Agent`, which anything on
  loopback can set, and a failed lookup serves the full set. Every handler keeps every check. The worker set is the
  `workerTools` map and only that map, so a new tool lands in `full` alone unless it is named there. Some file headers
  disagree with the map about which tools workers get. Trust the map. An item worker (`atrium:item`, `classWorkerItem`) gets the map less `itemWithheld`
  (`atrium_say`, `atrium_peers`, `atrium_task`, `atrium_report`) and done and blocked words that name no launcher. The
  room's say endpoints refuse its says as well.
- **`atrium_item` (get, list, unblock, cancel) is for owners and is in no worker set** (`items_mcp.go`, `items.go`). The hub owns the
  items and forwards an unblock or cancel to the item's room. `/_hub/items/sync` takes a room's outbox, a replay a no-op,
  and the room name comes from the link. `docs/changes/items-stage2a.md`.
- **The tool list is fixed at the first request.** MCP clients read `tools/list` once, so a retag lands at the next
  session start.
- **Audit lines record a claim, never a proof, and never a payload** (`ctlaudit.go`). Mutating tools are wrapped in
  `audited` at registration. A describe function never reads a text field.
- **Operator-only means `edge.LocalOperator`**: loopback, a loopback Host, no forwarding header. Settings that run a
  program on the hub or widen what the hub answers to (the notify command, extra hosts, launch caps, push keys) are
  set from the hub's machine only. A board reached over a share must not be able to widen the DNS rebinding guard.
- **A card is believed from headers only on the hub's own machine**, where the control server asks from. On the git
  store path card headers are dropped and the push is the operator's.
- **Atrium holds the NAME of a command and never a credential** (`notify.go`). The VAPID private key is atrium's own
  and is never logged, returned or written anywhere but `hub_setting`. A push endpoint is a capability and stays out of
  logs and error text.
- **Only the hub decides a gate is met** (`deps.go`), from what it reads itself or a person on the board. A failed check
  leaves the gate open with the reason. `atrium_deps` has no clear action on purpose.
- **Only the hub talks to a forge** (`forgeroute.go`). A room never runs `gh` or `bb` and never fetches from a forge
  host. The hub only reads.
- **A cross-room launch is never retried.** It is not idempotent, so a failure that may have come after the post is
  `unconfirmed`. A relayed message the target cannot take is the SENDER'S room's to keep.
- **The deploy-ready button names the tip it was shown**, and a mismatch is a 409. There is no timer that deploys.
- **`Silence` is three beats** (`DefaultTimings`). One missed beat on a busy machine must not evict a room holding live
  terminals.
- **`address.go` is a copy of `internal/daemon/address.go`**, since neither package imports the other. Both carry the
  same table test. Change both.
- **`resolve.go` is one rule for two callers**, so a name that works in `atrium_say` works in curl. Across rooms one
  live alias match wins, and anything else is ambiguous rather than whichever room answered first.

## The files

| | |
| --- | --- |
| `link.go` / `protocol.go` | the package comment, `Timings`, and the five frames |
| `hub.go` / `room.go` | the hub's half (accept and lend connections) and the room's half (dial and serve) |
| `direct.go` / `certs.go` / `overlaytls.go` | mutual TLS, the hub's own CA, the join string, TLS inside an overlay |
| `ziti.go` / `zrok.go` | the overlay transports |
| `proxy.go` | what the hub answers with, and what it refuses to pass to a room |
| `rooms.go` / `fanout.go` / `cardroute.go` | scoped versus aggregate, merging lists, routing by card |
| `events.go` / `announce.go` / `roomsevent.go` | the event stream fan-in, the room's cache announcements, the `rooms` event |
| `control_mcp.go` / `ctlclass.go` / `ctlaudit.go` | the control MCP served by the hub, who sees which tools, the audit line |
| `ctl_end.go` / `control_alias.go` / `*_mcp.go` | individual control tools. `*_door.go` is one code shared with the stdio server |
| `relay.go` / `control_relay*.go` / `hubask.go` | a room asking the hub to carry a message, a launch or a backlog call |
| `address.go` / `resolve.go` | how a name is split and turned into a card |
| `launchroute.go` / `launchcaps.go` / `openroute.go` | placing an unscoped launch or open, and the per-room cap |
| `prclaim.go` / `prmove.go` / `prworktreeroute.go` / `pulls.go` | one owning room per PR, moving it, the pulls view |
| `recognisers.go` / `recogniseroute.go` | the hub-owned recogniser table, and recognising a paste on the hub |
| `forgeroute.go` / `forgeci.go` | the forge, run by the hub for its rooms |
| `git.go` / `git_hub.go` / `git_store.go` / `git_url.go` | the git kind, the hub's git side, the store on the board, the lookup |
| `tasks.go` / `tasks_launch.go` | the tasks of an item: the owner's routes, a room's report of one, and the scheduler that starts the next. `docs/runtime/item-tasks-design.md` |
| `backlog.go` / `changerequest.go` / `deps.go` / `flow*.go` | the hub's backlog, change requests, gates and merge flow |
| `docs_api.go` / `files_api.go` | hub documents and the file drop. The hub's own, never proxied |
| `notify.go` / `notifyapi.go` / `push.go` / `pushapi.go` / `webpush.go` | the notify trigger, its command sink, Web Push |
| `growl.go` | what waits on a human, on every screen |
| `autoapprove.go` | board-wide auto mode, enforced by answering pending requests over the relay |
| `deployready.go` / `deployqueue.go` / `deploy_mcp.go` / `newcontexts.go` | deploying, and what a deploy waits for |
| `restartgate.go` | holding a hub restart until nobody is typing |
| `upgrade.go` | a room taking a newer binary, at its own choice |
| `setupwatch.go` / `predates.go` | whether rooms are set up alike, and a room too old for a route |
| `views.go` | a board tree per worker worktree at `<name>.localhost` |
| `hosts.go` | the extra names the hub answers to |
| `contextlimits.go` / `workerpolicy.go` / `inputlag*.go` / `mincolssetting.go` | hub-owned settings passed to every room |
