# Plan: messages that always arrive, and tests that are fast and never hang

Written 2026-10-09 against 70914fe5. Measured on the m1mini (macOS, 8 cores). No production code was changed.

## Summary (read this first)

1. **Goal 1, must be done: "delivered" means the runner acknowledged it.** Today it means "atrium wrote bytes" or "a
   hook
   answer was composed". Every path gets an explicit acknowledgement, a retry, and a board state that says what is true.
2. **Goal 1 phases in order:** M1 fix the typing model (the 2228 phantom is reproduced and has a cause), M2 the
   delivery ledger and the ack rule, M3 the hook and typed ack plus the Enter re-press, M4 wake, done and idle-target
   fixes, M5 launcher framing, M6 the fake-runner harness that keeps all of it from regressing.
3. **Goal 2, right after: whole suite under 3 minutes, every package under 60s, no hang.** Today the suite takes 173s on
   macOS and would take about 430s on Windows. Three packages are over 60s: daemon 173s, link 171s, gitsync 140s.
4. **Why it is slow:** zero `t.Parallel()` in 3,700 tests, and the daemon package spends 80% of its wall time waiting
   (33 CPU seconds in 166). Real sleeps, real `sleep 4` shells, and a 55ms schema migration per fresh database.
5. **Goal 2 phases by seconds saved per hour:** T1 process-shard the big packages (172s to about 64s, 2h), T2 template
   database everywhere (about 25s, 1.5h), T3 link timers as variables (about 40s, 2h), T4 gitsync parallel (about 100s,
   3h), T5 hang guard (3h), T6 the named flakes (6h), T7 headless board section deadline (2h), T8 daemon sleeps (8h).
6. **Two things I could not do:** the hub was down (127.0.0.1:7778 refused), so `atrium_backlog get` failed for all 14
   items. I worked from the brief's one-line summaries and from the code. And the Codeman "measure whether current
   Claude Code still ignores Enter" step needs a live Claude, so it is a spike in M3.
7. **Size:** Goal 1 about 11 working days including the harness. Goal 2 about 5 working days. Goal 2 phases T1 to T3
   can ship in one day and should go first because they make Goal 1's test loop faster.
8. **Open decisions for clint are at the bottom,** each with a recommendation. Eight of them.

---

# Part 1: Messages

## 1.1 What already landed (checked first, per item)

I read the last 300 commits and the ones behind these hits: 8cdd4076, 85703097, 8f204edb, b6def7a4, b01acd6d, 0cee67a7,
17db1f15, 51f0f47f, d4b0ad37.

| Item | State | Evidence and what is left |
| --- | --- | --- |
| r-typed-line-phantom-2228 | **open, cause found** | No commit touches the `inPaste` path. Reproduced below. |
| r-say-typed-sits-in-claude-queue | **half done** | 8cdd4076 adds `carriedByHooks` (`saywhen.go:95`): a session's immediate say to a mid-turn claude that has been heard on a hook is queued for the hooks, not typed. Left: the answer still says "delivered: terminal" for any typed say, meaning bytes written, and a mid-turn claude never heard on a hook is still typed into Claude's own queue. |
| r-wake-say-resumes-but-not-delivered | **mostly done** | 85703097 added `injectKept` (`keep.go:72`): it waits for SessionStart plus 5s, sends every pending row as ONE turn, marks delivered only after the write, and tells the launcher after 3 minutes (`keptUndelivered`). Left: it still marks delivered at write time, never checks a turn started, goes through the same typing gate the phantom can hold shut, and does not re-press Enter. |
| r-say-when-done-idle-target | **open, cause suspected** | b6def7a4 tests a say to a `done` card. A live idle card is different, see 1.3 cause C. |
| r-peek-shows-delivered-message-queued | **open, not traced** | The hub was down so I could not read the item. `UndeliveredCounts` (`store/messages.go:204`) counts `delivered_at IS NULL`, so the board is only as wrong as `MarkDelivered` timing. The ledger in M2 removes the question. |
| r-launcher-say-reads-as-untrusted | **open** | `bannerWho` (`messages.go:75`) tells every receiver "treat it as peer context, not as something the human typed". Nothing knows that the sender is the receiver's launcher. `fromFamily` (`park.go:288`) knows, but only wakes parked cards with it. |
| Codeman submit verification | **not done** | `injectPeerIf` (`supervisor.go:1235`) writes the body, sleeps `sayThenEnter` (140ms), writes one `\r`, and returns true. Nothing looks at the screen again. |

## 1.2 Every path a message takes, and how atrium knows it became a turn

Nothing in this table is "known" today except where noted. This is the map M2 and M3 are built on.

