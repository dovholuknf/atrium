# r-new-llm-agnostic-hooks. Every atrium hook works for every runner (design, then build)

Status: not started. Owned by @runtime (the subcommands and payload reading), with @fabric for the `claudeconf`
targets and provisioning. Filed by the orchestrator 2026-09-30 on clint's word: "they need to be llm agnostic".
Design first, reviewed by @rnd before it is built.

## Today

Atrium has 12 hooks, all subcommands of the atrium binary (`hook`, `session`, `turn`). How far each runner gets:

- **claude:** all 12, in `~/.claude/settings.json` (`claudeconf.Claude`).
- **codex:** 9 of 12, in `$CODEX_HOME/hooks.json` (`claudeconf.Codex`). Missing: Notification and PostToolUseFailure
  (codex has no such events), and the permission gate (f-006 Q3, deferred). Codex payloads use the same field names
  as claude's except `prompt` and `tool_response` (`docs/runtime/other-runners.md`).
- **gemini, ollama, any other harness row:** no target. Their cards get no activity badge, no session start or end,
  and no permission gate.

## Wanted

1. **The permission gate on codex.** `atrium hook --event permission` already reads the payload. Add the row to the
   codex target, decide `PreToolUse` versus codex's own `PermissionRequest`, and check the decision shape codex
   honours on a live session.
2. **A target per runner atrium can launch.** For each harness row, either a hooks target (file path, event names,
   quoting rule, trust step, payload differences) or a stated reason it has none and what the board shows instead.
   Start with gemini.
3. **The events a runner lacks.** Say per runner what the card loses and what stands in for it (for example the
   reaper for session end, the Stop hook for tool failure). The board says "N of M" rather than hiding the gap.
4. **One payload reader.** Every subcommand reads a runner-neutral struct, so a new runner is a mapping table and a
   target, not edits across `internal/cli`.
5. **Provisioning.** `provision-room.ps1` wires every runner the room has, not only claude.

## Not in this item

- Replacing clint's own dotfiles hooks (`set-session-state`, `set-tab-title` and the rest). Those are his.
