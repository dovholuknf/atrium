# r-new-move-card-between-rooms, M1 (room side): incomplete

Branch claude/r-new-move-card-between-rooms, from claude/main bcdbb267. Two commits, both with tests, store package green.
The hub (internal/link) is untouched. Nothing was run on a live room.

## M1 bullets

| Bullet | State |
| --- | --- |
| `InferRepo` reads `<repo>-worktrees/<name>` as `<repo>` | DONE, af29b609 (`internal/store/store.go`, `inferrepo_test.go`) |
| `moved_to` and `moved_from` columns | DONE, 1be12e4c. Migration `0084_room_move` at the end of schema.go. On `Task` as `MovedTo`, `MovedFrom`, in `taskColumns`, `scanTask` and the insert |
| Freeze with a queue of ids and a lease (store side) | DONE, 1be12e4c, `internal/store/move.go`, `move_test.go` |
| Freeze wired into the daemon (typing refused, says and notices held) | NOT DONE, see below |
| One lookup that follows `moved_to` (8 hops, cycle check) in `sayGate`, `currentLauncher`, the `spawned_by` fallback | NOT DONE |
| Old handle reserved while `moved_to` is set | DONE in the store: `Register` refuses a card with `moved_to` (returns `ErrStaleName`), and `wire_name` is unique and kept. Test `TestAMovedCardKeepsItsHandle` |
| Forward the freeze queue by id, dedup and ack on the receiver | Store half DONE (`FreezeQueue`, `AckFrozen`, `SeenMove`, `ForgetMove`). Routes NOT DONE |
| Launch accepts `moved_from`, `pin_order`, lineage, repeat-safe `task_id` | NOT DONE. Repo, org, host, branch and `spawned_by`/`spawned_by_id` are already accepted by `LaunchRequest`. Missing: `MovedFrom`, `Pinned`/`PinOrder` |
| Room routes for the pieces of section 3 | NOT DONE |
| Acceptance test | NOT DONE |
| Changelog `changelog/runtime/2026-10-04-r-new-move-card-between-rooms.md` | NOT DONE |
| Cross-build windows and linux | NOT DONE (host build and `go build ./...` pass) |

## What the store gives the daemon

- `Freeze(id, moveID, lease)` idempotent by move id. `RenewFreeze`. `Frozen(id)`.
- Every message queued for a frozen card goes to `freeze_queue` instead of `message`, at the one choke point `queueMessage` (so says, reports, notices, nags and wakes) and the ledger notice insert. Each keeps its id.
- `Unfreeze(id, moveID, replay)`: replay puts the queue back in order (the undo). Replay is refused with `ErrMoved` once `moved_to` is set.
- `SetMovedTo(id, moveID, to)` refused with `ErrLeaseExpired` after the lease. `ExpireFreeze(id, at)` (the self-undo) refused once `moved_to` is set. Both in one transaction each, so they cannot both happen. `ExpiredFreezes(at)` lists cards to sweep.
- `SetMovedFrom`, `MovedHandleHeld`, `SeenMove`, `ForgetMove`, `AckFrozen`.
- Refusals are returned through `moveTx`, not as errors out of `inTx`, since an error there halts the store.

## Where to pick up (daemon side), in order

1. `holdingMessages` (`internal/daemon/newcontext.go:465`) should also answer true when `st.Frozen(id) != nil`. That holds typing, `takeMessages`, `tellByTyping` and the pending injector at once. Also guard `deferPeerInjection` (the diverted message is not in `message`). Refuse board typing in `attach.go:360` the same way as `typingHeld`, with `in-refused` "input is refused while the card is moving". `sayGate` stays OK for a frozen card (so the say queues, and a frozen parked card is not woken), and the say answer should say it is held for a move.
2. Add `d.followMoved(t)` (`internal/daemon/move.go`): up to `store.MaxMoveHops`, a seen set, `move chain loops at <id>`. A `moved_to` of `room~id` whose room is this room continues locally, any other room is the answer. Add `sayMoved` to `sayGate` (checked first). In both say paths (`peers.go` resolvePeerSayWake near line 301, `messages.go` near line 572) forward with `sayAcross(ctx, from, <new id>, room, ...)` (a card id resolves on the other room through `localTargetVia`) and answer `forwarded: moved to B~<id>`. In `launcherOf` (`a2a.go:243`) follow a launcher that has `moved_to`: local live card is returned, a remote one re-points the worker with `SetLauncher(name@room, room~id)` and returns nil so `notifyRemoteLauncher` is used.
3. Launch: add `MovedFrom`, `Pinned`, `PinOrder` to `LaunchRequest`. In `Launch()` take a lock on `move:<MovedFrom>` and return the existing card if one has that `moved_from` (needs a store `GetByMovedFrom`), else launch and `SetMovedFrom`. Add store `SetPinSlot(id, order)` writing `pinned=1, pin_order, rank` as `SetPinOrder` does. Resume plus prompt is refused by `runnerArgsWith`, so the successor's first paragraph is queued with `QueueMessage` after launch, not sent as a prompt.
4. Routes, mounted by one `api.Server` field `Move http.Handler` at `/v1/move/` (the daemon sub-mux does the rest), all idempotent by move id:
   A: `freeze`, `renew`, `check` (idle, clean via `git status --porcelain`, `wip` commits `WIP: move to <room>`), `capture` (synchronous: `nctx.beginCaptureOnly` + `ncCapture`, as `startIdleHandoff` does in `idletick.go:233`), `bundle` (transcript of `conversationOf(t)` under `~/.claude/projects/<encoded cwd>/`, `memory/`, and `BRIEF.md`, `HANDOFF.*.md`; export `projectDirFor` from `internal/api/sessions.go`), `park` (as `idlePark`: `idleLeave`, wait, `parkCard`), `moved` (`SetMovedTo`), `queue`, `ack`, `release` (alias_note "moved to B~id", release alias, mark done, `Unfreeze(..., false)` only when the queue is empty), `undo` (`unpark`, then `Unfreeze(..., true)`).
   B: `receive` (write the bundle only through `internal/safepath` and a name allowlist: the jsonl under B's own encoded cwd, the memory folder beside it, `BRIEF.md` and `HANDOFF.*.md` into the cwd), `deliver` (dedup with `SeenMove`, deliver through `handleMessage` as `handleSay` does, `ForgetMove` on failure, answer `acked`), `adopt` (alias, pin, gate, auto-approve).
   Ending a successor can use the existing `/v1/tasks/{id}/exit`.
5. A reaper tick calls `ExpiredFreezes` then `ExpireFreeze`, and `unpark`s a card that undid itself.
6. Acceptance test on two in-process daemons with a fake `Relay` (see existing cross-room say tests) as in the M1 acceptance paragraph. Then the changelog, gofmt, and linux and windows cross-builds into `build.claude/`.

## What M2 needs from these routes

The route names and bodies in step 4 above are the contract. Not built yet, so M2 cannot start. M2 also needs the old card's lineage as `name@room` and a `report_to` on another room, which `LaunchRequest.ReportTo` does not take (this room only): the hub has to send `spawned_by`/`spawned_by_id` for the old launcher and set `report_to` after launch.