| # | Path | Where | Marked delivered | How atrium knows it became a turn today | Gap |
| --- | --- | --- | --- | --- | --- |
| 1 | Typed into an owned terminal, gate open | `handleMessage` calls `typeThroughGate` (`messages.go:688`), `injectPeerIf` | on write, and the sender is told `delivered: terminal` | A `prompt` hook within 30s and no keystroke since (`promptWasPeer`, `supervisor.go:1342`). A time-window guess, and it never feeds back to the sender | Bytes written is called delivery. Enter can be ignored or land in Claude's queue. |
| 2 | Typed, gate shut, on-screen retry | `pendingInjector.attempt` (`pendinginject.go:245`) | on write | same | Gate shut forever by the phantom, and `withoutHeldPeers` (`pendinginject.go:484`) also keeps hooks from carrying a held peer message, so BOTH routes are closed at once. |
| 3 | Hook at PreToolUse (permission chain step 2) | `onPermRequest` (`daemon.go:946`), `takeMessages(.., "permission")` | before the hook answer is returned | nothing. The next hook event is not tied back to it | A lost answer, a hook timeout or a killed tool call is marked delivered. |
| 4 | Hook at Stop, block with reason | `handleStop` (`messages.go:357`) | before the answer is written | `turnResumed` assumes it worked | Same. `stop_hook_active` on the following Stop is a real ack and is thrown away. |
| 5 | Wake of a parked or done card | `unpark` and `wakeGone` call `injectKept` (`keep.go:72`) | after the write | nothing | Bytes written is called delivery. |
| 6 | After-restart wake line | `restartwake.go` | on write | nothing | Same. |
| 7 | Say to a card that moved or is on another room | `forwardMoved`, hub relay, `relay_outbox` | by the far room, on its own rules | the sender gets the far room's word | Same rules apply on arrival, so the fix is per room and needs no hub change. |
| 8 | Unsupervised card (joined from its own terminal) | hooks only | at hook time | nothing | An idle one is unreachable without the Stop hook. `turnReachWarning` already says so. |

**The one rule.** A message row moves through `queued`, `sent`, `delivered` or `undelivered`. `sent` is what
atrium does (bytes written, or a hook answer returned). `delivered` is only set by an acknowledgement from the runner.
`undelivered` is a deadline passing without one. The sender, the board and `atrium_peers` read these four words and
nothing else.

## 1.3 Why messages get lost today

**A. The typing model can wedge shut (r-typed-line-phantom-2228). Reproduced.** `typedLine.feed` (`typedline.go:176`)
treats every byte inside a bracketed paste as text. Enter, control-c and control-u are text there too. The only way
out is the end marker `ESC [ 201 ~` arriving whole in one frame, and the parser says it assumes frames are never split.
I fed it a 2228 character paste with the end marker cut across two frames:

```
split end marker: count=2233 inPaste=true
after Enter:      count=2234 inPaste=true empty=false
after ctrl-c:     count=2234 inPaste=true
whole paste:      count=2228 inPaste=false, then Enter: count=0
end marker lost:  count=2229 inPaste=true   (Enter and ctrl-c are inert)
```

So `inPaste` stuck is a confirmed way to hold a gate shut for ever, with exactly this symptom: a count in the thousands
on a prompt that is visibly empty, and nothing the operator types clears it. I did not prove which real event splits
or drops the marker (a websocket re-frame, a reattach mid-paste, a paste box that sends the markers in two frames, or
Claude Code consuming a paste while the daemon restarted). The fix does not depend on knowing, see M1.
The same model has a second exposure: any submit that is not a lone `\r` frame in the board's own stream (a mobile
keyboard, the compose box, a runner that sends `ESC [ 13 u`) is read as `lost` or not at all.

**B. Delivery is declared at write time (paths 1 to 6).** There is no acknowledgement, so nothing can tell the
Enter that Claude ignored (Codeman saw 30 to 50 seconds of ignored Enter after first paint) or the line that landed in
Claude's own queue box from a turn.

**C. A lost Stop hook makes the card "mid-turn" for ever (suspected cause of r-say-when-done-idle-target).** `midTurn`
(`activity.go:556`) reads the last hook-posted activity with no staleness cutoff. Claude Code does not fire Stop on an
interrupt, so an interrupted turn leaves the card `thinking`. Then `when: done` waits for an end that never comes
(`turnHolds`), and an immediate say to a hook-heard claude is left for hooks (`carriedByHooks`) that an idle session
never fires. `looksidle.go` already notices this case but only draws a badge, it does not clear `midTurn`. Needs a
one-hour trace on a real interrupted card to confirm before M4 is built.

**D. The two routes block each other (path 2 plus `withoutHeldPeers`).** A peer message waiting for the typist is
withheld from the hooks. When the typist is wedged (cause A), nothing is left.

## 1.4 Phases

Sizes are working hours for one person who knows the daemon. "Tests" says what proves it.

