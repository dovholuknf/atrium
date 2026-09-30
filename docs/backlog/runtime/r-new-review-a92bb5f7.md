# Review of a92bb5f7 (@runtime: test guard, new context mid-turn, escalation R1 and R3)

Reviewed by @review, 2026-09-30, from reading `git diff a92bb5f7^1 a92bb5f7`, Go before docs. All room side.

## What holds

- **Test guard** (`internal/testguard`, each package's `TestMain`). Every `ATRIUM_*` variable is removed and
  `ATRIUM_LOCATION` points at a file that does not exist. `HOME` and `USERPROFILE` go to an empty directory. An agent
  command becomes a shell that waits (`agentSpawn`, launch and resume) or fails at once (`agentFork`, keep-alive).
  A child of a guarded test keeps what its parent set on purpose (`ATRIUM_TESTGUARD`).
- **New context mid-turn** (`newcontext.go`). `ncType` types `newContextStop` after `nudgeAfter` on a running card,
  once more at half the limit, and never a third time. It goes through the operator's gate and gives way to a
  dialog, it is checked against the run's generation, and it is recorded as a prompt event. The step still waits for
  the turn to end, and a failure names the times the card was asked.
- **Escalation R3** (`a2a.go` `longTurn`, `activity.go`). One notice per turn, keyed on the turn's start. Lowest
  priority behind a silent stop and one long tool call. The call log is trimmed to 10 minutes and capped at 1000. It
  is in memory, and a restart starts a new turn.
- **Escalation R1 on the hook path** (`messages.go` `takeMessages`). An aged `when: done` message is carried by the
  next permission hook with the framing line, recorded on the card and on its say, and never ahead of a deploy wake
  (`heldAged` asks `awaitingWake`).
- **Settings** validate as whole positive minutes or empty, and read back the default on a bad value or a read error.

## Findings

### Medium

1. **R1 never reaches a supervised runner with mid-turn input off, and those are the workers.** For a peer message
   the typist holds, `takeMessages` first drops it with `withoutHeldPeers` (`messages.go:133-138`,
   `pendinginject.go:468`), so the hook route never sees it. The typist route escalates only when
   `midTurnInputFor` is true (`escalate.go` `escalatesByTyping`). With mid-turn input off, neither route carries the
   message before the turn ends. The design's acceptance test asks for exactly this case: "The same message to a
   runner without mid-turn input is delivered by the hook route only" (held-message-escalation-design.md, section 3).
   `TestARunnerWithoutMidTurnInputGetsAnAgedMessageByTheHookOnly` gets there only by calling `d.pending.stopAll()`
   first, and its comment says so. Production never does that.

   Exposure: on claude-sg4 today, `claude-worker` and `claude-fable` have `mid_turn_input: false` (GET
   `/v1/harnesses`, read-only). So a director's `when: done` message to a worker in a long turn is not escalated.
   Nothing is worse than before this change, so this does not hold the room deploy.

   Could we let `withoutHeldPeers` pass an aged `WaitTurn` entry when `midTurnInputFor` is false, and have the
   typist drop it once the hook has marked it delivered (`deliveredElsewhere` already does that part)?

### Low

1. **An escalated message typed by the typist carries no framing line.** The hook route puts `escalationLine` in
   front of the text. `attempt` types `e.body` as it was held (`pendinginject.go`, around the `escalated` flag), so
   a runner that takes mid-turn input gets the old message mid-turn with nothing saying why.
2. **`launch.go` hands `viaShellIfScript` the original `args`**, not the `cmdArgs` `agentSpawn` returned
   (`launch.go:1181-1183`). This is the identity in production, but under the guard the waiting shell receives the
   agent's arguments. `cmd.exe /d /k` is replaced by `cmd.exe <claude args>`. It still starts no agent, so the guard
   holds.
3. **The guard leaves `APPDATA` and `LOCALAPPDATA` alone.** `os.UserConfigDir`, `os.UserCacheDir` and anything under
   `%LOCALAPPDATA%\atrium` still resolve to the real directories on Windows. `ATRIUM_LOCATION` covers the address
   file, so nothing reached today depends on it.
4. **The new-context nudge is typed into a runner with mid-turn input off**, where a line typed mid-turn is lost,
   and the chip then says the card was asked. Skip the nudge, or word the chip differently, when `midTurnInputFor`
   is false.

## Tests

At a92bb5f7, in a detached worktree, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet` on daemon, api, cli, store, testguard, link: clean.
- `go test ./internal/store/ ./internal/api/ ./internal/cli/`: ok (165s, 115s, 43s). testguard has no tests.
- `go test -run Notify ./internal/link/`: ok.
- `go test -run 'Escalat|LongTurn|NewContext|Nudge|TestGuard|Owed|WhereAmI|Held|WhenDone|Keepalive|PendingInject|Message'
  ./internal/daemon/`: ok, 119 tests, including every new escalation, long-turn and new-context test.
- `go test -timeout 40m ./internal/daemon/` (whole package): FAIL on the timeout. At 40 minutes,
  `TestAShellDoesNotInheritSomebodyElsesCard` had been 33 minutes inside `startDaemon`'s `waitFor`
  (`daemon_test.go:73`, from `shell_test.go:223`). Run alone it passes 3 of 3 at a92bb5f7 and 3 of 3 at a92bb5f7^1,
  so it is a load flake in the full run and not this batch. `startDaemon`'s wait has no deadline, so a flake there
  costs the whole package timeout. @runtime's gate ran the whole package green.

## Verdict

**ROOM DEPLOY OK a92bb5f7.** Medium 1 is a follow-up, not a regression.
