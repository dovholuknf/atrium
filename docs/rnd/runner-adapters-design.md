# Runner adapters: a contract others can implement, so atrium does not own every runner

Status: design, not built. Item `rnd-runner-hooks-extensible`. Written by @rnd, 2026-10-25.

Origin: clint, 2026-10-21, after the codex permissions and hooks parity fix (8e5706c38): "so the same thing with
opencode too. we should figure out if this is 'extensible' could people author their own hooks for this sort thing
for pi agent hermes etc etc ... i don't want to 'own' this for atrium if we can avoid it".

Read for this:
- `internal/base/claudeconf/target.go` and `hooks.go`: the Go `Target` per runner and the `WantedHooks` table.
- `internal/cli/hook_permission.go`, `hook.go`, `session.go`, `turn.go`, `finish.go`: the subcommands hooks run.
- `scripts/opencode/atrium.js`: the opencode plugin, which is already an adapter written outside the Go code.
- `internal/runnerprofile/profile.go`, `internal/runnersetup/`: the other two per-runner tables.
- `docs/runtime/other-runners.md`, `docs/runtime/codex-parity.md`, `docs/runtime/wiring-a-runner.md`,
  `docs/backlog/runtime/r-new-llm-agnostic-hooks.md`.

Survey sources are named in each row. Claude, codex and opencode were measured by atrium before this doc. Nothing
about pi, hermes, gemini hooks, aider or cursor was run for it: those rows come from upstream docs and are marked
**unmeasured**.

## 0. The answer

- **The contract already exists, and it is the atrium CLI, not the HTTP listener.** Every runner atrium reaches
  today reaches it by running `atrium hook|session|turn --event <e> --runner <id>` with a Claude Code shaped JSON on
  stdin and reading a Claude Code shaped JSON from stdout. The opencode plugin proves a third party can speak it:
  344 lines of JS, no Go change, and the board cannot tell the difference.
- **Publish that as `atrium runner protocol v1`**, frozen at what claude, codex and opencode use today. The HTTP
  endpoints behind it (`/activity`, `/session`, `/stop`, `/permission`, `/gate`, `/finish`) stay internal: the CLI
  owns port discovery, retries, fail-open, pid finding and agent naming, and none of that should be re-written in
  every adapter.
- **An adapter is the runner's own plugin, or the runner's own hooks file.** Atrium's line stops at the CLI's stdin
  and stdout. Whoever writes the adapter owns the mapping from their runner's events and fields to v1. Atrium does
  not ship, install or test third-party adapters.
- **Claude and codex become the reference adapters** at the protocol level, which they already are in practice.
  Their installers (`claudeconf.Target`) stay in Go, because writing an operator's config file safely is the part
  worth owning.
- **Opencode is the first adapter to leave the tree**: `scripts/opencode/atrium.js` moves to its own package and
  repo, outside atrium, and becomes the worked example for pi and hermes.
- **The board shows what a runner reported, not what an adapter claimed.** A card says which of the features it has
  heard from (activity, gate, turn end, delivery, usage) and the rest say "this runner does not report it".

## 1. What atrium needs from a runner

Six things. Each is one subcommand today, and each has its own idea of what failing open means.

| Feature | Runner moment | v1 call | Answer the adapter acts on | Fail open means |
| --- | --- | --- | --- | --- |
| Session | opens, closes, compacts | `atrium session --event start\|end\|compact` | none (start may add context) | no card change. The reaper ends a card whose pid is gone. 3 s budget |
| Activity | tool starts, ends, fails, prompt sent, subagent up, down, waiting | `atrium hook --event tool-start\|tool-end\|tool-failed\|prompt\|subagent-start\|subagent-end\|notification` | none, fire and forget | the badge is stale. 1 s budget, no retry |
| Permission gate | before a tool runs | `atrium hook --event permission` | allow, deny with reason, allow an edited input | prints nothing, exit 0, and the runner's own permission flow decides. No deadline on the human, 2 s on the probe |
| Turn end | the model stops and waits | `atrium turn --event end` | block with a reason (deliver a message) or continue | prints `{"continue":true}`. Never blocks twice in a row (`stop_hook_active`). 2 s budget |
| Finish | the model declares the work over | `atrium finish [recap]`, run by the model | none | the card stays where it is |
| Usage | a turn's tokens | none in v1. The room reads the runner's transcript itself (claude) or its export (opencode replies) | none | no usage rows |

### 1.1 What goes in