### M1. Make the typing model unable to wedge (S, 5h, depends on nothing)

What changes:
- `typedLine.feed` carries a partial escape across frames, so a marker split between frames still parses.
- `inPaste` gets a watchdog: no byte for 2 seconds means the paste is over (a real paste is one frame). Enter and
  control-c are always honoured after the watchdog fires.
- A bare control-c, or control-u, always leaves paste mode. Cheap and removes the worst case.
- The model resets from runner signals. On a `prompt` hook (UserPromptSubmit) with no keystroke since it started, call
  `line.clear()`, because a submit certainly emptied the prompt. A Stop is NOT a reset, an operator can have a draft up.
- A readout reason that says "wedged for N seconds" and logs it once, so the next one is findable.

Why: it is the only fix that touches the cause. Resetting from the hook is the Codeman and backlog idea, and it is
safe only on `prompt`. Files: `internal/daemon/typedline.go`, `supervisor.go` (feed call, `noteOperatorTyped`),
`activity.go` (the `prompt` case at line 837 calls a new `run.resetLine()`). Tests: a property test that splits any
byte stream at every offset and requires the same final state as the unsplit stream (this would have caught 2228), plus
the five lines of the probe above as table cases.

### M2. The delivery ledger and the one rule (M, 14h, depends on nothing, ships with M1)

What changes:
- `message` gets `sent_at`, `acked_at`, `ack` (which signal), and `attempts`. `delivered_at` stays and becomes "acked".
  Migration is additive, so the 2ms template copy trick still holds. Old rows with `delivered_at` set are acked.
- `MarkDelivered` is split into `MarkSent` and `MarkAcked`. `takeMessages` and the injector call `MarkSent`. Only the
  ack handlers in M3 call `MarkAcked`. The say record (`store/say.go`) takes the same four states.
- `handleMessage` answers `delivered: terminal` only for a typed path that was acked inside a short wait (4s), else
  `sent` with a plain reason. The word `queued` stays for rows nothing has sent yet.
- The board chip and `atrium_peers` `waiting` count rows with `acked_at IS NULL`, and show `sent, waiting for the turn`
  versus `queued` versus `undelivered`. This is the fix for r-peek-shows-delivered-message-queued, because the chip is
  derived from rows and not from `pendingInjector` memory.
- `withoutHeldPeers` is deleted. Hooks carry a peer message whenever one fires. If a hook carries it, the typist drops
  it
  (that is what `deliveredElsewhere` does already), so the two routes cannot double-deliver, and cannot block each
other.

Why one rule: every item in the backlog list is a different way of "marked delivered, never read".
Files: `internal/store/messages.go`, `say.go`, `schema.go` (one migration), `internal/daemon/messages.go`,
`pendinginject.go`, `peers.go` (waiting count and `reachability`), `internal/link/control_mcp.go` (words in the tool
text), board JS for the chip. Tests: store unit tests for the transitions, daemon tests per path using the harness in
M6.

### M3. The acknowledgement per path, the retry, and the Enter re-press (L, 22h, depends on M2)

| Path | `sent` when | `delivered` (ack) when | If no ack |
| --- | --- | --- | --- |
| Hook at PreToolUse | the permission answer is written | the SAME session posts any later hook event (tool-start of the retry, tool-end, prompt or Stop). The tool result with the banner reached the model | row goes back to `queued` after 60s while the runner is alive, so the next hook carries it again. After 3 attempts `undelivered`. |
| Hook at Stop | the block answer is written | the next Stop arrives with `stop_hook_active` true, or any tool event | same |
| Typed, idle | Enter written | a `prompt` hook whose prompt text begins with the first 60 characters of the body (the hook payload has the prompt, the daemon drops it today). Fallback for a runner whose hook lacks it: the transcript's user line, which `replies.go` already reads (`queuedPromptOf`, line 694) | at 8s, if the screen composer still shows the head of the text, press Enter again (Codeman). At 20s, if it is in Claude's queue box (a `queued_command` attachment appeared and no turn started), leave it, mark `sent` and let hooks carry it. At 45s `undelivered`. |
| Typed mid-turn | n/a for a hook-heard claude (`carriedByHooks` already stops it). For others | the `queued_command` attachment in the transcript IS the ack: Claude read its queue into the turn | as above |
| Wake (`injectKept`) | the one-turn text is written after ready | the `prompt` hook, as for typed idle | Enter re-press, then `keptUndelivered` to the launcher as today |
| After-restart wake | line written | the `prompt` hook | same |
| Unsupervised, no Stop hook | hook answer written | same as hooks | stays `queued`, `turnReachWarning` already tells the sender |

