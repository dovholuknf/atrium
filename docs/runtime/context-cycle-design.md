# Context cycle, design

Status: built. The limit is `api.ContextLimitFor` (`internal/api/contextsize.go`), the cycle is
`internal/daemon/newcontext.go`.

Built from clint's interview of 2026-10-05, kept below as the appendix. The plan's numbered items are his and are not
argued with here. This says where each one lands in the code, what goes, and what moves.

## The one trigger, and where it is checked

A card cycles when its context passes its limit. Nothing else starts an automatic cycle (plan 1, 2).

- **The limit** is resolved by `api.ContextLimitFor(st, t)`, which returns the size in thousands and where it came
  from:
  1. the card's own override `context_limit_k` (already exists, set in the details dialog now, plan 10),
  2. the room setting `context_limits`, a JSON object keyed by harness id, which the hub owns and hands to every
     room (below). A card whose runner id is not in it and whose harness is claude-shaped (`isClaude`) takes the
     `claude` entry,
  3. the built-in default, `{"claude": 200}`.
  A harness with no entry and no claude fallback has no limit and never cycles. Sources are `card`, `hub` and
  `default`.
- **The size** is the transcript figure `contextSizes.read` already keeps (what the board shows). It is read again,
  stat-gated and cheap, on each check.
- **The check** is `Daemon.cycleCheck(t, tokens)` in `internal/daemon/autocontext.go` (rewritten, a fraction of
  its size). It runs from the reaper's `watchContext` tick and from `onTelemetry`, so every statusline update
  re-checks the card (plan 7). It starts a cycle when all of these hold:
  - atrium supervises the card (`d.sup.get(id) != nil`), it is not a fixture, not a lent (guest) session, not
    parked and not frozen for a move. Fixtures and guests stay out: a fixture is not work, and a guest is someone
    else's session. **Nothing else excludes a card**: no `atrium:subagent` rule, no human/agent split, no tag (plan 9),
  - the card's override `context_cycle` is not `off` (the per-card switch, plan 9),
  - the limit is above zero and the size is at or past it,
  - no chip is on the card. A failed chip blocks it until dismissed, rerun or proven cleared, so a broken cycle is
    not restarted every fifteen seconds. The one exception is a chip a restart left on the limit step, which has
    nothing cleared and is not left at all (journal, below).

## The sequence (`internal/daemon/newcontext.go`)

The manual action (ctrl-alt-n, the menu, `POST /v1/tasks/{id}/new-context`) and the automatic one run **the same
sequence** (plan 2: "exactly as if clint pressed ctrl-alt-n"). The old capture with its token, its 200-byte floor
and its file in the cwd goes. Steps, as the chip shows them:

1. **`limit`**: atrium types the limit prompt
   `you are at context limit. wrap what is in flight, write your handoff to <path>, then run <atrium> ready.`
   (plan 3, wording from "Details"). It is typed **as soon as the input line is free, mid-turn included** (plan 5):
   the gate check is the one an immediate say uses (`typeLabelledGuarded`, no dialog on screen, the operator not
   typing), with no turn check. A runner that does not take input mid-turn (`midTurnInputFor` false) waits for the
   turn to end, since mid-turn the line would be lost.

   **Retry (plan 7)**: while no ack has come, the prompt is due again when the turn that carried it has ended:
   - typed mid-turn in turn N: due once turn N is over (the card is between turns, `turnsBegun` still N), or once a
     later turn has begun that has not had it,
   - typed between turns: it starts turn N+1, and is due again when that turn is over, or a later one begins.
   So it is typed **at most once per turn**, however many statusline updates arrive inside one. A floor of
   `ncTiming.promptGap` (30 s) between two prompts covers a runner whose turns are not counted. Statusline updates
   and the tick poke the run (`kick`), so a due prompt goes at once rather than on the next poll.

   There is **no timeout** on this step. It waits for the ack for as long as it takes, and never clears without
   one (plan 7). An automatic cycle whose card drops back under its limit before the ack (the runner compacted)
   ends quietly: the chip goes, the held messages are released, and an event says why.
2. **`clear`**: after `atrium ready`, `/clear` is typed between turns (`ncType`, which waits for the turn to end:
   the agent ran `ready` inside a turn, and a `/clear` typed mid-turn is not a slash command). Then the new session's
   SessionStart is waited for, as today.
3. **`wake`**: once the new session has settled, `read <path> and continue.` (plan "Details"). CLAUDE.md reloads on
   its own after `/clear`, so the wake carries nothing else.

A failed `clear` or `wake` leaves the failed chip with its reason, as today. Running the action again on a chip that
failed after `/clear` was typed resumes at the wake alone, so a cleared session is never asked to write a new
handoff over the one it is about to need.

**Held messages** (`holdingMessages`) are unchanged: from the claim until the wake is typed, nothing is typed into
the card and no hook carries a message (brief: keep it). The cycle's own typing does not ask.

## The ack, `atrium ready` (plan 6, 8)