Stdin is one JSON object. These fields are read, all optional, all spelled the way Claude Code spells them:
`session_id`, `cwd`, `transcript_path`, `hook_event_name`, `source`, `reason`, `trigger`, `tool_name`,
`tool_input`, `tool_use_id`, `stop_hook_active`, `agent_id`, `agent_type`, `notification_type`. A missing field is
a fact nobody claimed, never an error.

`tool_name` and `tool_input` use Claude's names (`Bash`, `Edit`, `Write`, `file_path`, `old_string`, ...). The gate
skips pure reads by those names and standing rules match them, so an adapter that sends its own names gets every
read gated and no rule matched. The opencode plugin carries the two maps that do this (`TOOL_NAMES`, `ARG_NAMES`).

The environment carries the rest: `ATRIUM_RUNNER` (atrium launched this, so report), `ATRIUM_TASK_ID` (which card),
`ATRIUM_LOCATION` or the location file (where the room is and which binary is current), `ATRIUM_HOOK_EXE` (an
override), `ATRIUM_PERM_GATE` (off, on). An adapter that sees no `ATRIUM_RUNNER` does nothing, which is how a
runner started by hand in another terminal is left alone.

### 1.2 What comes out

Only two calls answer, and they are the two that change what a session does:

- **Permission.** `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow|deny",
  "permissionDecisionReason":"...","updatedInput":{...}}}`, or nothing. `updatedInput` is the whole input, Claude
  spelled. An adapter that cannot apply it must refuse the call, never run the original (`applyEdit` in the opencode
  plugin is the pattern).
- **Turn end.** `{"decision":"block","reason":"..."}` or `{"continue":true}`. A block's reason is a message for
  the model. A runner with no Stop to block sends the reason as its next prompt instead, as the opencode plugin does
  with `client.session.promptAsync`.

Codex's native `PermissionRequest` answer (`decision.behavior`) is the one output today that is not the Claude
shape. It is selected by sniffing `hook_event_name` in `permissionHookForRunner`. That is the smell this doc is
about: a second runner-specific output in Go is where "atrium owns the runner" starts.

### 1.3 How a queued message gets in

A say reaches a session three ways, and an adapter decides which of them its runner has:

1. **At turn end**, by the block above. The only way into an idle session.
2. **At the next gated tool call**, as a deny whose reason is the message. A busy session makes these constantly.
3. **Typed into the terminal** by the room, when the runner profile says the runner reads mid-turn input
   (`MidTurnInput`). This needs no adapter at all, and it is what a runner with no hooks falls back to.

A runner with none of the three gets "queued" and nothing ever delivers it. The board must say so on the say, not
after.

### 1.4 Is v1 stable enough to publish

Yes, for the input and the claude-shaped outputs. They have carried three runners without a change, and the field
list above has not grown since codex. Three things should be settled before it is called v1, all small:

- **A version on the wire.** The adapter sends `"protocol": 1` in stdin, the CLI ignores what it does not know.
  Without it there is no way to change an output shape later without breaking every adapter at once.
- **Codex's native shape becomes an opt-in output, `--answer codex-permission`**, rather than a sniff. Today any
  payload with `hook_event_name: PermissionRequest` gets codex's answer, which a gemini adapter would trip over.
- **Usage** is left out of v1 on purpose (section 5).

## 2. Where an adapter lives

Three candidates were weighed.

| Shape | What it is | Who owns the mapping | Fits | Breaks on |
| --- | --- | --- | --- | --- |
| A. Data file | a row or JSON/YAML in atrium: config path, event-name map, field map, output template | atrium, as data | runners with a Claude-like hooks file (codex, gemini, cursor) | anything not a hooks file (opencode, pi, hermes have plugins, not files), quoting rules, multi-step answers like opencode's prompt-instead-of-block. The data format grows into a language |
| B. Shim program | a small program the runner's hook runs, stdin in the runner's JSON, stdout the runner's JSON, calling atrium inside | the shim's author | hooks-file runners whose shapes differ from Claude's | same reach as A, plus a second binary to install and find. It is A written in code |
| C. Runner plugin | code the runner itself loads (opencode JS plugin, pi TS extension, hermes Python plugin), calling the atrium CLI | the plugin's author, in the runner's ecosystem | every runner with a plugin system, and it is the only shape that can do prompt-instead-of-block | runners with no plugin system (aider). Needs node, python or bun, which the runner already has |

**Recommendation: C for runners with a plugin system, and the runner's own hooks file for runners with one, with
atrium's line at the CLI's stdin and stdout.** A and B are both ways for atrium to keep owning the mapping in a
different file, which is the thing clint asked to avoid.