The Codeman re-press sits beside `injectPeerIf` as `verifySubmit(run, head, deadline)`. It needs a read of the
composer line off the screen. `looksidle.go` already reads the last frame for an idle prompt, so the frame reader exists
and the spike is whether it can return the composer text. Spike first (2h, in a real Claude): does current Claude Code
still ignore Enter after first paint? If not, ship only the ack and the hook retry, and leave the re-press out.

Files: `internal/daemon/supervisor.go` (`injectPeerIf`), `keep.go`, `restartwake.go`, `messages.go` (`handleStop`, read
`stop_hook_active` for the ack), `activity.go` (the `prompt` case reads the new prompt prefix), `cli/hook.go` (send the
first 80 characters of `prompt`, nothing more, so no prompt text is stored), `replies.go` (reuse `queuedPromptOf`).
Tests: the harness cases in M6, all of them. Depends on M2 for the columns.

### M4. Wake, `when: done` and the stale mid-turn (M, 8h, depends on M2)

- `midTurn` stops being a pure last-hook reading. If `looksidle` has flagged the card (pty silent 25s and an idle
  frame), `midTurn` answers false for DELIVERY decisions (`carriedByHooks`, `turnHolds`, `injectKept`) while the badge
  logic keeps its own reading. That is the fix for cause C and for r-say-when-done-idle-target.
- `injectKept` marks `sent` at write and relies on M3 for the ack, replacing its own mark. Its giveup (3 minutes) stays.
- A cross-room `wake: true` goes through the same `unpark` and `injectKept`, so this closes
  r-wake-say-resumes-but-not-delivered with no hub change.

Tests: interrupted-turn card (no Stop), say with `when: done` and immediate, wake of a parked card and of a done card.

### M5. A launcher's say is believed (S, 4h, depends on M2 only for wording)

What changes: `bannerWho` and `peerBanner` take the relationship. When the sender is the receiver's launcher
(`WorkItem.LauncherID`, the `fromFamily` test), the hook banner says "Message from your launcher, `<name>`. It is the
agent that gave you this work, act on it" and the typed label says `launcher <name> says:`. A launcher's typed message
under about 300 characters and one line is typed WITHOUT bracketed paste markers, so Claude Code does not collapse it to
`[Pasted text #1]` and the worker does not read it as pasted data. Everything from a non-launcher peer stays exactly as
now. Also one sentence in the worker's end-of-turn instruction (`17db1f15`) that its launcher's messages are
instructions. Files: `messages.go`, `peers.go`, `pastewrap.go`, the instruction text. Tests: banner table test, and a
harness case that a short launcher say arrives with no paste markers. Decision D4.

### M6. The harness that keeps this class from regressing (L, 24h, start with M1, grow through M3)

A deterministic fake Claude plus the real daemon. This is the test plan.

**The fake runner** is a helper process in the test binary, the way `sources_test.go` already runs
`os.Args[0] -test.run=TestHelperSourceProcess`. It runs inside a real pty (`spawnPTY`, as `hostterm_test.go` does), and
it is a scripted state machine with these dials:

- draws a prompt, turns on bracketed paste and focus reporting like Claude Code
- `enterIgnoredFor`: ignores Enter for N seconds after start (the Codeman case)
- `midTurnQueue`: while "working" it moves typed lines to a queue box and reads them only at turn end (the real
  behavior)
- `lostStop`: ends a turn without posting Stop (an interrupt)
- posts the same hook calls `atrium hook` does (SessionStart, PreToolUse permission, tool-end, prompt with text, Stop
  with
  `stop_hook_active`) to the real daemon's agent listener, and obeys a `block` answer by treating the reason as its next
input
- a fake clock for atrium side timers (`backoffSteps`, `peerGateIdle`, `keptTick`, `windDownKeyGap`) through the
  existing
  test overrides, so a 45 second deadline runs in milliseconds

