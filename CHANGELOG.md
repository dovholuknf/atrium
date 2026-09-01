# Changelog

A running log of what's been built. Newest first. No formal version cuts yet (everything is `v0.0.0-dev`); each
section heading is just "what landed in this iteration."

## 2026-09-01 -- v2 prototype: durable state, a human-facing API, and a board

First working slice of `docs/architecture-v2.md`. New subcommand `atrium daemon` runs the whole thing.
`atrium hub` is untouched and still works exactly as before.

- **The hub is no longer amnesiac.** This deliberately reverses the "restart equals reset" invariant that
  `CLAUDE.md` and `docs/state-of-the-art.md` both declared. Restarting used to be the reset switch. It is now
  just a restart, and cards, history, and permission state survive it. The reversal is the entire point of v2:
  "how long has this been sitting" and "what was I even doing" cannot be answered from memory that dies with
  the process.
- **`internal/store`**: SQLite via `modernc.org/sqlite` (pure Go, so no cgo and no cross-compile pain). Schema
  written to stay Postgres portable: text ULID-ish keys, RFC3339 text timestamps, `CHECK` instead of enums,
  TEXT instead of JSONB, `?` placeholders. Tables are `task`, `event`, `permission`, `launch_spec`.
- **The wedge.** Storage failure is not a degraded mode. Open or migration failure means the daemon refuses to
  start. `SQLITE_BUSY` is retried internally and never surfaces. Anything else wedges: the agent-facing
  listener closes and stays closed, so runners see connection-refused and park on the backoff they already
  have, burning nothing. The process stays alive and the human-facing listener reports the cause.
- **Two listeners.** Agents on `--addr` (default `:7777`, same as the hub). Humans on `--http` (default
  `:7778`). Separate so a wedge can kill the agent side without blinding the board.
- **Task model.** Cards, not agent names. `wire_name` is now an attribute, and pid is only a reconnect hint,
  so restarting a runner no longer splits one piece of work across two cards.
- **Observed versus overrides.** Runner-reported fields refresh on every reconnect. Operator-set values live in
  `overrides` and always win. Rename a card and the name survives the agent dying. This generalizes v1's
  `/rename`, which only ever covered the display name.
- **Human-facing API**: `/v1/tasks`, `/v1/waiting`, `/v1/permissions`, per-task events and prompts, and an SSE
  stream at `/v1/events`.
- **A board.** Kanban by status with age on every card, a stack view ordered by longest wait, and a permission
  queue with approve and block-with-guidance. Served from the binary via `go:embed`. This is a plain page for
  now rather than the agreed React SPA: it exercises the same JSON plus SSE contract, so the server does not
  care which one is talking to it.
- **`rank`** orders cards within a column, with midpoint insertion so reordering never renumbers neighbours.
  The board sorts by rank, the stack sorts by wait time, on purpose.
- **First tests in the repo.** Ten in `internal/store` covering reconnect identity, override survival, waiting
  order, the permission dedup replay, rank placement, and the wedge refusing further work.

Known gaps in this slice: the TUI still consumes `*Hub` in process rather than the API, agents do not send a
registration payload yet (so observed data is limited to the wire name), and nothing launches or supervises
runners.

## 2026-06-11 -- permissions-only mode + deny-with-guidance

- The PreToolUse perm hook (`atrium-perm-hook.ps1`) now has a tri-state `ATRIUM_PERM_GATE`:
  - `on` / `force` / `1` / `true` / `yes`: gate EVERY session through the hub, no `.mcp.json` and no submit
    loop required. This is the permissions-only fleet mode: many agents funnel approvals to one hub pane.
  - `off`: never gate.
  - unset / anything else: auto-detect (gate only sessions wired to the atrium-agent MCP). Original behavior.
- Hub can now deny WITH free-form guidance, not just `y`/`n`. In the perms tab, type a message and press Enter:
  the hub denies the highlighted request and hands your text back to the agent as the block reason, so the
  agent course-corrects ("no, do X instead") rather than just getting a bare refusal.