The line, said once:

- **Atrium owns:** the v1 subcommands and their fail-open rules, the HTTP behind them, the location file, the
  environment variables, the spec document, a conformance check (section 6), and installers for the two runners
  it ships reference adapters for (claude, codex).
- **The adapter author owns:** finding the runner's events, mapping names and fields into v1, applying an answer
  inside the runner (deny, edit, deliver), packaging it the runner's way (npm, pi package, pip), installing it, and
  keeping it working when the runner changes.

A runner whose hooks file is already Claude-shaped (gemini, cursor) needs no plugin, only lines in its file that run
the v1 subcommands. Those lines are the adapter, and its author writes them, the way `docs/runtime/wiring-a-runner.md`
already asks an agent to.

## 3. Survey

What each runner exposes, against the six features. "v1 fit" is how far a plugin or hooks file could take it with no
Go change.

| Runner | Surface | Session | Activity | Gate (deny, edit) | Turn end, deliver | Usage | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| claude | hooks file, `~/.claude/settings.json` | yes | all | yes, yes | Stop block | transcript | measured, reference |
| codex | hooks file, `$CODEX_HOME/hooks.json`, Claude shape | yes | no notification, no tool-failed | `PermissionRequest`: deny yes, edit no (deny with the edit as the reason) | Stop block | rollout jsonl, not read | measured 0.153.2, 0.159.0 |
| opencode | JS plugin, `~/.config/opencode/plugin/` | yes | tools, prompt, subagents | `tool.execute.before`: throw denies, mutate `output.args` edits | no Stop. `session.idle` then `promptAsync` | `opencode export`, read for replies only | measured 1.18.34, plugin in tree |
| pi | TS extensions, `~/.pi/agent/extensions/` or `.pi/extensions/` | `session_start` and shutdown events | `tool_call`, `tool_result`, turn events | `tool_call` returns `{block, reason}`, mutating `event.input` edits | `agent_end`, then `pi.sendUserMessage` (with `deliverAs` mid-stream) | usage on assistant messages | unmeasured, pi.dev docs |
| hermes | Python plugins, `register(ctx)` with `ctx.register_hook` | `on_session_start`, `on_session_end` | `pre_tool_call`, `post_tool_call`, llm calls | `pre_tool_call` returns `{"action":"block"}` or `{"action":"modify","args":...}` | no documented way to inject a message from a plugin | unknown | unmeasured, hermes docs |
| gemini | hooks file in `settings.json`, Claude-like | `SessionStart`, `SessionEnd` | `BeforeTool`, `AfterTool`, `BeforeAgent`, `Notification`, `PreCompress` | `BeforeTool` decision `deny` with reason, rewrites args | `AfterAgent` can block and retry | unknown | unmeasured, geminicli.com |
| aider | none. CLI flags, a chat history file, a Python API | none | none | none, aider has no tool calls to gate | `--notifications-command` runs when it waits on input (per aider's docs, unchecked) | none | unmeasured |
| cursor | `hooks.json` (`~/.cursor/` or `.cursor/`), its own shape | unknown | `beforeShellExecution`, `afterFileEdit`, `beforeSubmitPrompt` | `permission: allow\|deny\|ask` for shell and MCP only | `stop`, `followup_message` reported working in some versions | unknown | unmeasured. Forum reports say the CLI ignores hooks the IDE runs |

What follows from the table:

- **Pi and hermes are plugin runners, like opencode.** Each can reach session, activity and the gate with a plugin
  that calls the v1 CLI. Pi can also deliver a message to an idle session, the same way opencode does. Hermes, as
  documented, cannot, so a hermes card would rely on typing into the terminal.
- **Gemini is the codex case again**: a Claude-like hooks file, different event names. Its output keys are close to
  Claude's but not the same, so it either gets a v1 answer shape of its own (an `--answer gemini` in the CLI, which
  is atrium owning it again) or a shim. This is the one place B earns a mention, and question 3 asks it.
- **Cursor** is not worth an adapter until its CLI is shown to run hooks at all.
- **Aider** can reach turn end and nothing else. Its card shows the terminal, the room types says into it, and the
  board says "reports turn end only".
- **What none of them can give that claude gives**: compaction with a trigger, the notification kinds that
  distinguish a question from a finished turn, and `transcript_path` pointing at a file the room can read for usage
  and bubbles.

## 4. Claude and codex on the same mechanism

They already are, at the protocol level. Both register lines that run v1 subcommands with v1 input. What makes them
special is two things in Go, and only one should go:

