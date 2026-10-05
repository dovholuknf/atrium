# r-new-move-card-between-rooms, M1 (room side): done

Branch claude/r-new-move-card-between-rooms. The hub (internal/link) is untouched. Nothing ran on a live room.

## M1 bullets

| Bullet | State |
| --- | --- |
| `InferRepo` reads `<repo>-worktrees/<name>` as `<repo>` | DONE, af29b609 |
| `moved_to`, `moved_from` columns, migration 0084 | DONE, 1be12e4c |
| Freeze, queue with ids, lease (store) | DONE, 1be12e4c |
| Freeze in the daemon: typing refused, says and notices held | DONE, bf93a08f. `holdingMessages` is true for a frozen card, `deferPeerInjection` skips it, board typing gets `in-refused` "input is refused while the card is moving". A frozen card's `sayGate` is OK, so a say queues and a parked frozen card is not woken |
| One lookup following `moved_to` (8 hops, cycle check) in `sayGate`, `currentLauncher`, `spawned_by` fallback | DONE, bf93a08f. `followMoved` in `internal/daemon/move.go`. Says forward through `sayAcross` and answer `forwarded: moved to B~<id>`. `launcherAfterMove` re-points a worker at a remote launcher |
| Old handle reserved while `moved_to` is set | DONE (store, 1be12e4c) |
| Freeze queue forwarded by id, dedup and ack | DONE: routes `queue`, `ack` on A and `deliver` on B (SeenMove dedup, ForgetMove on failure) |
| Launch accepts `moved_from`, `pin_order`, lineage, repeat-safe | DONE, launch commit. `LaunchRequest.MovedFrom`, `Pinned`, `PinOrder`. The repeat key is `moved_from` (a lock on `move:<id>` and `GetByMovedFrom`), NOT `task_id`, because `task_id` already means "start onto an existing card". Lineage was already taken (`SpawnedBy`, `SpawnedByID`) |
| Room routes | DONE, ef3446aa, `internal/daemon/moveroutes.go`, mounted by `api.Server.Move` at `/v1/move/` |
| Lease sweep (self-undo) | DONE, `sweepFreezes` in the reaper tick |
| Acceptance test | DONE, PASS: `TestAWorkerMovesBetweenRoomsThroughTheRoomRoutes` plus undo, lease and allowlist tests (`move_acceptance_test.go`, `move_test.go`) |
| Changelog | DONE |
| Cross-build windows and linux | see the end of this file |

Acceptance covers: a say in the freeze is queued and arrives once on the new card (deliver twice, one delivery), a say to the
old card id after the move arrives forwarded, a child with `report_to` the old handle is re-pointed to `B~<id>`, undo
before the move replays the queue and after the move is refused, pin slot and the `dovholuknf/atrium` label on the
successor. Not covered by a test: typing refused on the attach socket (the same `frozenForMove` is covered through
`holdingMessages`), and a live Claude capture (`capture` needs a real runner).

## The routes M2 calls (all POST JSON, idempotent by `move_id`, base `/v1/move/`)

Body fields: `id` (card, `room~id` accepted), `move_id`, `lease_seconds`, `room`, `wip`, `to`, `msg_id`, `msg_ids`, `from`, `text`, `wait_turn`, `alias`, `pinned`, `pin_order`, `gated`, `auto_approve`, `cwd`.

Room A (the old card):
- `freeze` (lease default 30 min), `renew`.
- `check`: `{ok, failures[], wip}`. Idle and clean. `wip:true` commits `WIP: move to <room>` and leaves `BRIEF.md` and `HANDOFF.*.md` out.
- `capture`: synchronous handoff. Answers `file`. A card with no runner answers `skipped`.
- `bundle`: `{conversation, cwd, jsonl, memory[], files[]}`, data base64, 64 MB cap (413 `transcript too large`).
- `park`: needs the freeze (409 otherwise). Runner leaves, card stays parked.
- `moved` (`to` is `room~id`): 409 after the lease or after the self-undo.
- `queue`: `{queue:[{id, text, from_peer, wait_turn}]}`. `ack` (`msg_ids`).
- `release`: 409 while the queue is not empty or `moved_to` is unset. Releases the alias with note `moved to B~id`, marks done, ends the freeze.
- `undo`: unpark, unfreeze, replay in order. 409 once `moved_to` is set.

Room B:
- `receive`: `{cwd, conversation, jsonl, memory[], files[]}`. `cwd` is B's own worktree, which must exist. Writes only through `safepath` with name allowlists.
- Launch is `/v1/launch` with `moved_from: "A~<old id>"`, `pinned`, `pin_order`, `repo` (`org/repo` as stored), `spawned_by`, `spawned_by_id`. Resume plus a prompt is refused by the runner args, so queue the successor's first paragraph through `deliver` or a say after the launch.
- `deliver`: `{id, msg_id, from, text}`. Answers `acked`, `duplicate:true` on a repeat.
- `adopt`: alias, pin, gated, auto_approve on the successor.
- End a successor with the existing `/v1/tasks/{id}/exit`.

## What M2 still has to do

- The hub must send `spawned_by`/`spawned_by_id` for the old launcher and set `report_to` on another room after launch (`LaunchRequest.ReportTo` is this room only).
- A re-pointed worker's remote launcher name is taken from the old card's alias or handle with the new room, so the hub should confirm that name exists on B (the adopt step gives the alias).
- `resolvePeer` preference and the move record are M2.
- The first paragraph for the successor and the "cut-over note" are the hub's to send.

## Cross-build and tests

`GOOS=windows` and `GOOS=linux` builds of `./...` and `go vet` of daemon, api and store pass. `go test ./internal/...` fails only
the known Mac ones (hostterm unix-socket path, testguard claude.cmd, TestTheWalkerLaunchSetAndClear, TestKeepaliveFork...).