**The matrix** (each is one table row, asserted on the board state, the sender's answer, the row's `acked_at` and the
fake's own record of which text became a turn):

1. idle, gate open: typed, acked by prompt, sender hears `delivered`
2. idle, operator mid-line: held, then typed after the line clears
3. idle, wedged paste (split marker, lost marker): not held for more than the watchdog
4. mid-turn, hook heard: carried at PreToolUse, acked by the next hook event, exactly once
5. mid-turn, no tool call, turn ends: carried by Stop, acked by `stop_hook_active`
6. Enter ignored for 12s: re-pressed, one turn, no duplicate
7. typed mid-turn into the queue box: acked by `queued_command` only when read, never `delivered` before
8. hook answer lost (the fake drops the next event): row returns to `queued`, carried again
9. `when: done` to an idle card, and to a card with a lost Stop
10. wake of a parked card and of a done card, with and without a `wake: true` cross-room say
11. launcher say and peer say framing
12. daemon restart with `sent` rows: they are retried, not lost, not doubled
13. phone/compose-box submit forms (`ESC CR`, `\n`, CSI u) never leave the model wrong

**A recorded real pty** covers what the fake cannot: `testdata/scrollback` and `frame-turn-ending.bin` already exist,
and an opt-in script (`ATRIUM_REAL_CLAUDE=1`, never in CI) runs matrix rows 1, 4 and 6 against the installed Claude and
re-records the fixtures, so the fake is held to the real thing by hand once a release. Decision D3.

Where: `internal/daemon/fakeclaude_test.go` and `delivery_matrix_test.go`. Runs in the daemon package, so it also gets
the sharding in T1. Budget: the whole matrix under 20 seconds because the clocks are fake.

---

# Part 2: Fast tests

## 2.1 Measurements (m1mini, macOS, 8 cores, `go test -count=1 -json ./...`, whole suite 173s wall, exit 1)

Packages over 60s, and the ones near it:

| Package | Wall | Tests | Sum of test time | Median test | Note |
| --- | --- | --- | --- | --- | --- |
| internal/daemon | 172.7s | 1,622 | 170.9s | 10ms | CPU was 33s of 166s wall, so about 80% is waiting. 20 tests over 1s total 51s. 507 tests of 50 to 500ms total 89s. |
| internal/link | 171.4s | 700 | 170.4s | 10ms | 51 tests over 1s total 125s. |
| internal/gitsync | 140.0s | 231 | 138.9s | 430ms | every test spawns git. 51 over 1s total 78s. |
| internal/api | 42.8s | 300 | 42.0s | 60ms | |
| internal/store | 25.2s | 435 | 24.8s | 60ms | 88% of its CPU is `Store.Open` migrating. |
| internal/deployready | 21.7s | 34 | 20.7s | | |

Sum of package times is 760s. The wall is 173s only because the packages run side by side. On Windows at about 2.5x
that is about 430s, which matches the 550 to 600s reported for the daemon package alone there.
The 60s budget per package on Windows means about 25s on macOS.

The 25 slowest tests (all pass):

```
 12.0 link     TestAConnectionThatWaitedInThePoolStillWorks      link_test.go:281  time.Sleep(12s)
  8.1 daemon   TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated  hostterm_test.go:140  4s shell waits x2
  6.7 guard    TestGitGuard                                      gitguard_test.go:456
  6.3 link     TestAHeldAskIsPolledThroughAPause                 restartgate_held_test.go:33
  6.1 daemon   TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize   hostterm_test.go:194
  6.0 link     TestAnAnswerWithNoCardListIsNotAnEmptyRoom        announce_test.go:297  waits 3 x announceEvery
  6.0 link     TestARoomInTroubleDoesNotAnnounceItselfEmpty      announce_test.go:264  waits 3 x announceEvery
  5.0 link     TestAHungRoomDoesNotStallThePrune                 prune_test.go:106
  4.5 api      TestBoardScriptParses                             board_test.go:54    spawns node once per script file
  4.2 link     TestNothingIsSentWhenNothingChanged               announce_test.go:224  2 x announceEvery
  4.1 gitsync  TestAHubStoppedMidFetchLeavesEveryRefWhole        hub_test.go:481  time.Sleep(3s) at :508
  4.0 link     TestTheMoveIsRefusedWhileEitherRoomIsOffline
  4.0 link     TestAnAnnouncementThatFailedIsSentAgain           announce_test.go
  4.0 link     TestARoomDoesNotAnnounceAtAHubThatKeepsNothing    announce_test.go
  4.0 gitsync  TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch   receive_test.go:510
  3.9 daemon   TestRestartRunnerLeavesAStubbornRunnerUp          restart_session_test.go:47
  3.9 runnersetup TestConcurrentClaudeWritesKeepEveryChange      claudetrust_test.go:274
  3.6 ptyhost  TestIdleExit
  3.6 daemon   TestAStartWritesItsRunRowBeforeTheLaunchIsComplete
  3.5 link     TestAKeystrokeCrossesTheLinkWithoutDelay
  3.4 gitsync  TestPushToHubIgnoresAProxyTheClonesConfigSets
  3.3 daemon   TestARunnerAndAShellOnOneCardHaveTheirOwnRuns
  3.2 link     TestTheScopedStreamWithoutTheParameterIsUnchanged
  3.0 link     TestABuildThatDoesNotMatchItsHashIsNotInstalled
  3.0 link     TestPlacementDoesNotWaitForASlowRoom
```

Top causes, with where:

1. **No test uses `t.Parallel()`.** Zero of about 3,700. Inside a package every test runs one after another, so
   the machine sits at 20% CPU in the daemon package. Parallel inside a package is blocked by process-global test
   overrides (`windDownKeyGap`, `settleWindow`, `readUserSettings` in `daemon/main_test.go:40` and
`keepalive_args_test.go:21`)
   and by `t.Setenv`, which panics under `t.Parallel`. I tried `t.Parallel()` in `openTestStore`: the store package
   panicked in the first run. So the safe parallelism is across processes, not inside one.
2. **A 55ms migration per fresh database.** Measured: fresh `Open` 54.8ms, `Open` of a copied migrated file 2.1ms.
   435 store tests pay it (88% of that package's CPU), and every `startDaemon` pays it in `daemon.New` (12.6s of the
   daemon package's 33s CPU). The daemon's `testDaemon` already copies a template (`daemon/main_test.go:80`) and costs
   2ms. The store, api and link packages and `startDaemon` do not.
3. **Real timers in `link`.** `announceEvery` is a `const` of 2s (`link/announce.go:63`) and the tests sleep 2 to 3
   times
   it (`announce_test.go:244,281,312,443`). Plus `link_test.go:281` sleeps 12s, `everywhere_http_test.go:184` sleeps
   3.2s, `changerequest_test.go:1009,1028,1062,1100` sleep 300ms each, and 14 tests use `time.After(3..10s)` as the
   deadline of a poll. The daemon package does it right for its own timers (vars shortened in `main_test.go`), link does
not.
4. **Real shells that sleep.** `hostterm_test.go:140` runs a shell that waits 4s twice inside a pty. Two of the six
   slowest daemon tests.
5. **Subprocess per test.** gitsync runs `git init`, `fast-import` and friends in nearly each of 231 tests
   (`scm_test.go:20`, `forward_test.go:84`, `capped_test.go:15`), median 430ms. It is subprocess bound, not CPU
   bound, which makes it the best case for running tests in parallel.
6. **Polling in 50ms and 250ms steps.** 172 `time.Sleep` calls in daemon tests. `settlesTo` (`idletick_test.go:72`)
   sleeps 250ms to prove a negative, the cleanup loop in `testDaemon` (`sources_test.go:98`) polls at 50ms, and
   `releaseDB` (`daemon_test.go:109`) polls on removal.

Failures seen in this run (exit 1): `TestStartRunsADetachedHostFromACopy` and `TestHostCloseKillsItsRunners`
(ptyhost) fail with `bind: invalid argument`. The unix socket path under `$TMPDIR/<TestName><n>/001/` is longer than
macOS's 104 byte `sun_path`. That is the macOS red suite (t-macos-suite-red), and it is not flaky, it is deterministic
on any long test name. And `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` fails because the fork's arguments
carry the word `permissions` from text outside the test's control (the argument dump shows a worker instructions
block), so it depends on the environment it runs in. The two known failing tests (`TestReposIsOpenLike...`,
`TestRealSessionsKeepTheirText`) passed here.

**A sharding probe.** I split the 1,640 daemon tests four ways with `-test.run` and ran the four processes at once:
172.7s became 64s wall. Six extra tests failed in the shards, each after a 10s timeout
(`TestALaunchedClaudeStartsOnTheClassicRenderer`, `TestACardsLaunchEnvCarriesItsGitTokenScopedToTheForwarder`,
`TestALaunchedRunnerDoesNotInheritTheRoomsDebugSwitches`, `TestAHarnessGitConfigIsContinuedNotReplaced`,
`TestACardThatRunsOutsideCodeGetsNoGitTokenInItsEnv`, and `TestALaunchMakesTheItemAndAFailedStartEndsItOnce`, which is
the named t-launch-failed-start-flaky). I ran the compiled binary directly from `internal/daemon`, not through
`go test`, so either that run lost environment `go test` sets, or those tests share something across processes. T1
resolves which. It is the first thing to do.

## 2.2 Phases, ordered by seconds saved per hour of work

Wall seconds below are on this machine. Multiply by 2.5 for Windows.

| Order | Phase | Saves | Cost | Seconds per hour |
| --- | --- | --- | --- | --- |
| 1 | T1 process shards for daemon, link, gitsync | about 110s wall (daemon 173 to 64, link and gitsync alike) | 3h | 37 |
| 2 | T2 migrated template everywhere | about 25s of store, plus 10s of daemon | 1.5h | 23 |
| 3 | T3 link timers as variables | about 45s of link | 3h | 15 |
| 4 | T4 gitsync parallel | about 100s of gitsync if run alone | 3h | 30 on its own, overlaps T1 |
| 5 | T5 hang guard | prevents a 500s hang, saves 0 when green | 3h | n/a, required by the goal |
| 6 | T6 the named flakes and the macOS red | removes reruns | 6h | n/a |
| 7 | T7 headless board deadline | prevents a 500s hang | 2h | n/a |
| 8 | T8 daemon sleeps and shells | about 60s of daemon CPU-idle time | 8h | 7 |

T4 and T1 overlap, so do T1 first and T4 only for gitsync's own p-limit after measuring.

### T1. Shard the big packages across processes (3h)

`scripts/test-shard.sh` and the same logic in `scripts/ci.sh`: `go test -c`, `-test.list` to enumerate, split into N
round-robin shards (N = min(cores/2, 4)), run the shards at once each with its own `TMPDIR`, merge the `-json`. Windows
runs the same file through Git Bash, as `ci.sh` already does. No test is edited. First, find out why six tests failed in
my shards (above): print `env` and diff the `go test` and direct runs, and check for any shared fixed path or port.
Result target: daemon 64s, link and gitsync about 50s each on macOS. About 130s and 120s on Windows, which still
misses the 60s Windows budget, so T3, T4 and T8 are needed as well. Test: the whole suite with shards equals the whole
suite without, test for test, with a script that compares the two `-json` outputs.

### T2. One migrated template for every test database (1.5h)

Move `seedMigratedDB` (`daemon/main_test.go:80`) into `internal/store/storetest` as `storetest.Open(t)` and
`storetest.Copy(t, path)`, then use it in `openTestStore` (`store/model_test.go:11`), the 58 `Open` sites in store, the
5 in api, 14 in link, and `startDaemonWith`. A test of migrations themselves keeps the real `Open`. Measured: 55ms to
2ms. Expected: store 25s to about 3s, api about 10s less.

### T3. Make link's timers variables, as daemon already does (3h)

`announceEvery` to a `var`, set to 100ms in link's `TestMain`, and the same for the 12s idle-connection test, which
instead sets the transport's idle timeout to 200ms and sleeps 1s (or gets `-short` and runs in the nightly, decision
D6). Replace the 14 fixed `time.After(N)` polls with `waitFor` (already in `announce_test.go`) at the same bounds.
Replace the 300ms sleeps in `changerequest_test.go` with `waitFor`. Expected: link 171s of test time to about 60s.

