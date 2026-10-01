# Review of f-011 stages 0-1 plus f-011d 78289385..9f5dbac2 (@fabric: pty host behind `pty_host`, off by default)

Reviewed by @review, 2026-10-01, from `git diff 78289385..9f5dbac2` (12 commits) and a scratch worktree at
9f5dbac2. Room side. The read went deepest on what runs with the setting OFF, since that is what a deploy changes
for everybody, and then on the paths that turning it on and off again opens.

Tests at 9f5dbac2: `go vet` clean. ptyhost and store pass, ptyhost `-count=5` passes. The daemon package's focused
run (`-count=5 -run 'Host|RunExit|Exit|Supervis|Shell'`) passes. The full daemon run failed once, on
TestGlobalAutoSurvivesAReopen (the reopen gets SQLITE_BUSY). That test fails 2 in 60 at the base 78289385 as well as
at the tip, so it is an old flake and not this change.

## What holds with the setting off

- **The migration is safe.** `0077_pty_run` is last in the slice, `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF
  NOT EXISTS`, `CHECK` for the kind, no foreign key (and the comment says why). It is a room-store migration.
- **The seam is behaviour-preserving.** `localTerm.Wait` returns 0, the exit code, or -1 exactly as `awaitExit` did.
  `Kill` on a terminal with no process is a no-op, as the old `r.cmd.Process != nil` guard was. `startTerm` runs the
  same `beginPTYRaise`, `sizeAtLaunch`, `Command`, `Start`, `raise.apply` sequence that spawnPTYResume and
  spawnShell each had. Test runners with a fake `pty` get wrapped on first `tm()`.
- **Exit filing is idempotent on (task, run)** in one transaction (mark, `why`, `exited` event, ledger), and an empty
  run id files every time as before. A store error still carries on as filed, as it always did.
- **`removeRunner` and `removeShellIf` remove by identity**, so a late exit no longer removes a newer start's entry.
  That is a fix on the default path too.
- **`internal/detach` is the old `startDetached` moved**, flags and the access-denied retry identical on both
  platforms, so `restart_atrium` is unchanged.

## Findings

### 1. MEDIUM: turning `pty_host` off while the host holds runners starts a second copy of each on the next start

`Run` calls `reattachRuns` only `if d.st.PtyHostOn()`. Turn the setting on, let a card start in the host, turn it
off (the board's settings API takes `pty_host`, and off is the obvious rollback when something looks wrong), and
restart the room. The host is a detached process, so it and its runners are still up. The new daemon does not
reattach them, then `startFixtures` and `reopenSaved` start each card in-process. Two claude processes are then on
one conversation, and the host's copy is unreachable from the board.

Fix: reattach whenever a host answers at startup, whatever the setting says. `hostClient(true, false)` never starts
a host, so with no host it costs one failed dial. The setting then decides where NEW terminals go, which is all it
should decide. A test: record a run, keep a host holding it, turn the setting off, run the startup sequence, and
assert the card has one runner and it is the host's.

### 2. LOW: a Spawn that fails in transport can leave a host run behind the in-process fallback

`startHostTerm` falls back to a local start on any non-`RemoteError` from `Spawn`. If the host started the process
and the connection dropped before the reply, the host holds a run nobody knows the id of and the fallback starts
a second copy. The `Attach` failure path kills the run for exactly this reason, and the `Spawn` path cannot, as it
has no run id. Either list the host's runs for that task id and kill them before the fallback, or do not fall back
after a transport error mid-spawn.

### 3. LOW: `pty_run` is never pruned

One row per runner start and per shell start, forever. Small rows, but a room that cycles cards all day grows the
table without bound. Delete filed rows older than the event history keeps, in the existing sweep.

## Note, not a finding

A graceful stop still winds down every runner, including the host's (`stopSupervised` calls `windDown` on
`d.sup.all()`), so in this stage the host keeps runners alive only across a crash or a kill, not across `atrium
stop`. I read that as the stage boundary. If the design means stage 1 to survive a deploy, it does not yet.

## Verdict

HOLD 78289385..9f5dbac2 for finding 1. It only bites after someone turns the setting on, but off is the rollback,
and the rollback must not make it worse. Re-read 78289385..<tip>, then room-ok.

Quality: after the Sonnet switch, no drop seen. The cherry-pick resolution is right (migration renumbered to the end,
`ClearResumeID` kept), the default path is unchanged line for line, and the filing order (register live runs,
then file the exited ones, collect only after commit) is careful. The miss is the setting's off state, which is the
second-order case again.

## Re-read: 55eb5ec3..d682a7c4 (rebased, the first 12 unchanged by range-diff, fixes in 5422d397)

- Finding 1 closed. `Run` calls `reattachRuns` at every start, whatever the setting says. With no host that costs
  one failed dial, and the log line only appears when the setting is on. The test calls `reattachRuns` with the
  setting off, not `Run`. The change in `Run` is the removed `if`, so reading it is enough.
- Finding 2 closed. On a transport error from `Spawn`, `killStrays` ends the host's live runs for that card and kind,
  sparing the one this daemon holds. A transport error from `call` only comes after `c.done` is closed, so
  `hostClient` redials rather than handing back the dead client. A host that is still up then gets asked, and one
  that is gone has no stray to end. Tests cover the stray, the other kind spared, and the held run spared.
- Finding 3 closed. `PrunePtyRuns` deletes filed rows older than 7 days on the reaper tick and never an unfiled one,
  with a test for all three cases.
- Tests at d682a7c4: `go vet` clean. store and ptyhost pass. The daemon's focused run (`-count=3 -run
  'Host|Reattach|Stray|SettingOff|RunExit|Exit|Supervis|Shell'`) passes.

### Verdict

OK, room-ok 55eb5ec3..d682a7c4. No new findings.

Quality: after the Sonnet switch, no drop seen. Each fix goes at the cause, and the stray cleanup also handles the
held run, which is the second-order case that usually gets missed.