- `internal/cli/ready.go`: a command with `atrium finish`'s posture. Which session from `$ATRIUM_AGENT_NAME`, the
  cwd's name, or `--name`; `$ATRIUM_TASK_ID` beside it; `--url` or `$ATRIUM_HUB_URL`. Not silent: it prints what
  happened and exits non-zero on a refusal, because the agent must know the ack did not land.
- `POST /ready` on the agent listener (`internal/daemon/ready.go`), beside `/finish`. It:
  1. finds the card (`taskFor`, as `/finish`), and refuses with 409 when no cycle is waiting on its limit step,
  2. reads the handoff file. Missing or empty is refused with 409 naming the path: an ack with nothing to read back
     is not an ack, and the cycle keeps waiting (plan 7: never clear without one),
  3. **stores a copy on the card**: a `notified` event `{by: "context-cycle", handoff: <text>, path, bytes}`, capped
     at 256 KB with a note when cut. The board's history draws it as a `handoff` row whose text opens in full, so it
     can be read after `$TEMP` is cleaned,
  4. marks the run acked and kicks it, and answers `{ok, task_id, path, stored}` with the line the CLI prints:
     "atrium clears your context when this turn ends. End your turn now."

## The handoff path (plan 4, 8)

`handoffPath(t)` = `<dir>/<card-id>.md`, where `<dir>` is the room setting `context_handoff_dir` when set, else
`os.TempDir()/atrium/handoffs` (`$TMPDIR` on macOS, `%TEMP%` on Windows, `/tmp` on Linux). The daemon makes the
directory before typing the prompt. The prompt names the absolute path and the absolute path of the atrium binary
(`os.Executable()`), because the CLI is not on PATH in every room. The path is fixed when the cycle is claimed, so
a setting changed mid-cycle cannot split the prompt, the ack and the wake.

The idle parking and the room move keep their own capture (`ncCapture`, `HandoffName` in the cwd). They are not a
context cycle and are not touched.

## What is deleted

- `internal/daemon/contextnudge.go` and its test: the line on tool calls (plan 1). `daemon.go`'s permission path
  keeps its order (queued messages, shelved, deploy hold, rules, …) with the context line simply gone.
- The launcher's context notice (`watchContext`, `NoticeContext`) and every auto-new-context launcher notice
  (started, waiting, gave up, still large).
- In `autocontext.go`: the modes, the arm state machine, the start grace, the minimum gap, the retry/give-up logic,
  the human quiet, the ceiling (`ceilingHeld`, `ceilingCrossing`, the wait note), the `atrium:subagent` exclusion,
  the 70%-of-window line, and the idle-clock hold. `AutoContextTag`, `NoAutoContextTag` and `ContextCeilingTag`
  go; a card still wearing one is simply ignored (not migrated). (`api/exitguard.go` reads `atrium:context-ceiling`
  as a director marker for the exit guard, which is not a context tag and stays.)
- In `newcontext.go`: the capture prompt, the token and its checks, `newContextStop` and the mid-turn nudges, the
  ceiling wake, `handoffWritten`/`handoffExists` for the cycle.
- In `store/autocontext.go`: `auto_new_context`, `auto_new_context_k`, `auto_new_context_idle_s`,
  `context_ceiling_k` and their checks. In `api`: `context_threshold_k` (the 150k line), the floors tying those
  together, and their settings fields. `ContextSize.AutoK` goes; `Warn` now means past the limit.
- The runner layer of the limit: `harness.context_limit_k` is no longer read (the hub's per-harness setting is
  that layer now) and the runners page stops offering it. The column stays, unused, since SQLite cannot drop it
  safely in a migration.
- The tests of all of the above.

## Settings and the migration

New room settings (rows in `setting`, no DDL):

- `context_limits`: JSON object, harness id to thousands of tokens, each from 10 to 2000. Empty reads as the
  default `{"claude":200}`. Written by the hub.
- `context_handoff_dir`: an absolute directory, or empty for the default.

Migration **`0086_context_cycle`**, appended at the end of `migrations` per the header: one statement,
`DELETE FROM setting WHERE key IN ('auto_new_context','auto_new_context_k','auto_new_context_idle_s',
'context_ceiling_k','context_threshold_k')`. A DELETE is idempotent, so a re-run is harmless. Nothing else needs a
schema change: the per-card switch and limit are keys in the card's `overrides`.

