# Review of item 49, 451b48ef..7f523835 (@fabric: a card on every room, `atrium:everywhere`)

Reviewed by @review, 2026-09-30, from `git diff 451b48ef 7f523835` (m1mini/claude/49). Hub side and room side. The
review was parked once for board work and finished in a second context.

## What holds

- **Locks.** The index takes `everywhere.mu` alone, and the hub path takes `f.mu` then `e.mu` and never the other
  way. `all` calls the store's `Holding` with the lock released and drops a room the store no longer holds, so a
  room removed by the rooms CLI in another process leaves the index on the next read. `onChange` runs outside the
  lock. Stream sends do not block.
- **`/_hub/everywhere` needs no gate.** No guest listener serves `/_hub/`. It reads only the index.
- **A local card always wins.** The daemon falls through only when `localTargetVia` finds no exact handle, alias or
  card id. That lookup covers everything `resolvePeerSayWake` tries, so `tell` cannot capture a name that would
  have resolved here. The hub side falls through only after `matchCard` misses on the caller's own room. A
  `boardError` (the room not answering) is passed back as itself, so a quiet room is never mistaken for a miss.
- **Two matches are a 409 with nothing sent**, on both doors, naming each card as `handle@room (@alias)`.
- **One match goes through `sayAcross`** with the room written out, so holding, `unconfirmed` and the outbox are
  what they are for a typed `name@room`. A sender with no handle or no text is refused before the hub is asked.
- **Old peers.** A hub without `find` gives a note on the 404 and nothing is held. A hub that ignores
  `everywhere=1` answers untagged rows, and `onlyEverywhere` drops them. An old board's stream is unchanged.
- **`find` never matches a card id**, never the asking room, and never a `done` or `dead` card (`cardLive`).
- **Exit does not fall through.** `atrium_task` does, which is a read of a card that `room~id` could already
  reach.

## Tests

In a detached worktree at 7f523835, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet ./internal/link/ ./internal/cli/ ./internal/daemon/`: clean
- `go test -count=1 -timeout 25m ./internal/link/ ./internal/cli/`: ok (link 204s, cli 27s)
- `go test -count=1 -timeout 25m -run 'Everywhere|Relay|Say|Tell|Peers' ./internal/daemon/`: ok (16.5s)

@fabric reports the full daemon package passing at `-timeout 40m` (587.7s). I did not rerun the whole package.

## Findings

### Low

1. **Name capture.** Any live card tagged `atrium:everywhere` with a handle or alias answers a bare-name miss from
   every other room. `atrium_launch` takes `tags`, so an agent can launch a card that does this. A mistyped local
   name then reaches a card on another room rather than failing with the list of names that would have worked. The
   409 catches two such cards, but one is enough. @fabric agreed to index only cards a human launched (`spawned_by`
   empty). That change is not in 7f523835, and I review it when it lands.
2. **The peers rows leave out the alias the card answers to.** `asPeer` and the `RelayPeers` everywhere branch
   copy only handle, card, room and status. The test plan's own case is `to=orchestrator`, an alias, and
   `atrium_peers` does not show that word anywhere. The miss sentence (`spelled`) does show it. Copy `Alias`, and
   `Title` if the index keeps it.

### Nit

3. `handleTell` drops `in.Wake` on the fallthrough. `sayAcross` has no wake, which is also true for a typed
   `name@room`, so this matches existing behavior. The tool description could say that wake is local only.
4. No test covers a `done` card keeping its tag and dropping out of `find`. `indexed` handles it with `cardLive`.

HUB DEPLOY OK and ROOM DEPLOY OK 7f523835. Low 1 gets a re-read when the human-launched rule lands.
