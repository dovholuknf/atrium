# What the other runners will tell you

The harness table offers four runners as equals. Only one of them has ever told atrium anything about itself.
This page is what each of the others actually offers, found out by running them rather than by reading about
them, and what atrium now does with it.

Everything about codex below was measured against **codex-cli 0.153.2** on Windows, with a probe hook in a
scratch `CODEX_HOME` that wrote its stdin to a file. Where a claim is not measured it says so in place.

## The short answer

| Runner | Hooks | Session id | Resume | What atrium gets |
| --- | --- | --- | --- | --- |
| claude code | twelve | yes | `--resume <id>` | everything |
| codex | twelve, same file shape, same payload fields | yes | `codex resume <id>` | everything except a notification |
| ollama | none | none | none | nothing |
| shell | none | none | none | nothing |

Two of four can report. Two cannot, and the board now says so on the row instead of leaving a blank where the
count goes.

## Codex

### It has hooks, and they are Claude Code's hooks with a different file

Codex fires twelve events: `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`,
`PermissionRequest`, `SubagentStart`, `SubagentStop`, `Stop`, `PreCompact`, `PostCompact` and `Interrupt`.

It has no `Notification` and no `PostToolUseFailure`, which are the two claude events with no codex row.

The file is `$CODEX_HOME/hooks.json`, or `~/.codex/hooks.json`, and its shape is the same object claude uses:
a `hooks` key, an event name, matcher entries, a list of `{type, command}`. Atrium has read and written that
file since the codex target landed.

### The payloads carry claude's field names

This is the part that decides how much work wiring a second runner is, and the answer is almost none. A real
`SessionStart`, reformatted and shortened:

```json
{ "session_id": "01a077bd-dd17-74d3-bd13-3c709f22218b",
  "transcript_path": "...\\sessions\\2026\\09\\06\\rollout-...jsonl",
  "cwd": "...\\work", "hook_event_name": "SessionStart",
  "model": "gpt-6-astra", "permission_mode": "bypassPermissions", "source": "startup" }
```

`session_id`, `cwd`, `transcript_path`, `source`, `reason`, `tool_name`, `tool_input`, `tool_use_id`,
`stop_hook_active`, `agent_id`, `agent_type` and `trigger` are all spelled the way claude spells them. Two
differ, and atrium reads neither: the prompt arrives as `prompt` rather than `user_input`, and a tool result as
`tool_response` rather than `tool_result`.

`tool_name` is normalised too. A codex shell call arrives as `"tool_name": "Bash"`, not as codex's own internal
name for it, so the badge on a card reads the same for both runners without a translation table.

Two fields are codex's own. `turn_id` is on every turn-scoped event, and `model` is on all of them. Neither is
read yet. `turn_id` is the better answer to "is this the same turn" than anything atrium currently infers, and
it is written down here rather than used, because nothing on a card asks that question today.

### The Stop hook works, block and all

This was the question that mattered, because delivering a message to an idle session is the only thing atrium
does that changes what a runner does, and it is built entirely on a `Stop` hook answering with a block.

A probe hook that printed

```json
{"decision":"block","reason":"Atrium says: reply with the single word BANANA and then stop."}
```

produced `hook: Stop Blocked` and then the model said BANANA and stopped. The second `Stop` arrived with
`"stop_hook_active": true`, exactly as claude sends it, so the guard in `internal/cli/turn.go` that refuses to
block twice in a row is doing its job on codex without a line changing.

So `atrium turn --event end` is wired for codex and carries the same warning it carries for claude. It is
offered by name and "install all" does not sweep it in.

### The permission gate works too, on `PreToolUse`

Codex reads the same output shape claude does. A probe hook that printed

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse",
  "permissionDecision":"deny","permissionDecisionReason":"atrium denied this"}}
```

got `Command blocked by PreToolUse hook: atrium denied this` and the call did not run.

Atrium's gate is `atrium-perm-hook.ps1` in the operator's own dotfiles, not something atrium writes, so nothing
here installs it. What this establishes is that pointing that same script at codex's `PreToolUse` gates codex
sessions with no change to the script. The board's own list still will not write it, for the reason it never
writes it for claude: it decides what runs.

Codex also has a `PermissionRequest` event, which is the narrower gate `docs/hook-coverage-spike.md` recommends
moving to. It is not offered, because atrium does nothing with it yet and a switch that reports success and
changes nothing is worse than no switch.

### The quoting, which is the one place codex is not claude

**Codex takes the first word of `command` as the program and does no quote handling on it.** Claude Code hands
the whole string to a shell, which needs the quotes and strips them.

Two runs differing only in two characters:

```
"C:/.../pwsh.exe" -NoProfile -File C:/.../probe.ps1     ->  hook: SessionStart Failed
 C:/.../pwsh.exe  -NoProfile -File C:/.../probe.ps1     ->  hook: SessionStart Completed
