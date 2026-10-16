# bug-room-ziti-goroutine-pileup: design note

## What creates the contexts

`ziti.NewContext` is called in three places only.

| Where | When | Closed |
| --- | --- | --- |
| `link.Ziti.context()` (`internal/link/ziti.go`) | once per `link.Ziti`, under `sync.Once` | never: nothing calls `Ziti.Close` |
| `Daemon.ZitiCapabilities` (`overlay_ziti_services.go`) | every `GET /v1/overlays/ziti/services` | `defer ctx.Close()`, but N concurrent calls build N |
| `startZitiNative` (`overlay_native.go`) | each start of the board overlay | never: not on a failed `Listen`, not when the share stops |

In the SDK (v1.2.8) `runRefreshes` and the posture `Cache` goroutine are per context (`firstAuthOnce`, `NewContext`),
so ~1500 of each means ~1500 contexts, not ~1500 refreshes of one. The router-channel groups (`txer`, latency probe,
`readLoop`) are per router connection. `Context.Close` closes `closeNotify`, which stops refreshes, the posture cache
and the router channels, so a closed context does not leak. An unclosed one does, forever.

## Why the landed fix did not stop it

`Room.redialLink` calls `Reset()` on `r.Dial` when it implements it. The room's `Dial` is a `*link.Ladder`, whose
rungs are `link.Proven{Dialer: *link.Ziti}`. None of `Ladder`, `Proven` or `Ziti` has a `Reset`, so the drop/redial
after N failures is a no-op for the old context: `Ziti.once` holds the same stale, half-authenticated context for the
life of the process, and a stuck `Dial` on it is never aborted. I found no site that builds a context per dial, so I
cannot show from the code which path made 1500 of them. What I can show is that every context made is held with no
way to close it, and that the SDK cannot be asked to bound itself. The fix therefore makes the count provable:
at most one live context per `link.Ziti`, closed on every path that drops it.

## The fix

1. `link.Ziti` swaps `sync.Once` for a mutex and a context that can be torn down. `Reset()` closes the live context
   and the next dial builds one. `Close()` is the same, idempotent. Closing aborts a dial stuck inside the old context,
   which frees its `maxDataDials` slot. A construction hook (`newCtx`) lets a test count contexts.
2. `link.Proven` and `link.Ladder` forward `Reset()` to the dialers beneath, so `redialLink` reaches the ziti rung.
3. `Ladder.Set` closes dialers of rungs the new list dropped (never the one it stands on, which is closed when the
   ladder moves off it). `learnHubPaths` needs no change: it goes through `Set`.
4. `startZitiNative` closes its context on a failed `Listen`, and closes it when the served listener closes.
5. `ZitiCapabilities` takes a mutex and answers from a 30 second cache, so a board that asks repeatedly or from two
   tabs builds one short-lived context, not one each.

Not done: the posture cache starting an `Evaluate` per tick while one is still in `GetAdaptersAddresses` is inside the
SDK (`ziti/edge/posture/cache.go`) and cannot be changed here. With one live context at a time the pile is bounded by
one cache, and a Reset after N failed dials discards it.

## Test

Unit, no disk or sockets, under 10ms. A fake context counts live instances, and each one runs a goroutine until
`Close`, as the SDK's refresh loop does. The test dials and fails, resets and redials 200 times through a `Ladder`
over `Proven`-shaped wrappers, and asserts live contexts never exceed 1 and `runtime.NumGoroutine` returns to its
baseline. Further tests: `Ladder.Set` closes a dropped rung's dialer and keeps the one it stands on; `Reset` while a
dial is stuck unblocks it.

## Changes after review

- **`link.Ziti` locking.** The mutex is held only to read or swap the context pointer, never across `ctx.Dial`. The
  "Reset while a dial is stuck" test uses a fake Dial that blocks until Close and must finish within a deadline.
- **learnHubPaths is the likely creator.** `OnPaths` fires on every attach, so a room redialling every minute runs
  `learnHubPaths` every minute, and when the hub's list cannot be saved it rebuilds every time. `roomRungs` reuses a
  dialer only where `reuse` matches by exact `==` on the address, while `Ladder` matches with the case-folded
  `samePath`. A rung that differs only by case gets a new `*link.Ziti` and so a new context. The fix: `reuse` uses
  `samePath`, and an unchanged rung keeps its existing dialer, so a refresh builds zero new contexts. Test: 100
  `roomRungs` + `Set` rounds over the same path list build 1 context.
- **Counted creation.** Every `ziti.NewContext` goes through one helper that logs its caller (`link`,
  `capabilities`, `native`) and the running live count. The live count is added to the room's health output.
- **ZitiCapabilities** caches only a success, for 30 seconds. A failed lookup is asked again on the next call.

## Test plan

## @LETTER@. A room with an unreachable ziti hub stays small

### @LETTER@1. Redial resets the context

1. Join a room over a ziti service, then take the hub's service offline.
2. Watch the room log for `[ziti] context built (link)` and `context closed (link)` lines.
3. Expected: the live count in each line never goes above 1, and the room's `ziti_contexts` stat stays at 0 or 1.
4. Bring the hub back. Expected: the room attaches on its next dial.

### @LETTER@2. Capabilities answers again after a failure

1. Open the overlay panel with the controller unreachable and press "what can I host?". Expected: the failure text.
2. Restore the controller and press it again. Expected: the services list, not the earlier failure.
