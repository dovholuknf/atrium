# Review: r-usage-no-stop 18417530

Range `1c2997c6..18417530`, one commit, room only. It touches `internal/daemon/usage.go`, the four call sites
(`finish.go`, `launch.go`, `session.go`, `supervisor.go`), a new `internal/daemon/usageflush_test.go`, the changelog
and `docs/test-plan.md`.

Until now a row in `session_usage` was written only from the Stop handler. A worker launched by `atrium_launch` that
ends without a Stop hook left no rows. This adds `usageTracker.flushed`: the same `record()` read, after the same
settle delay, run when a card reports, its session ends, it is terminated, or its runner exits.

Verdict: **OK** for the room. There is no Medium. There are five Lows and a letter fix. The letter fix is needed
before this lands.

## How it was checked

- **Reading.** I read the diff, and beside it:
  - `record()` and the cursor (`usageCursor`, `replySet.take` and `advance`);
  - `subagentsOf` and `readSubagentFiles`;
  - `endSegment` and `unspent`;
  - `LastTranscriptUsage`;
  - `BackfillUsage` and `backfillCard`;
  - each of the four call sites in its own function.
- **Gates**, run in a scratch worktree at the tip with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - `gofmt -l` lists only `daemon/fyi_test.go`, which is unchanged in this range;
  - `go vet` passes on daemon and store;
  - store passes;
  - daemon fails only on the known reds: the hostterm and unix-socket set, `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` and `TestNoTestHereCanReachALiveRoom`.
- **Race.** Their tests and my probes ran with `-race -count=3`, with no data race reported.
  - One probe writes 60 replies. After each one it fires three `flushed` and three `stopped` at the same time, then
    one last flush. Each run ends with exactly 60 replies and their exact output total.
  - Removing `readMu` from `record` fails that probe.
- **Restart probe.** A flush, then a fresh tracker on the same store (a daemon restart), then a flush and a Stop. Each
  reply is counted once.
- **Merge.** A trial merge onto `claude/landing` 3d7f1d25 is clean. It builds, vets, and the usage tests pass.
- **Mutants**, with backups in my own temp dir:

| Mutant | Result |
|---|---|
| finish call site removed | killed (`TestAReportRecordsTheWorkersUsage`) |
| Kill call site removed | killed (`TestATerminateRecordsTheWorkersUsage`) |
| session-end call site removed | killed (`TestASessionEndRecordsTheWorkersUsage`) |
| runner-exit call site removed | killed (`TestARunnerExitRecordsTheWorkersUsage`) |
| `flushed` ends the turn (`endSegment`) | killed (`TestFlushedLeavesTheTurnsCauseForTheStop`) |
| `readMu` dropped from `record` | killed (my race probe only, see L5) |
| file offset never advanced, alone | survives |
| `main.advance()` dropped, alone | killed (`TestUsageLeavesTheNextTurnForTheNextRow`) |
| both layers | killed (8 tests) |
| `flushed` skips the `isClaude` check | survives (L4) |
| `flushed` never clears the resume flag | survives (L3) |
| the flush's cut-off `stop` an hour in the future | survives (L1) |

## Points

1. **Concurrency. Sound.**
   - The whole of `record` runs under `readMu`: the cursor read, the file read, the row write and `advance`. So a
     flush and a Stop on one card are serialised, and the second read starts where the first stopped.
   - `cause` and `resumed` are under `mu`.
   - The race probe and `-race` agree.
2. **Kill and exit. Safe, and the same as a Stop.**
   - `recordUsage` reads the card when it is called and copies it. The goroutine never reads the card row again,
     except in `stamp`, which files under "" when the card is gone.
   - A transcript that is gone gives an open error, which is logged.
   - A goroutine still sleeping at shutdown can write after the store closes. That error is only logged.
     `stopped` already has the same shape, so this is not new.
   - Three flushes on one exit (Kill, the runner exit and the session end) cost two empty reads.