Per-card overrides (validated in `api.go`'s PATCH like `context_limit_k` is today):

- `context_cycle`: `off`, or empty for on.
- `context_limit_k`: unchanged.

## How the hub hands the limit to rooms

The pattern `input_lag_log` already uses (`internal/link/inputlagsetting.go`), in a new
`internal/link/contextlimits.go`:

- Any `/v1/settings` write naming `context_limits`, in any scope, is read at the hub ahead of routing, validated,
  and stored in the hub's own settings (`HubSetting("context_limits")`).
- The hub passes it to every attached room. In the ALL view it answers itself with `{context_limits}`; in a named
  room's view the write goes on to that room as usual (whose answer the board reads) and the other rooms get it
  from the hub.
- A room that attaches later is sent the hub's value (`PushContextLimits`, beside `PushInputLag` in `OnAttach`), so
  a room asleep when it was changed still gets it.
- The ALL view's settings read overlays the hub's value, so the board shows the hub's answer, not the first room's.
- A room with no hub (a lone daemon) takes the write itself and works the same.

## Board

- **Details dialog** (`#detail`, `settings-spine.js`): a "context cycle" section with a switch ("cycle this card's
  context at its limit") and a limit box, with a line saying the limit in force and where it came from. Both
  write the card's overrides.
- **Settings**: the "context size to warn at" box is replaced by "context limit per harness", one `harness=k` list
  (`claude=200, codex=300`), saved through the hub. The handoff directory is a room field beside it.
- **History**: the stored handoff is a `handoff` row, its full text behind a disclosure.
- The chip says `context 1/3: waiting for ack`, `2/3: clearing`, `3/3: waking`. The size mark, the peek and the
  terminal list read the new limit and source. The runners page loses its limit field.

## Tests

Go, in `internal/daemon/contextcycle_test.go`: the limit reached mid-turn types the prompt once per turn across many
statusline updates; no ack never clears; ack, then `/clear`, then the wake; the per-card off switch and override;
the handoff stored on the card; `ready` with no file refused. API tests for the settings and override checks, a
store test for the migration, a link test for the hub fan-out. Headless board: the details switch and limit, and the
hub setting.

## Appendix: Context cycling plan, interview with clint, 2026-10-05

Merged from `docs/context-cycle-plan.md` on 2026-10-07.

Why: the 150k nudge made workers stop mid-task, the nudge blocked the handoff write so the clear never ran, and
the launcher notice pulled the orchestrator in, which stopped a card instead of letting it cycle. Churn.

### Decided

1. **No nudge.** No "wrap up" line on tool calls, no launcher notice. One event only: the limit.
2. **At the limit atrium starts the cycle**, exactly as if clint pressed ctrl-alt-n. Nobody hears anything. The
   worker reports only `done` or `blocked` when its work is over.
3. **The sequence:**
   - atrium types, terse: `you are at context limit. wrap what is in flight and prepare for context clear. when
     ready ack atrium` (exact wording under "Details")
   - the agent finishes, writes its handoff, runs `atrium ready` (6)
   - atrium types `/clear`
   - atrium types, terse: `read <handoff> and continue`
4. **The handoff lives in a location atrium configures**, not the card's cwd. See 8.
5. **Mid-turn at the limit: type the limit prompt at once**, the way an immediate say works. The agent reads it at
   its next step. No waiting for the turn to end.

6. **The ack is a command, `atrium ready`.** Cheapest in tokens: no MCP tool schema in every context, and it works in
   every runner the way `atrium finish` does.

7. **No ack means try again, never force.** Each time the turn ends without `atrium ready`, atrium types the limit
   prompt again. It never clears without an ack, with or without a handoff file. clint: "try, try again. don't be a
   dick".

   **Retry trigger:** each statusline update re-checks the card. If it is still past the limit with no ack, atrium
   types the prompt again. Debounce (my call): at most once per turn, so a burst of statusline updates inside one
   turn types it once.
8. **The handoff:** the file goes in `$TEMP/atrium/handoffs/<card-id>.md` (`%TEMP%` on Windows), overridable by a
   room setting. The limit prompt names the exact path. `atrium ready` also reads the file into the store, so
   atrium holds a copy on the card, shown in its history on the board, and the user can read it even after `$TEMP`
   is cleaned.

9. **Every card atrium supervises cycles, clint's own included.** A per-card switch turns it off, placed in the
   card's details panel.

10. **The limit is a hub setting, user defined, keyed by harness.** Default: `claude` at 200k. The hub hands it to
    every room. The 150k threshold, the per-card ceiling tag and the `auto_new_context` modes go away. A per-card
    override of the limit sits in the card's details panel, next to the off switch.

### Details decided without asking (clint can overrule)

- Removed: `contextnudge.go` (the tool-call nudge), the launcher context notice (`contextsize.go:246`), the
  `atrium:subagent` exclusion, `atrium:auto-new-context` / `atrium:no-auto-new-context` / ceiling tags. Old tags on
  existing cards are ignored, not migrated.
- The limit prompt names the handoff path and the full path of the atrium binary, because the CLI is not on PATH on
  every room: `you are at context limit. wrap what is in flight, write your handoff to <path>, then run <atrium>
  ready.`
- The wake prompt is `read <path> and continue.`
- CLAUDE.md reloads by itself after `/clear`, so the wake carries nothing else.
- A cycle in flight still holds queued messages, as today (`holdingMessages`), so nothing is typed into the clear.

### Side issues found on the way (not this plan)

- m1mini cards have no atrium-control MCP (no atrium_git_push), and the ziti clone I seeded by ssh has no hub
  remote. Spike-2 bd983a42c is stuck on m1mini until one of those is fixed.

### Build

Interview closed, clint said "work resumes". One Opus worker on m1mini, branch claude/r-context-cycle. Nothing
deploys without clint's yes.
