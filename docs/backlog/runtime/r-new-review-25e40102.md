# r-new-review-25e40102. Review findings on the runtime batch 25e40102 (bug)

Status: 1, 2, 4 and 5 fixed on claude/r-review-25e4. 3 not fixed, see its answer. Owned by @runtime. Filed by @review 2026-09-30 after a correctness read of the batch landed on
claude/main as 25e40102 (r-029, r-032, r-034, r-035, r-041 to r-044, r-046). Read only: no tests were run for this
review, and @runtime's gate (build, vet, tests of daemon, store, api, runnersetup, cli and claudeconf) had passed.

Nothing here is HIGH, so nothing here blocks the room restart. The MED is reachable only with `auto_new_context` on,
which is off by default.

r-045 is not in the batch on purpose. Its backlog file says "not started": it was split out of r-034 by @rnd's review
and never built.

Checked and fine: the permission chain order in `onPermRequest` (only step 4 changed, and only the recorded `by`),
MCP rule matching (`MatchRule` tool globs, the tie going to block, `dropShadowedMCP` for a bare `mcp__*` deny), the
`ClaimResumeID` transaction under `SetMaxOpenConns(1)` (its `live` callback, `runnerIsLive`, never touches the store),
`WireNameHeld` returning its error instead of spinning, the hook posture (the permission hook finds the pid only when
it will post, and every new hook field is optional), and the halt (`AutoNewContextMode` reads a store error as off).

## 1. MED: a dismissed chip at the wrong moment panics the daemon

`internal/daemon/newcontext.go`, `runNewContext`, the `fail` closure for an automatic run:

```go
if !d.nctx.mine(taskID, gen) {
	return
}
cur := d.nctx.get(taskID)
...
reason, attempt, giveUp = d.autoFailing(taskID, gen, cur.step, step, reason)
```

`mine` and `get` take the lock separately. A board DELETE on `/v1/tasks/{id}/new-context` between them runs
`nctx.clear`, `get` returns nil, and `cur.step` dereferences nil. This runs on the run's own goroutine, which has no
recover, so the daemon process exits and takes every supervised runner with it (resilience guarantee 5).

The window is small, but it opens exactly when a person is most likely to press the chip's dismiss: a step has just
timed out on a wait they were watching.

Proven by reading, not by a test. Could `fail` take one `get` and check `cur != nil && cur.gen == gen` on it, instead
of `mine` then `get`?

Fixed as asked: one `get`, and the generation checked on the copy it returns.

## 2. LOW: the result of a finished automatic run can go unrecorded

`internal/daemon/newcontext.go` calls `d.nctx.finish` and only then `d.autoFinished`. A context tick between the two
reaches `settleAuto` with `cur == nil`, `s.finished == false` and no retry. When the conversation id has not moved
yet, that is the "dismissed from the board before it ended" branch. It re-arms the card with `attempts` at 0, then
`autoFinished` sets `finished` on the re-armed state, and the `done` event with `before` and `after` is never written.
The minimum gap still holds the card back, so nothing runs twice. Unproven. Should `autoFinished` run before
`finish`, or under the same claim?

Fixed: `autoFinished` marks the run finished before `finish` takes the chip off. If `finish` then finds the chip
already dismissed, the mark is taken back, so a dismissal still re-arms the card. The idle release moved out of
`autoFinished` and runs only when `finish` succeeds.

## 3. LOW: the keep-alive fork uses today's gateway, not the one the card launched with

`forkCardArgs` (keepalive.go) reads `st.LeanWorkerGateway()` at refresh time. A lean card launched before
`lean_worker_gateway` was set or changed gets a fork with a different server under `mercurius`, so its tool list, and
with it the cache key, differ from the card's own. The refresh then pays for a cache write and warms nothing, until
the card restarts. Unproven, and it depends on how Claude Code builds its tool prefix. Should the gateway a card
started with be recorded on the card (a tag, as `atrium:mcp:mercurius` already is), and the fork read that?

Not fixed. A restart of the card reads today's setting too (`launch.go`), so recording the gateway on the card
changes what a restart does, not only the fork. The cost is bounded: a cache write per refresh, only on a lean card
launched before the setting changed, and only until that card restarts. Reopen if the setting starts changing often.

## 4. LOW: the keep-alive card view reads the old session after a new context

`keepalive.tick` applies `followSession` before `decide`. `keepalive.view` calls `decide` on the raw card, whose
resume id names the old session until the next Stop. After a new context, `next_refresh_at` is worked out from the
old conversation's transcript. Unproven. Could `view` call `k.followSession(t)` first, as `tick` does?

Fixed as asked.

## 5. LOW: an automatic run drops an idle park's handoff mark

`autoIdleHold` replaces whatever `d.idle` held for the card. `autoReady` refuses a mark that is `capturing`, but not a
finished one. A card with a written idle handoff (`written: true`) that is then cycled and fails loses that mark in
`autoIdleRelease(taskID, false)`, which drops the entry. The next idle park asks for one more handoff. Unproven.
Could the hold keep the old mark and put it back on a failure?

Fixed as asked. `handoffMark.prev` holds a finished mark under the cycle's hold, and a failed cycle puts it back.

## Also seen, not filed

`watchAutoContext` reads `autoThreshold` (two setting reads) before it checks the mode, so every live card pays those
reads on every tick with the setting off. `contextSizeFor` adds a card read and a mode read per board fetch. Cheap in
SQLite, noted only in case the tick ever shortens.