3. **A daemon restart. Idempotent.** The cursor is not persisted, but it is rebuilt from the session's last row
   (`LastTranscriptUsage`: its `Ended` and `LastMessage`). The times decide what was counted, not the offset, so a
   restart and then a flush counts nothing twice. My restart probe confirms it.
   - As before, a card with no row yet counts only from the daemon's start. So the deploy itself fills no old gap.
     The changelog says so.
4. **Subagent files.** They use the same cursor and are read under the same lock. `TestFlushedReadsSubagentFiles`
   covers a flush and then a Stop.
5. **"It does not end the turn." True.** `flushed` reads `cause` and `resumed` without clearing them, so the Stop
   that follows gets its cause. A test pins this, and the `endSegment` mutant fails it. A resume flag is cleared only
   once a row took it, but that clearing is untested (L3).
6. **Their question: a test for each guard layer.**
   - The reply times (`advance` and the `lastMsg` and `lastAt` check) are the real guard. Removing them alone fails
     `TestUsageLeavesTheNextTurnForTheNextRow`, which already exists.
   - The file offset is a speed-up. Removing it alone changes no count, by design: the times skip what was read.
   - So the layer that matters has its own test already, and the offset needs none. Say so in a comment, so nobody
     takes the offset for the guard.
7. **Backfill. It is span-based and does not double-count rows a flush wrote.** A reply inside a row's
   `Started..Ended`, or equal to the last row's message, is skipped. Two notes for the run the orchestrator plans,
   in L2.
8. **Gates.** As above. Only the known reds fail.
9. **The letter.** The section says **IS**, but stage 4 (`claude/f-git-url`) uses IS and stage 5
   (`claude/f-change-requests`) uses IT. Rename this one **IU**. IU is unused on landing and on both fabric branches.
   Once stage 4 lands, this section and IS will conflict at the end of `docs/test-plan.md`. Keep both.

## Lows

- **L1: a reply split across a flush keeps its first line's figures.**
  - Claude Code writes one reply as several lines with the same message id. `take` keeps the last line it reads. A
    later read skips every further line of that id (`lastMsg`).
  - My probe writes a line of `m1` with output 5, flushes, then writes a line of `m1` with output 500 and Stops. The
    record says 5.
  - A Stop comes after the turn, so it rarely meets this. A flush can, if it lands while a reply is being written.
  - The common cases are probably safe:
    - a report runs after the reply that called it is written in full;
    - an exit leaves the reply as it is.
  - A mid-turn flush could hold back the newest message id and leave it for the Stop or the exit flush. Or let a
    later line of `lastMsg` replace the counted figures. The flush's cut-off is also untested: moving it an hour
    ahead passes.
- **L2: two notes for the backfill.**
  - `atrium usage backfill` runs against the store while the room is live. A backfill row for a live card's newest
    replies is not seen by the tracker's in-memory cursor, so that card's next flush or Stop counts them again.
    Run the backfill with `--since` before the deploy time, or restart the room after it.
  - Backfill does not read subagent files, and the live read counts subagents only from the daemon's start. A
    worker's subagent spend before the deploy stays missing. Say so with the backfill numbers.
- **L3: clearing the resume flag after a flushed row is untested.** Remove it and nothing fails. Without it, the
  Stop after a flush flags its row `after_resume` as well, so two rows claim the resume. Add a case: launch on a
  resume, flush, Stop, and expect one flagged row.
- **L4: the quiet case at flush level does not cover a non-Claude runner.** Removing `isClaude` from `flushed`
  passes, because the test's transcript lookup finds nothing anyway. Add an opencode card with a transcript in place
  and expect no row.
- **L5: no test of their own fails without `readMu`.** The lock is older than this change, but a flush is a new
  second writer. Keep a short version of the race probe (flushes and Stops at once, then count) in
  `usageflush_test.go`.

A side note, outside this range: dropping `side.advance()` (the inline-subagent cursor) also passes the usage tests.

Atrium-Verdict: room-ok 1c2997c6..18417530
Quality: a small change that reuses the Stop's read instead of a second one. Each call site has its own test, and
the times-based cursor makes it safe to call as often as a card ends. The gaps are in the corners: a reply split
across a flush, the resume flag, and the backfill run that has to follow.
