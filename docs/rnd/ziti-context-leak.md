# The room's ziti context leak

Backlog item `bug-room-leaks-ziti-contexts`. Found with pprof on the two live rooms, 2026-10-17. Both run commit
`4b92117d`, which already holds the first fix (`5ad9a58b`, close and reset the context a `link.Ziti` owns). That fix is
sound and does not touch this leak, because the leaked contexts belong to `link.Ziti` values nothing ever closes.

## Verdict

**The room's ssh sync loop builds a brand new ladder, and so a brand new `link.Ziti`, and so a brand new ziti context,
for every hub call it makes. Nobody closes any of them.** `sshSyncLoop` runs once a minute for the life of the room, and
each context it builds authenticates, starts its own refresh, posture and HTTP goroutines, and lives until the process
dies. The SDK retry loops of 1,660 such contexts are the CPU.

- Leak site: `roomLinkFor` at `internal/cli/roomssh.go:157` (`hubSSH.fetch`) and `:199` (`hubSSH.publish`), which call
  `roomLinkFor` (`internal/cli/atrium_rooms_spec.go:137`), which calls `roomLadder` (`internal/cli/transport.go:373`),
  which calls `rungDialer` and `rawRoomDialer` (`transport.go:351`, `&link.Ziti{...}` at `:361`).
- It is returned as `climbing{l}` (`transport.go:435`), a type with `Dial` and `Describe` and **no `Close`**, so even a
  caller that wanted to release it has nothing to call.
- Caller: `go sshSyncLoop(ctx, keys.Dir)` at `internal/cli/roomrun.go:464`, introduced by `7c7870fc` on 2026-10-14. The
  first leaked context in both room logs is 20 seconds after the room starts, which is the loop's first timer.

## Evidence

### The log counts every context, and says who built it

`internal/zitictx` logs each build and close. Whole `room.log` of each room, up since 2026-10-16 20:59:

| room | built | closed | live (counter) | goroutines | contexts with a `runRefreshes` |
| --- | --- | --- | --- | --- | --- |
| sg3 (PID 10936) | 1,692 | 1 | 1,691 | 11,040 | 1,662 |
| m1mini (PID 79132) | 1,400 | 13 | 1,387 | 1,014 (dump at 14:07) | 174 |

Every line says `(link)`. None says `native` or `capabilities`, so the hub link's own `link.Ziti` is not the leaker.
One long-lived room needs exactly one `link` context per rung (one `link.Ziti` per rung, built once and reused).

### The leak is a metronome, one burst a minute

Contexts built per clock hour, both rooms (`grep 'context built' room.log | cut -c1-13 | uniq -c`):

```
        20  21  22  23  00  01  02  03  04  05  06  07  08  09  10  11  12  13  14
m1mini 162 162 162 163 102  50  51  50  51  50 101 136 120   8  27   1   .   .   .
sg3    162 159 162 159 105  50  50  50  51  50 101 157 153 153  70  17  29  11
```

At the start of the m1mini log, bursts of three two seconds apart, one minute apart:

```
20:59:48 context built (link), 2 live     <- 20s after start, sshSyncLoop's first timer
20:59:50 context built (link), 3 live
20:59:52 context built (link), 4 live
20:59:54 [ssh] step 6: publish this room's key and login to the hub the hub answered 400: ...
21:00:55 context built (link), 5 live
21:00:56 context built (link), 6 live
21:00:59 context built (link), 7 live
21:02:01 context built (link), 8 live
```

- **162 an hour is 2.7 a minute** while step 6 of the wizard keeps failing (the hub answers 400 to the publish, as in the
  `[ssh] step 6` line above, and the loop retries a minute later). One pass is then `pass`, `fixPublish` and the final
  `pass` (`internal/sshwizard/fix.go:73-89`): `hub.published` dials, `hub.publish` dials, `hub.published` dials again
  because publishing resets `h.got` (`roomssh.go:204-206`). Three dials, three ladders, three contexts.
- **50 an hour is 0.83 a minute** on the quiet hours: a pass that finds step 6 done is one `published` dial. Still one
  context a minute, forever.
