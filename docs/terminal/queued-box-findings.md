# Claude Code's queued-message box: what was measured (r-say-typed-sits-in-claude-queue, S-0)

Measured 2026-10-15 for stage S-0 of `docs/rnd/say-typed-queue-design.md`. Nothing here is atrium code. The
design's every "unverified" claim about Claude Code is answered below, for the one version that was available.

## What was driven

A scratch Claude Code session on this machine, started by the worker itself and never one of the operator's cards.

- A throwaway git repo, and a throwaway `CLAUDE_CONFIG_DIR` holding a copy of the login, an onboarding-complete
  `.claude.json`, and a `settings.json` whose only content was four hooks (`UserPromptSubmit`, `PreToolUse`,
  `PostToolUse`, `Stop`) appending each payload and a timestamp to a log. The `ATRIUM_*` and `CLAUDE*` environment
  variables were stripped, so the session never reached the live atrium.
- A 100 line Go program on `go-pty` (ConPTY, the same library the room uses) that started `claude` on a 160x50
  terminal, logged every output chunk with a timestamp, and wrote the bytes it was told to. The bytes were typed
  the way atrium types them: the text, then a separate `\r`.
- Each run gave the model a prompt that ran one Bash call holding a node timer (45 to 70 s), so the "turn" was
  one long tool call, the case in the incident.

Timestamps below are from the hook log and from the session transcript (`projects/*/<session>.jsonl`).

## The version table

| Claude Code | Model | Mid-turn line read when | Chord Ctrl+X Ctrl+S | Chord kills the tool |
| --- | --- | --- | --- | --- |
| 2.1.301 (Windows, ConPTY) | Opus 5.5 and Fable 5.1 (the CLI chose) | the next step boundary, here the return of the running tool, in the same turn | bound by default, with text in the box it ends the wait for the running tool | no |

Only 2.1.301 was available, so the table has one row. A newer or older version is a row to add, by repeating the
runs below. An unknown version is therefore an unknown, which is what the design's decision 7 already says.

## The five questions

1. **Is a line typed mid-turn read at the next tool boundary, at the end of the turn, or only on a manual action?**
   At the next step boundary. A line typed at 02:54:11 into a Bash call that ran until 02:55:10 sat in the box for the
   whole call and was absorbed the moment the call returned (transcript `queue-operation remove`, reason
   `absorbed_mid_turn`, 02:55:10.044), then answered in the same turn (`DONEONE`, then `ALPHA`). It was not read
   while the tool ran, so a worker inside one long tool call reads nothing for the length of that call (gap G1 in
   the design stands). **The incident's "not drained even after the tool was killed and new calls ran" was not
   reproduced**: it needs a longer session or another state, and the runs here drained every time.
2. **Is Ctrl+X Ctrl+S bound, in which versions, and can it be rebound?** In 2.1.301 it is a default binding for the
   action `chat:sendNow`, beside `ctrl+enter` (the same action) and `ctrl+x enter` for `chat:queueSubmit`. It is
   rebindable, because the CLI loads a user `keybindings.json` and validates the action names it lists. Other
   versions were not available to check.
3. **What does the chord do when a tool is running?** It ends the wait for the tool and does not kill it. With a line
   in the box and a Bash call running, the chord made Claude Code move the call to the background (tool result
   text: "Command was moved to the background ... so that a message that arrived while it was running can reach you;
   it was not interrupted"), the box line was absorbed 0.4 s later (02:57:57.5 chord, 02:57:58.0 absorbed), the
   model read it and answered, and the node timer ran to its end and arrived later as a task notification.
   **The chord does not interrupt a running tool. It detaches a Bash call from the turn so the turn can read the
   line.** What it does to a tool that cannot be backgrounded (an MCP call, a fetch) was not measured.
4. **What does it do with an empty box, and with a dialog open?** Nothing in both. On an idle runner with an empty
   box nothing appeared and no hook fired. Mid-tool with an empty box the Bash call ran its full 46 s and returned
   normally. With the `/model` picker open the picker stayed as it was, and Esc closed it. So the chord is safe to
   send to an empty prompt, and the design's rule of sending it only on an empty line with no dialog is stricter than
   Claude Code needs, which is a margin and not a cost.
5. **Are a `UserPromptSubmit` hook and a `queued_command` transcript line written when the box drains, and with
   what text and timestamp?**
   - `UserPromptSubmit` fires **when the line is submitted into the box, not when it drains.** It fired at 02:54:11.159,
     0.1 s after the Enter and 59 s before the line was read. Its payload carries the text. **It is not proof of
     reading.** Design decision 3 lists it as read-proof signal (c), and that has to change: only an idle runner's
     submit is its read.
   - The transcript writes `queue-operation enqueue` at submit (02:54:11.172, `content` is the text) and
     `queue-operation remove` at drain (`reason: absorbed_mid_turn`, `commandUuid`, `deliveryId`). It also writes an
     attachment `queued_command` holding `prompt` (the text), `source_uuid`, `delivery_id`, `commandMode: prompt` and
     `origin: {kind: human}`, placed in the file after the tool result it was absorbed with. **Its own timestamp is the
     enqueue time, so the drain time is the `remove` line's, not the attachment's.** Both carry the full text.
   - A turn Claude Code starts itself (a finished background shell) goes through the same enqueue and a bare `dequeue`
     with the `<task-notification>` text.

## What this changes in the design

- **S-3 stays.** The chord does not interrupt a running tool, so it can be the escalation: type the line, then send it.
  It is gated by the version table (one row today) and does not help when the box is empty.
- **S-2's read proof must drop `UserPromptSubmit` as a read signal for a mid-turn line.** The proof is the `queue-operation
  remove` line (best, it has the time) or the `queued_command` attachment, matched by text. `UserPromptSubmit` stays a
  proof only for a line typed into an idle runner.
- **G1 is real and measured.** One long tool call delays a typed line, and a hook carry, until it returns. The
  chord is the only lever that shortens it, and for Bash only.

## How to repeat it for another version

Start the scratch session with the same four hooks, run one Bash call with a 60 s node timer, type a line during it,
and read the transcript's `queue-operation` lines and the hook log. Add a row to the version table.