### T4. `t.Parallel()` where it is safe, gitsync first (3h)

gitsync tests are subprocess bound and use `t.TempDir()`. A mechanical pass adds `t.Parallel()` to every gitsync test
that uses no `t.Setenv` and no package global, found by a small `go vet`-style check that lists tests touching
package-level vars. A `scripts/check-parallel.go` linter then keeps new tests honest. Not for the daemon package,
whose globals are the reason it cannot. Expected: gitsync 140s to about 30s on its own.

### T5. A hang guard that fails loudly and dumps goroutines (3h)

Two layers. (a) In every `TestMain`, `testdiag.Guard(m, 90*time.Second)` starts a watchdog that, if one test runs past
its budget, writes all goroutine stacks (the existing `testdiag.Dump`, `testdiag/testdiag.go`) and exits the process
with a named failure, instead of the 10 and 20 minute package timeout that hides which test hung. Budget is 30s per test
and 90s per package, with a `//atrium:slow 60s` marker for the few that need more. (b) `scripts/ci.sh` and the shard
script pass `-timeout 4m` per package (not 20m). Also t-gitsync-full-run-hangs: gitsync passed here in 140s, so the hang
is machine or load dependent. The guard will name the test the next time it happens. `exec.Command` in `gitsync/run.go`
and `backend.go` is not `CommandContext`, so one stuck git child can block a test until the package timeout, change both
to `CommandContext` with the request context. Test: a deliberately hung test under a build tag proves the guard fires.

