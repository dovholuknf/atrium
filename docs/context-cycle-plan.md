# Context cycling plan, interview with clint, 2026-10-05

Why: the 150k nudge made workers stop mid-task, the nudge blocked the handoff write so the clear never ran, and
the launcher notice pulled the orchestrator in, which stopped a card instead of letting it cycle. Churn.

## Decided

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

## Details decided without asking (clint can overrule)

- Removed: `contextnudge.go` (the tool-call nudge), the launcher context notice (`contextsize.go:246`), the
  `atrium:subagent` exclusion, `atrium:auto-new-context` / `atrium:no-auto-new-context` / ceiling tags. Old tags on
  existing cards are ignored, not migrated.
- The limit prompt names the handoff path and the full path of the atrium binary, because the CLI is not on PATH on
  every room: `you are at context limit. wrap what is in flight, write your handoff to <path>, then run <atrium>
  ready.`
- The wake prompt is `read <path> and continue.`
- CLAUDE.md reloads by itself after `/clear`, so the wake carries nothing else.
- A cycle in flight still holds queued messages, as today (`holdingMessages`), so nothing is typed into the clear.

## Side issues found on the way (not this plan)

- m1mini cards have no atrium-control MCP (no atrium_git_push), and the ziti clone I seeded by ssh has no hub
  remote. Spike-2 bd983a42c is stuck on m1mini until one of those is fixed.

## Build

Interview closed, clint said "work resumes". One Opus worker on m1mini, branch claude/r-context-cycle. Nothing
deploys without clint's yes.