- `/approve` and `/deny` now accept an optional trailing reason: `/deny 3 use the staging bucket instead`, or
  `/deny use a temp file` to deny the oldest with guidance. Works in both the Bubble Tea and `--simple` TUIs.
- Recommended activation (fleet-wide): set `ATRIUM_PERM_GATE=on` in the `env` block of settings.json and bump
  the atrium hook `timeout` so it blocks until you answer. The hook still fails OPEN when the hub is down.

## 2026-06-11 -- list-cursor navigation + forget-agent

- The perms and all-agents tabs now have cursor navigation: `↑`/`↓` (or `k`/`j`) move the highlight, `Enter`
  acts on the highlighted row. In the all-agents tab `Enter` opens that agent's chat; in the perms tab `Enter`
  or `a` approves and `d` denies the highlighted request. All gated behind an empty input so typing still works.
- New forget-agent action: `x` or `Delete` on the highlighted row in the all-agents tab drops the agent from
  both the TUI and the hub's in-memory maps (`Hub.Forget`). Use it to clear stale wire names left behind when a
  claude process dies. If that agent POSTs again it re-registers fresh.
- New slash command `/pick` (alias `/k`) opens the agent switcher overlay, same as `Ctrl-K`.
- These are Bubble Tea TUI only. The `--simple` line-mode TUI is unchanged (no rename / forget / pick).

## 2026-06-04 -- inline-mode TUI (native scrollback) + scoped gating

- TUI is now inline (no alt-screen). The chat content flows into the terminal's native scrollback via
  `tea.Println`. Mouse-wheel scroll, copy/paste, and terminal-side search all just work.
- The frame is a floating bottom panel: optional perm banner (top of frame, only when pending), perms /
  agents list (only on those views), header (active agent), tabs, input, status. Chat view in the frame is
  empty -- the conversation IS the scrollback.