- **Keep:** `claudeconf.Target` writing the operator's config file, with the round-trip-safe map walk, the backup,
  the quoting rule, the trust note. That is hard to get right, it touches a file that breaks every session when it
  is wrong, and it is atrium's to get right for the runners it ships.
- **Move:** the runner-specific output. `nativePermissionAnswer` becomes the reference for an `--answer` flag the
  codex lines name explicitly, and the sniff on `hook_event_name` goes.

So claude and codex are the reference adapters of type "hooks file", opencode is the reference of type "plugin",
and the spec points at all three.

## 5. Usage

Usage is the one feature that does not fit the hook shape. A hook fires at a moment, and usage is a sum over a turn
that the runner already wrote somewhere. Today the room reads Claude's transcript at Stop (`usage.go`) and nothing
else is read for tokens, so every non-claude card has no usage rows.

Two ways in, for a later v2:

- **The adapter reports it.** `atrium turn --event end` takes an optional `usage` object (`model`, `input`,
  `output`, `cache_read`, `cache_write`) summed over the turn by the plugin, which has the numbers in hand on pi and
  opencode. Atrium prices it and writes the row. This keeps atrium out of every runner's storage format.
- **Atrium reads the runner's store**, as `opencodereader.go` does for replies. Accurate, and exactly the "atrium
  owns opencode forever" this doc is avoiding.

Recommend the first, after v1 has one outside adapter using it.

## 6. What the board shows for a partial adapter

Report what was observed, the rule `docs/rnd/hook-coverage-spike.md` already argues for. A card records which v1
calls it has received in this session (a set in memory, like activity), and the runner row records the union over
its recent sessions.

- **The runner row** says `reports: session, activity, gate, turn end` or `reports nothing`, from what was heard,
  replacing today's split between "has a claudeconf target" and "reports nothing".
- **A say to a card with no delivery path** says so before it is sent: "this runner has no way to receive a message
  while idle. It will be typed when the terminal is free" or "cannot be delivered".
- **A card that never reported a turn end** does not move to needs-input. Its column says `running (unreported)`
  rather than implying the session is busy.
- **The gate switch** is offered only where a gate call has been seen. A runner without one is never shown as
  gated.

A conformance check helps an author get there: `atrium runner check <cmd>` runs the adapter against the fake
runner's payloads (`internal/cli/fakerunner_integration_test.go` already holds them) and prints which features
answered. That is a later stage, not the first step.

## 7. Staged plan

1. **Smallest first step: write the spec, change nothing.** `docs/runtime/runner-protocol.md` states v1 as section 1
   says it, points at claude and codex lines and at `scripts/opencode/atrium.js` as the three reference adapters,
   and replaces the "what to produce" half of `wiring-a-runner.md`. No code. It is the document an outside author
   needs, and writing it is what shows whether v1 is really as stable as section 1.4 claims.
2. **Version and answer flag.** `"protocol": 1` read and ignored, `--answer codex-permission` on the codex lines
   with the sniff kept for one release so installed codex files keep working. A store migration is not needed.
3. **Opencode leaves the tree.** `scripts/opencode/atrium.js` and its test move to their own repo and npm package,
   installed with opencode's own plugin config. Atrium keeps one line in the spec pointing at it.
4. **Observed coverage on the board** (section 6), which replaces `runnerprofile.Hooks` and the gemini `NA` check.
5. **Usage in turn end** (section 5), once opencode or pi reports it.
6. **Invite pi and hermes adapters** from their communities, pointing at the spec and the opencode package. Atrium
   writes none of them.

## 8. Open questions for clint

1. Is "the atrium CLI with Claude-shaped JSON" the contract you want to publish, knowing it ties every adapter to
   Claude Code's field names for good? The alternative is a neutral v2 shape, which costs a translation for claude
   and codex too.
2. Should `scripts/opencode/atrium.js` leave the repo (stage 3), or stay as an in-tree reference that atrium tests
   but does not promise to keep up with opencode releases?
3. Gemini's hooks file is close to Claude's with different output keys. Do we add `--answer gemini` to the CLI
   (atrium owns a little of gemini) or leave gemini to an outside shim author?
4. Is usage in scope for outside adapters at all, or does a non-claude card simply have no usage rows?
5. For a runner with no delivery path, should a say be refused at send time, or queued and typed into the terminal
   whenever the room thinks it is safe?
6. Hermes and pi need someone to write the first adapter against a real install. Is that an atrium worker on a
   machine with them installed, or do we wait for their users?