```

Quotes on the ARGUMENTS are honored. `-File "C:/Users/claude/atrium probe/probe.ps1"` runs. It is the program
alone.

Atrium quoted the path unconditionally, for both runners, because until now the only reader was a shell. That
means every codex hook atrium has ever written fails on any machine where the atrium binary sits under a path
with a space in it, once per event, reporting it as one line in a runner nobody is watching. `Target.
ProgramUnquoted` is the fix, and it carries a consequence worth stating: a path with a space in it has no
spelling that codex will run, so atrium refuses to write the file and the board says why on the row rather than
offering a button that fails when pressed.

Codex accepts no array form for `command`. It answers `invalid type: sequence, expected a string` and then
ignores **the whole file**, not just that entry, which is worth knowing because the only symptom is every hook
silently not firing.

### Trust

Codex will not run a hook it has not been shown. The operator approves it once in a codex session, or passes
`--dangerously-bypass-hook-trust`. Atrium writes the file and says this. It does not reach into the trust
store, because that is the one step whose whole purpose is that a human took it.

### Resume

`codex resume <SESSION_ID>` takes the same uuid codex puts in `session_id` on every hook payload, which is the
id a card already records. The seeded harness row had an empty `resume_args` and a note saying to confirm the
flag, so a codex card has been carrying a usable resume id and refusing to use it. Migration `0039_codex_resume`
fills it in for a database that already exists, guarded on the empty list so an operator's own value is not
overwritten.

Running that migration against a copy of a live database, which is the rule in `internal/store/CLAUDE.md`,
turned up the thing the guard is there for and a worse problem behind it. That machine already had codex
configured, as `resume --last`. The guard left it alone, correctly. But `--last` never mentions the id, so the
substitution silently does not happen and codex picks up whichever conversation that machine saw last, which on
a board with several cards is somebody else's. A launch that is resuming now refuses arguments with no
`{resume}` in them and says what it was given. It refuses rather than corrects: which spelling a runner wants
is the operator's to write.

### What is still not wired for codex, and why

- **`PostCompact`** and **`Interrupt`**. No atrium subcommand means either one.
- **`PermissionRequest`**. See above.
- **`turn_id`**. Recorded here, read by nothing.
- **`PreCompact`** is wired and has not been seen to fire. Filling a context to make it happen costs more than
  the fact is worth. The subcommand behind it reads `trigger` and records the moment either way.

## Ollama

`ollama run <model>` is a chat prompt. There is no hook system, no configuration file of hooks, no session
identifier handed to anything outside the process, and no tool calls to gate. It is a REPL that atrium owns a
pseudo terminal for.

So the honest answer is that a card for an ollama session shows the terminal and the things a human typed on the
card, and it will never show activity, a lifecycle or a resume id. That is not a gap to be filled later. It is
what the runner is.

## Shell

The same, and on purpose. The seeded row already says "a plain shell, not an agent, reports nothing about
itself". A shell could in principle be made to report through its prompt function, and that would be atrium
editing somebody's profile to make a board look busier, which is a bad trade.

## Aider

**Not measured.** Aider is not a row in the harness table and is not installed on the machine this was written
on, so everything that could be said here would be a guess. What is worth writing down is the shape of the
question for whoever picks it up: aider has no documented hook system of the kind claude and codex have, and
what it does have is a chat history file and `--message` for scripting. A runner whose only channel is a file
that grows would be a watcher, not a hook, and that is a different mechanism to everything on this page.

## What the board says now

Fixed rows, and each one says what it can report, which is the same argument
`docs/hook-coverage-spike.md` makes about lanes: report what was observed, not what a config file claims.

- A row whose command is `claude` or `codex` carries its own hooks button, with its own count, over its own
  file. Wiring one says nothing about the other and the dialog has always kept them in separate panels.
- A row atrium has no hooks target for carries `reports nothing`, with the reason on hover. Blank read as
  wired, and a card that never leaves `running` then read as atrium being broken rather than as the runner
  having nothing to say.
- A row atrium cannot point at this binary carries `hooks: cannot wire`, with the reason. Today the only cause
  is the codex quoting rule above.
- A codex entry that does not name codex reads as `points elsewhere` rather than as wired. It is the right
  binary and the right subcommand and it reports `claude`, which the path check cannot see, so without this it
  would sit there correct-looking and wrong with nothing offering to fix it.