- Perm-requests are filtered OUT of scrollback. They only appear in the floating banner. Same for
  keepalives (defensive; shouldn't arrive anyway).
- User's typed prompts are echo'd into scrollback as `[ts] you → <agent>` so the conversation reads top
  down: prompt, response, prompt, response.
- Permission hook now skips `mcp__*` (any MCP-provided tool) and `ToolSearch` (claude's meta-tool for
  finding other tools). Critically this stops the atrium-agent MCP's own `submit` from being gated, which
  was demanding a permission on every loop turn. MCP wiring itself IS the gate for those tools.

## 2026-06-04 -- two-line header + multi-pending banner

- Header is now two lines:
  - Line 1: `atrium │ <active agent name>  ← waiting  (+N unread elsewhere)  ·  K agents total`
  - Line 2: tabs `chat │ perms │ all agents`
- This makes the active agent feel like the parent of chat / perms rather than a sibling. The third tab is
  renamed `all agents` for clarity (it's the global view; ctrl-k is the inline picker).
- When more than one perm is pending, the banner now adds a "(showing oldest; N more queued -- see perms tab)"
  hint so it's obvious the banner is summarizing.
- Perm count in the tabs tab flashes red/yellow.

## 2026-06-04 -- pending-perm visibility (banner + beep)

- New permission arrivals now print the BEL byte (`\a` / 0x07). Most terminals (Windows Terminal default) beep
  on this. Fires once per new perm, not on every tick.
- Persistent, flashing, bordered banner across the TOP of every view (chat / perms / agents) whenever any
  perm is pending. Lists count, the first pending perm in detail (id / agent / tool / command preview), and
  the resolution keystrokes. Alternates yellow/red each tick. Designed to be unmissable.
- Status bar nudge "⚠ NEW permission #N -- press y/n" fires on arrival.

## 2026-06-04 -- activation-prompt content ignored + all-tool gating

- Activation phrase is now treated strictly as a trigger. Any task content in the same message (e.g.
  "atrium write a file") is ignored. The agent's only action on the activation turn is a greeting submit;
  real work waits for a hub prompt. Fixes a class of bugs where the agent did the activation-message task
  inside its claude tab, invisible to the hub.
- Permission hook now gates ALL tool calls that have side effects. Bash, Write, Edit, MultiEdit,
  NotebookEdit, and anything not in the read-only allowlist (`Read`, `Grep`, `Glob`, `WebFetch`,
  `WebSearch`, `TodoWrite`, `Task`) flow through atrium. Previously only Bash was gated, so file edits
  surfaced as claude's own in-tab permission UI instead of in the hub.
- Hub perm-request body now picks the most-useful field per tool: `command` for Bash, `file_path` (+ a
  "(replace edit)" / "(write N chars)" annotation) for Write/Edit, `url` for WebFetch, `pattern` for
  Grep/Glob, and the raw tool_input JSON as a fallback.

## 2026-06-04 -- unique default names + hub-side rename

- Default agent name is now `<cwd-leaf>-<pid-mod-100000>` (e.g., `atrium-19432`). Two agents in the same dir
  no longer collide on a shared prompt channel. Override still works via `--name` arg, `ATRIUM_AGENT_NAME`
  env, or by editing `.mcp.json`.
- New TUI slash command `/rename <agent> <new display name>`. Sets a UI-only alias; wire routing is unchanged.
  `/rename <agent>` with no second arg clears the alias.
- `@<name>` targeting now resolves wire names, display aliases, and prefix matches on either.
- Agents view and switcher show the display name with the wire name in parens when an alias is set, so you
  always know which is which.

## 2026-06-04 -- opt-in activation

- Agent instructions (MCP `ServerOptions.Instructions`) no longer auto-start the loop on first user message.
  The model now treats `atrium-agent` as opt-in: behaves like a normal claude session until the human types a
  recognizable activation phrase ("atrium", "run atrium", "start atrium", "atrium go", "enter atrium", etc).
- The exit phrase set ("stop atrium", "leave atrium") returns the session to default behavior without a
  trailing submit.
- Activation is by the model's judgment, not a regex on our side. The instructions tell it which phrasings
  count and which merely mention atrium in passing.

## 2026-06-04 -- choices picker + chat layout fixes

- Agent tool description now teaches the `{choices}...{/choices}` sentinel. When an agent has a small set of
  options for the human, it wraps them in that block; the TUI renders an inline numbered picker.
- TUI keystroke: `1`-`9` in the chat view picks the Nth choice for the active agent, sends that text back as the
  prompt, and clears the pending choices. Falls through to the agent quick-switch when no choices are pending.
- Chat viewport rendering: each message body is word-wrapped to viewport width, indented under a colored sigil
  (`◆` greeting, `·` response, `⚠` perm-request), and separated from the next message by a muted horizontal
  rule. Long (e.g., 100-line) responses are now readable instead of blowing the layout.

## 2026-06-04 -- per-agent TUI + agent switcher

- TUI now keeps per-agent scrollback. Chat view focuses on ONE agent at a time; tab bar reads `chat: <name>` and
  shows `(+N)` when other agents have unread.
- `Ctrl-K` opens the agent switcher (↑/↓, Enter to pick, Esc to cancel).
- `1`-`9` quick-switches to the Nth known agent (when input is empty and the active agent has no pending
  choices).
- Agents view shows every known agent with a flashing `← waiting` marker on agents that submitted and haven't
  been answered. Default routing target is marked with `>`.
- Hub tracks per-agent "waiting" flag: set true on every real submit, cleared on every prompt send. Exposed via
  `Hub.Waiting()` and `Hub.IsWaiting()`.
- Routing default for typed prompts is now the ACTIVE chat agent (was: most-recent submitter). Explicit
  `@<name>` overrides still work.

## 2026-06-04 -- Bubble Tea TUI (with --simple fallback)

- New `internal/tui` package: full-screen alt-screen Bubble Tea UI for `atrium hub`.
- Three tabbed views: `chat | perms | agents`. Tab / Shift+Tab to cycle. Slash-commands `/chat /perms /agents`
  jump directly.
- Status bar at the bottom shows transient feedback (e.g., "approve perm #5") and key hints.
- Old plain stdin TUI is still available via `atrium hub --simple` (single-terminal fallback when the
  full-screen UI is undesirable -- piping, scripting, dumb terminals).
- Added Charm deps: `bubbletea`, `bubbles`, `lipgloss`.

## 2026-06-04 -- permission gating via PreToolUse hook

- New hook script `dotfiles/claude/hooks/atrium-perm-hook.ps1`. Fires as a claude-code `PreToolUse` hook.
- Auto-activates when the project has an `.mcp.json` referencing `atrium-agent` somewhere in cwd or ancestors.
  Opt-out via `ATRIUM_PERM_GATE=off`.
- For Bash tool calls, POSTs to atrium hub `/permission` with `{agent, command, tool}` and blocks until the
  human at the hub runs `/approve N` or `/deny N` (or `y` / `n`). Hook returns `{decision: approve|block}` to
  claude-code, which obeys.
- Fails OPEN when atrium is unreachable: agent keeps working under claude-code's normal permission flow rather
  than getting bricked.
- Existing footgun-guarding hook (`pre-tool-use-hook.ps1`) still runs after this; an atrium-approved command
  can still be blocked by static rules.

## 2026-06-04 -- ANSI sentinel translation

- Hub TUI (both simple and Bubble Tea) translates curly-brace sentinels in agent content to real ANSI escapes
  before printing. Vocabulary: `{reset}` `{bold}` `{dim}` `{underline}`, foregrounds `{red}` `{green}` etc, plus
  `{bgred}` etc. Auto-appends `{reset}` if the message ends without one.
- Agent tool description teaches the vocabulary. No more relying on the LLM smuggling raw `0x1b` bytes through.
- Perm-request announcements use the same sentinels so they pop visually.

## 2026-06-04 -- hub/agent core (Mode A)

- New `internal/hub` package: HTTP server on `:7777` (configurable) plus an interactive stdin TUI.
- New `internal/agent` package: MCP server with a single `submit(kind, content)` tool.
- Loop: agent calls `submit` -> hub displays content + long-polls for a human prompt -> returns the prompt as
  the tool result -> agent processes -> calls `submit` again. Forever.
- Resilience:
  - LLM never sees a connection error. The agent's `post` retries indefinitely with exponential backoff
    (5s -> 60s) on every transport failure. Stderr nag once per `ATRIUM_DISCONNECTED_LOG_INTERVAL` (default 10m).
  - LLM never sees an empty prompt. Long-poll timeouts are absorbed internally as `kind="keepalive"`, which the
    hub does NOT display.
- MCP `ServerOptions.Instructions` carries the loop bootstrap so the model knows to call `submit` on first turn
  without re-pasting a long prompt.
- Agent name defaults to the cwd leaf when `--name` isn't passed. Override per `.mcp.json` if needed.
- Hub TUI commands: `@<agent> text`, `/agents`, `/perms`, `/approve [id]`, `/deny [id]`, `/help`. Plus `y`/`n`
  shortcuts to approve/deny the oldest pending permission.

## 2026-06-04 -- Mode B aggregator (read-only)

- `atrium serve` runs as an MCP stdio server with three tools backed by the gwt session ledger:
  - `snapshot` -- every session's current state.
  - `wait_for_change` -- long-poll for the next state transition (default 30s, max 300s, `since` cursor).
  - `focus_session` -- best-effort `wt.exe -w <window> focus-tab`.
- `atrium status` (CLI) prints the same data as a table; `atrium watch` is a native Go replacement for
  `gwt watch`.
- Reads `$env:WORKTREE_ROOT\sessions\*.json` (per-session state) and `watch\state.log` (transition events).
  Strictly observer; writes nothing.