- m1mini stops building at 12:00:42. That is not the leak healing: the loop is wedged (next section).

### The goroutine families (`goroutine?debug=2`)

sg3, 11,040 goroutines, 1,662 contexts that finished their first authentication. Families that scale with that number:

| family (top frame, and what created it) | sg3 | m1mini |
| --- | --- | --- |
| `ziti.(*ContextImpl).runRefreshes`, created by `onFullAuth.func1` (`sdk-golang@v1.2.8/ziti/ziti.go:1132`) | 1,662 | 173 |
| `posture.(*Cache).start.func1`, the 10s `Evaluate` ticker (`ziti/edge/posture/cache.go:265`) | 1,690 | 173 |
| `net/http.(*persistConn).readLoop` (the controller client each context owns) | 1,663 | 183 of 366 conns |
| `runRefreshes` parked in `refreshServices`, `Authenticate`, `RefreshApiSessionWithBackoff` (`ziti.go:716-745`, `:1001`, `:1110`) | 77 mid-retry | 21 and 5 |

Each context pins about six goroutines plus an HTTP connection pair and an edge router link. That is the 1,660
established TCP connections to the router, the 20k handles and the 1,683 threads in `sg3-cpu/REPORT.md`.

The goroutine ids in the m1mini dump reach 193,258,251. Two hundred million goroutines were created in 17 hours. That
is the retry storm, not 1,400 contexts.

### Why it pegs the CPU

The cost is per leaked context, so it grows with the leak:

1. `ziti.go:849-891` `runRefreshes`: when the api session refresh fails, `refreshAt = time.Now().Add(5 * time.Second)`.
   That is a fixed 5s with no backoff and no jitter, and every context has its own. With the controller now resetting
   and rate limiting, every one of 1,660 contexts fails and retries `POST /oidc/oauth/token` every 5s: about **330 token
   requests a second**. That is why 17,180 of the last 20,000 lines of m1mini's `room.log` are `oauth/token`.
2. `posture/cache.go:265-275`: every context evaluates posture every 10s, and `Refresh` runs `posture.Os()`
   (`os.go:38`, `host.PlatformInformation`), `MacAddresses()` and the domain func. On macOS `PlatformInformation`
   forks. 170 contexts is 17 forks a second, and in the m1mini dump **170 posture goroutines sit in
   `syscall.forkExec` > `acquireForkLock`** waiting on one another.
3. Those forks starve everything else that execs. The same dump has `sshSyncLoop` (goroutine 70) stuck in the same fork
   lock inside `sshwizard.checkSSHD` > `exec.Cmd.CombinedOutput`, and a `link.Proven.Dial` behind it. So the loop that
   leaks has been parked since 12:00 and the build rate fell to zero, with 174 contexts still running.

### Why nothing closes them

`link.Ziti` closes its context in `Reset`, `Close` and `dialFailed` (`internal/link/ziti.go:130,223,206`), so one `Ziti`
never has two. The leak is in what owns the `Ziti`:

- `roomLinkFor` returns `climbing{l}` (`transport.go:435`). `climbing` has no `Close`, and `hubSSH.fetch` and
  `hubSSH.publish` drop it when they return. `link.Ladder.Close` exists (`ladder.go`) and no caller reaches it.
- `localSSHMachine` (`roomssh.go:123`) makes a fresh `hubSSH` per call, and `sshSyncOnce` (`:255`) makes a fresh machine
  per pass, so there is no cache to hang a Close on either.
- The garbage collector cannot rescue the dropped value: a context that authenticated starts `runRefreshes`, the
  posture ticker and the HTTP client, and running goroutines are roots.
- Contexts whose first dial failed never authenticate and have no goroutines, so they are collected. That is why the
  counter says 1,387 and the dump holds 174 on m1mini: only the ones that reached the controller are pinned.

### Not the cause (checked)

- `link.Ziti` itself, `Ladder.Set`, `learnHubPaths`, `OnPaths`: "the hub's transports changed" appears 5 times in the
  whole m1mini log, and `Set` closes what it replaces.