### T6. The named flakes (6h, depends on T5 for the evidence)

- t-macos-suite-red: shorten the unix socket path. Name the folder `ptyhost-<hash>` under a short root
  (`os.MkdirTemp("", "ph")`) in `ptyhost` tests, and make `channel_unix.go:24` fall back to a hash only name when the
  path exceeds 100 bytes. 1h.
- `TestKeepaliveForkCarriesALeanCards...`: stop asserting that the word `permissions` is absent from the whole argument
  string and assert on the settings value instead (`keepalive_args_test.go:44`). 1h.
- t-launch-failed-start-flaky (`ledger_test.go:90`) and t-idle-park-workers-first-flaky (`idletick_test.go:188`): both
  rely
  on a wall clock window (`settles` polls 2s, `settlesTo` sleeps 250ms). Use the fake clock in T8, or loop until the
  state holds with a generous bound. Run each 200 times under `-cpu 1` and with 7 background `yes` processes to
reproduce
  first. 2h each.
- t-link-tests-flaky-under-load: T3 removes most of it. What remains is `time.After(3s)` deadlines on a loaded
  machine, which `waitFor` with a scaled bound fixes (`HEADLESS_SLOW` already exists for the board, add `TEST_SLOW`).

### T7. The headless board: a per-unit deadline that fails loudly (2h)

