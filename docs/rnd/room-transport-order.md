# The room link over more than one transport, in order

Status: decided and built on `claude/room-transport-order`. Every decision below was made without an interview,
because none of them needed clint: the order he asked for is the default, and everything else follows from rules
already written in `internal/link/AGENTS.md` and `docs/fabric/ziti-zrok-flow-design.md`.

## The problem

A room reaches its hub over exactly one transport today, the one its join string named. When that path breaks the
room retries the same path forever. m1mini and sg3 are in that state now: direct mTLS to the hub stopped answering
and they cannot re-attach, even though an OpenZiti service could reach the same hub.

What is wanted: the hub holds an ORDERED list of transports (clint's default is direct first, openziti second),
every room tries them in that order, and a room can override the order locally.

## The transports, and what is built now

| | hub listens | hub tells rooms | room can dial |
| --- | --- | --- | --- |
| direct (mTLS over TCP) | yes | yes, its advertised `host:port` | yes |
| openziti | yes | yes, the service name | yes, with its own enrolled identity |
| zrok private | yes | NO | only with a share token the room already holds |

zrok is in the list shape (an order may name it, a room joined over zrok keeps it as a rung) but the hub does not
hand its share token to rooms. Giving a share token to an authenticated room from the hub is a later job.

## Decisions

1. **The hub listens on every transport it is started with, at once.** `atrium run --transport direct,ziti`. One
   value keeps today's behaviour. Each listener is served by the same `Hub`, and every connection is tagged with the
   transport whose listener accepted it. Rooms that are already attached are not affected by a second listener.
2. **The hub's order is a hub setting, `room_transport_order`, in `hub_setting`.** A comma list of kinds. Unset
   reads as `direct,ziti,zrok`, which is clint's default with zrok last. Set with `atrium rooms transports
   direct,ziti` on the hub machine, read at every attach, so a change needs no restart. It orders, it does not
   enable: a kind the hub is not listening on is skipped, and a kind it listens on that the order leaves out goes
   last.
3. **The hub tells each room its list in the welcome**, as `paths`: `{transport, addr}` for direct and
   `{transport, service}` for ziti. Never a credential. The room's ziti identity is the room's own.
4. **The room keeps its transport settings in `transports.json` in its key directory** (beside `room.json`,
   `room.crt` and `ca.crt`, default `~/.atrium/room/` on Windows `%USERPROFILE%\.atrium\room\`). A separate file
   from `room.json` because `atrium room join` rewrites `room.json`, and a re-join must not drop a ziti identity
   somebody configured by hand. Three parts:
   - `order`: the local override, a list of kinds. Empty means the hub's.
   - `paths`: what this room was told locally (`atrium room transports ziti --identity ...`). Wins over the hub's
     word for the same kind: what a person typed on the machine is the override, what the hub said is observed.
   - `hub`: the hub's list as last heard, in the hub's order, rewritten when it changes. This is how a room that is
     cut off from the hub still knows where the fallback is on its next start.
5. **The ladder is built from, in rising precedence: the join in `room.json`, the hub's list, the local paths.**
   Ordered by the local `order`, else the hub's order, else `direct,ziti,zrok`. A ziti rung with no identity is
   left out and said so in the log, since it cannot dial. A direct address is never laid over another: each one is
   its own rung, a local one first, then the joined one, then the hub's advertise. The first cut merged the advertise
   over the join, and on 2026-10-14 claude-sg4 sent every data dial to an address its hub had left while its control
   connection stayed up on 127.0.0.1 (`docs/changes/room-transport-order.md`).
6. **Every rung after a direct join runs mutual TLS inside the overlay** (`link.Proven`). The room's name comes from
   its hub-signed certificate on every rung, which the hub already requires: a name that enrolled a certificate is
   refused on the old certificate-less overlay path. A room that joined with a proven overlay string is wrapped the
   same way, which `room.json` now records as `proven`. Before this a proven overlay join was only wrapped while
   `join` ran, and not when `atrium room` started later from what was saved.
7. **Fallback trigger: a failed dial, never a refusal.** The control connection walks the ladder top down. A rung
   whose dial or TLS handshake fails (refused, unreachable, timed out after `DialWait`, ten seconds) moves the room
   to the next rung at once, in the same attempt. A hub that ANSWERS and says no (not on the list, marked for
   deletion, version) is the same answer on every transport, so it ends the attempt and the normal backoff applies.
   When every rung fails the room backs off as it always has (one to five seconds) and climbs from the top again.
8. **Every connection a room opens uses the rung the control connection is on.** Data, announce, relay, git and
   upgrade connections all go through the one dialer, so none can wander to another transport. The ladder stands on
   the rung itself, address and all, not on its kind, and a new list from the hub that leaves that rung out does not
   move it until the next climb.
9. **Coming back to a better transport.** Every reattach climbs from the top, so after any drop the best rung that
   answers wins. While attached below the top, the room probes every better rung every two minutes (a dial and a
   TLS handshake, closed straight away, nothing said to the hub). Two probes in a row that succeed on a better rung
   make the room close its control connection on purpose and reattach, which takes the better rung. Two in a row so
   a path that flaps does not move the room back and forth. Moving costs what a hub restart costs: the hub's pool
   for that room is dropped and the board reconnects. A probe that fails resets the count.
10. **The hub records which transport each attached room is on and whether that is a fallback.** `via` is the
    transport of the listener that accepted the control connection, the hub's own observation. `fallback` is the
    room's own word, in the hello, that it is not on its first rung. Both are in `/_hub/rooms`, `/_hub/inventory`
    and the `rooms` event. So is `starved`: the hub's own observation that the pool has been empty, with no data
    connection arriving, for 30 seconds (`Timings.Starve`). A control connection that beats is not a room the board
    can reach, and the rooms tab says "attached, unreachable" rather than "attached".
11. **The board shows it as the transport badge it already draws, never a column.** An attached room's badge names
    `via` rather than the transport it was added with. A room on a fallback gets the warning colour and a tip saying
    so: "on ziti because its first choice did not answer". The room's facts table says "connects over ziti
    (fallback)". The rooms dashboard tile draws the same badge.
12. **The room's own board says it too.** The link state the room's loopback board reads gains `transport` and
    `fallback`, so a room cut off from its hub on direct shows which rung it is trying.

## Settings at a glance

| where | what | how it is set |
| --- | --- | --- |
| hub flag | which transports the hub listens on | `atrium run --transport direct,ziti --atrium-identity <hub.json> --atrium-service atrium-hub` |
| hub store | the order rooms are told | `atrium rooms transports direct,ziti` (no argument prints it) |
| room `transports.json` | a local ziti identity and service | `atrium room transports ziti --identity <json or jwt> [--service atrium-hub]` |
| room `transports.json` | a local direct address | `atrium room transports direct --addr host:7779` |
| room `transports.json` | a local order | `atrium room transports order ziti,direct`, `--clear` to follow the hub |

`atrium room transports` with no argument prints the ladder the room would climb, in order, and where each rung came
from. Every room command takes `--dir` for a room whose key directory is not the default.

## Not built, named

- The hub handing a zrok share token to an authenticated room.
- Setting the hub's order from the board. It is a hub-machine CLI setting like `rooms legacy`, for the same reason:
  it changes how rooms reach the hub, and the board can be reached over a share.
- Moving a room between transports without dropping its pool. A move is a reattach.