- `ProbePaths`: it dials the room's one ladder, so it reuses the one `Ziti` per rung.
- The overlay code in `internal/daemon/overlay_native.go` and `overlay_ziti_services.go`: no `native` or `capabilities`
  line in either log.
- Other callers of `roomLinkFor`: `roomLogins` (`sshconfig.go:182`) is a CLI command that exits. The room's
  `GET /v1/ssh/wizard` (`roomssh.go:302`) has the same leak per request and is a second trigger, not the one observed.

## The trigger and the rate

The trigger is the room starting. The rate is one context a minute at best and 2.7 a minute while step 6 is failing, for
as long as the room lives: about 1,200 to 3,900 a day. After about half a day the pile is large enough that the
controller's rate limiter answers refreshes with a reset, which is what both rooms hit. The first fix (`5ad9a58b`) did
not change this because it only changes what happens to a context its own `Ziti` still holds.

## Failing test design

The invariant: N hub calls from one room leave at most the room's own contexts live.

1. In `internal/cli`, stand a fake ziti connection in place of the SDK. `link.Ziti` has a `newCtx` hook, but it is
   unexported, so add a small seam on the cli side (a package variable the rung builder consults, set only by the test)
   that returns a counting fake with `Dial`, `Listen` and `Close`.
2. Join the room to a ziti hub: `keys.SaveOverlayRoom` with a ziti service, an identity path and `Proven`.
3. Run `sshSyncOnce(ctx, dir)` ten times against a fake hub that answers `FetchSSHSet` and `PublishSSH` on the
   `net.Conn` the fake returns. Answer 400 on publish to reproduce the three-dial pass.
4. Assert `built - closed <= 1` after every pass. Today `built == 3N` and `closed == 0`, so it fails on the first pass.
5. A second case drives `sshWizardHandler`, ten GETs, same assertion.
6. A guard test on `zitictx`: build more than a small cap (say 16) without closing and expect `New` to refuse with a
   named error, so a future leak is loud in a test and in the log instead of at 1,600.

## The fix (smallest correct change)

Stop building a ladder per call. The room already holds the right dialer: `dial` at `internal/cli/roomrun.go:322` is the
ladder the room climbs, with one `link.Ziti` per rung and one context.

1. `internal/cli/roomrun.go:464`: `go sshSyncLoop(ctx, keys.Dir)` becomes `go sshSyncLoop(ctx, keys.Dir, dial)`.
2. `internal/cli/roomssh.go:123`, `:255`, `:262`, `:302`: `localSSHMachine`, `sshSyncOnce`, `sshSyncLoop` and
   `sshWizardHandler` take a `link.Dialer` (nil means build one as today, for the CLI commands) and hand it to `hubSSH`.
3. `internal/cli/roomssh.go:157` and `:199`: `hubSSH` uses its given dialer and only calls `roomLinkFor` when it has
   none. Where it did build one, release it before returning.
4. `internal/cli/transport.go:435`: give `climbing` a `Close() { c.l.Close() }` and release it in the other
   `roomLinkFor` callers (`atrium_rooms_spec.go:156,173`, `sshconfig.go:182`). They are one-shot commands today, so
   this is hygiene, but it makes the type honest and stops the next in-process caller copying the leak.

Do NOT call `Ladder.Close` on the room's shared ladder from the ssh code. It would close the hub link's context under
the room.

Defence in depth, separable:

- `internal/zitictx/zitictx.go:36`: refuse past a cap (say 16 live) with an error and a log line, so a repeat costs one
  feature instead of the machine.
- The fixed 5s token retry and the 10s posture fork are the SDK's (`sdk-golang v1.2.8`). They are what turns a leak
  into a CPU fire, and not something to fix here.

## What was not done

- No heap profile. `heap` on m1mini produced nothing within 60s (the room is at 200% CPU) and the goroutine dump
  already names the creators, so it was not worth more load on a struggling room. The raw dumps are under
  `D:\tmp\ctrl-hunt\ziti-leak\` (`m1_g1.txt`, `sg3_g1.txt` once copied), not in git.
- A second goroutine dump 5 minutes later. The build log gives the growth per hour for 17 hours, which is stronger.
- Nothing was restarted, killed or changed on any machine.