`unit()` in `scripts/test-board-headless.js:24457` runs each of 242 units with a bare `await fn()`. One unit that never
settles hangs the whole run, which is what stopped after `TIMING core/history`. Wrap it in `Promise.race` against a
timer (default 90s from the unit's weight in `board-suite-weights.json` times 3, `HEADLESS_UNIT_MAX` to override), and
on
expiry print `HUNG <unit>` with the page's pending requests and open dialogs, close the browser context, record the
failure and move on. Also a total ceiling (`HEADLESS_TOTAL_MAX`, default 15 minutes) that exits 3. Duplicate id
`t-compose`: `index.html:615` (the new-context paste box) and `index.html:691` (the old hidden div) share the id, which
`check-board.sh:352` is right to refuse. Rename the second, or delete it if `grep` shows nothing reads it. 30 minutes.
It
also makes the headless unit that touches `#t-compose` pick the right element.
I did not reproduce the `core/history` hang (it needs the full 500s run and I only had the budget for the Go side), so
the deadline plus its printed state is how the cause gets named the next time.

### T8. Daemon sleeps (8h, last, because T1 already moved the daemon package under 70s)

Replace the highest cost waits with events: `hostterm_test.go:140` and `:194` use a fake shell script that prints and
waits on a pipe the test controls, not `sleep 4`. The cleanup loop in `testDaemon` (`sources_test.go:98`) uses the
runner's `done` channel. `settlesTo` takes a clock. Remove the 100ms `time.Sleep` calls in 12 places and the 5ms
spins in 38. Target the 20 daemon tests over 1s first (51s total).

## 2.3 Budgets, enforced

`scripts/ci.sh` prints per-package wall time already. Add a gate: any package over 60s on a CI runner, or the suite over
180s, fails the job with the list of its five slowest tests, from the `-json` stream. Today's numbers go in
`scripts/test-budget.json`, and the number only goes down.

After T1 to T3 the expected macOS suite is about 60s with daemon the longest. After T8 the expected Windows
daemon package is about 90s, still above 60s. If that matters, the lever left is splitting the daemon package into
`daemon` and `daemon_slow` (a second package for the hostterm and launch tests). Decision D7.

---

# Order of work and dependencies

```
Day 1   T1, T2, T3          (fast loop for everything below, about 6.5h)
Day 2   M1                  (phantom fixed, small and reproduced)   T5 (hang guard)
Day 3-4 M2                  (ledger, one rule, board chip)
Day 5-7 M6 skeleton, M3     (fake runner, acks, Enter re-press, retry)   spike for the Enter question first
Day 8   M4, M5
Day 9   T4, T6, T7
Day 10+ T8, budgets
```

Each phase is one branch and one merge. M1 and T1 to T3 touch no shared code and can run in parallel by two workers.

# Decisions for clint

1. **D1. Is the ack rule the rule?** `delivered` only on a runner signal, with `sent` and `undelivered` as visible
   states. Recommend yes. It is the one change that closes every item in the list at once.
2. **D2. May hooks carry a peer message whenever a hook fires, removing `withoutHeldPeers`?** Recommend yes. It
   ends the case where a wedged typist blocks both routes. Cost: a hook delivery can arrive before the typed one would
   have, mid-turn, which is what `carriedByHooks` already chose.
3. **D3. Do we run a real Claude in the harness?** Recommend an opt-in script run by hand each release, never CI, plus
   the fake for CI. A real Claude in CI costs money and is not deterministic.
4. **D4. A launcher's short say without paste markers and with launcher wording?** Recommend yes. A worker doubting its
   own launcher costs a turn every time. The wording is the risk, so I would show you the banner text before merging.
5. **D5. Keep the Enter re-press only if the spike shows Claude still ignores Enter?** Recommend yes: measure first,
   2 hours. A re-press on a runner that did not ignore Enter risks a double turn, so it is guarded by the screen check.
6. **D6. The 12 second idle-connection test: shrink it with a shorter idle timeout, or move it to a nightly run
   behind `-short`?** Recommend shrink it. It guards a real bug and a 1 second version proves the same thing.
7. **D7. Is "every package under 60s on Windows" a hard bar?** After T1 to T8 the daemon package is about 90s there.
   Recommend: yes for the macOS 25s equivalent and for the suite total, and accept a split `daemon_slow` package
   (4h more) only if Windows CI time actually hurts, not before.
8. **D8. Process sharding in CI by default?** Recommend yes. It needs no test edits and is the biggest single win. The
   cost is a few log lines per shard and the need to resolve the six extra failures first, which is T1's first hour.

Not covered here: the hub relay itself (path 7 needs no change), the backlog items I could not read, and any change to
Claude Code's own queue behavior. `CLAUDE.md` files were not touched.
