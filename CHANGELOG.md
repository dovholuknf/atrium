# Changelog

A running log of what's been built. Newest first. No formal version cuts yet (everything is `v0.0.0-dev`); each
section heading is just "what landed in this iteration."

## Unreleased

- **The board can be reached from elsewhere, through an overlay atrium drives.** Atrium listens on loopback and
  has no login on purpose, and until now "use an overlay" was advice rather than a feature. The gear grows a
  panel per overlay: zrok publishes the board at a zrok address, OpenZiti hosts whatever services an identity is
  allowed to bind. Atrium keeps the configuration, starts the process and shows what it printed. It never opens
  an identity file, never proxies traffic, and has no opinion about who may connect, because that is the
  overlay's job and moving it here would be inventing the auth layer this project has ruled out twice. A zrok
  share defaults to private, and turning on a public one says out loud that the link has no login in front of it.
  Shares end with the daemon, since an address outliving the board it points at reads as the overlay being
  broken. See `docs/overlays.md`.
- **Global auto mode.** One switch for every session, including ones that have not started yet. Same slot in the
  permission chain as the per-session kind, which is last: it stops new questions and does not discard answers
  already given, so `never` rules, shelved cards and queued messages all still win. Recorded as `global-auto`
  rather than `auto`, because six hours later "I turned this session loose" and "I turned the whole board loose"
  are different answers. Kept in the database, so a restart is not consent to start asking again and not consent
  to keep approving either.
- **The header stops wrapping.** Nothing in it breaks across two lines any more: the tabs give up their width
  first and scroll, the actions keep theirs, and below 1150px the new-agent button and the auto-mode switch drop
  their labels rather than their shape.

- **Hooks can be wired one at a time, from the board or from a terminal.** Each row has its own `wire it`, and
  `atrium hook install [--event x]` makes the same edit from a shell, so the manual route is the same job by
  hand rather than a different one. Wanting the tool events and not the subagent count is a reasonable thing to
  want, and one button for all five made that impossible to say. An install that finds everything already
  correct writes nothing and keeps no backup, and says so rather than reporting a write.
- **The board hears about a hook the moment it lands.** `hook install` posts to `/hooks-changed`, which
  broadcasts over the event stream, so the count moves immediately instead of on the next poll of a tab that
  has to be open. Best effort like everything else atrium sends: the edit is already on disk and the poll is
  still behind it. The steps dialog drops each command as it is run and closes when the list empties, since a
  command you have run is not a step any more.
- **The daemon records where it is listening**, in the local runtime directory for the platform:
  `%LOCALAPPDATA%` on Windows, `XDG_RUNTIME_DIR` or `~/.local/state` on Linux. Deliberately nothing that roams,
  because a localhost port synced to another machine points a caller somewhere confidently wrong. `atrium hook`
  reads it, so a daemon on a port that is not the default needs no flag baked into `settings.json`. A file left
  behind by a daemon that was killed costs a connection refused in milliseconds, which is the fail-open path
  every hook already takes.
- **`atrium hook` cannot hang.** It read stdin to end of file, so run at a prompt it waited forever for someone
  to type EOF. An interactive stdin is not read at all now, and a pipe gets the same one second the post gets. A
  hook that can block indefinitely breaks the one rule that matters most: it must never fail a session.
- **A toast over a dialog is on top and clickable.** Two browser rules together rule out the easy answers: a
  modal is in the top layer, so nothing outside it can be drawn over it at any z-index, and a modal makes
  everything outside itself inert, so a popover drawn over one takes no clicks. Visible with dead buttons is
  worse than hidden. The host moves inside whichever modal is on top, and the stack it consults is recorded as
  `showModal` is called, because that order is not readable from the DOM: picking by document order put the
  toast in the dialog underneath the one on screen.
- **Clicking a toast or a notification closes whatever dialog is in the way.** A toast drawn over an open dialog
  lives inside it, since the top layer is the only place anything can draw over one. The click registered and
  the view changed, but the dialog stayed put over the thing you clicked to go and look at. A dialog holding
  edits nobody has saved, which is the runner form and the launch form, asks before it goes: throwing away a
  half-filled form because a toast arrived is worse than the toast being ignored.
- **The hooks are a subcommand, and the board wires them.** `atrium hook --event tool-start` replaces the
  PowerShell activity script. The script held a path only one machine had, so atrium could describe the wiring
  and never write it; a subcommand's path is this binary's own, which atrium knows. The runners tab reports
  which of the five are registered, which point at some other binary, and writes the missing ones into
  `~/.claude/settings.json`. Everything else in that file survives the round trip, the old file is copied aside
  first, and a settings file that will not parse is refused rather than rewritten. The `Stop` hook is not
  offered: one that blocks makes a session keep working, so it stays a manual decision. Reached from a button on
  the claude row, since these are claude's hooks and not that row's: they cover every claude on the machine and
  outlive the row being disabled. The count on that button can be turned off, through the same "do not ask me
  again" store as everything else, because not wiring them is a choice. Doing it by hand gets its own wide
  dialog: one command per block, each with a copy button, and the settings path copyable too.
- **Grouping is reachable from the board and the stack.** A `by project` / `off` control next to the sort pills,
  rather than only in the gear. It is not folded into the sort pills, because sorting and grouping are two
  questions and one control would mean picking a group rule costs you the sort. One setting behind both screens,
  so turning it off on the stack turns it off on the board.
- **A question you turned off can be turned back on.** Ticking "do not ask me again" saved the question as
  skipped but there was no way to undo it, and the gear's "ask me again" button called a function that did not
  exist. The gear now lists what is turned off and empties the list. Three questions carry the tick: asking a
  runner to exit, terminating one, and moving a card an agent is waiting on. Forgetting a card, clearing a
  column, deleting a runner and turning a session loose unattended ask every time, because each throws away
  something there is no way back to.
- **The subagent count comes from the subagent hooks.** `SubagentStart` takes it up, `SubagentStop` takes it
  down. It used to count `Task` tool calls on the way up, which reported the same subagent twice once the real
  hook was wired.
- **The board only rewrites the DOM when the markup changed.** The five second refresh is still blind, because
  ages tick with no server event, but the board, the stack, the terminal list, history and rules now compare
  before they write. Permissions already did.
- **Stack sort pills are one axis each.** `newest activity` and `quietest` were the same axis read two ways, as
  were `show wants me` and `sort by needs me`. Activity is first and the default.
- **Cards say what they are doing.** A live badge: thinking, running `Bash`, three subagents, and how long it has
  been at it. Fed by `POST /activity` from the tool hooks, and for free from `/permission` for gated sessions,
  since a permission request IS a tool starting. Never stored: a stored activity is a lie the moment the daemon
  restarts, and expires on its own after fifteen minutes of silence so a killed session stops claiming to be
  busy. See `docs/activity-design.md`.
- **Auto mode.** Per session: approve without asking, record everything. It does not override a `never` rule, a
  shelved card, or a queued message, because it means "stop asking me new questions" and not "forget the answers
  I already gave". Its counterpart is **what did it do?**, `GET /v1/tasks/{id}/review`: the decision log grouped
  by tool, identical calls folded into one line with a count, and the ones nobody saw put first. Interruption
  traded for review. See `docs/auto-mode.md`.
- **Rules can cover a folder.** A new rule kind covering work inside a directory: either the command names an
  absolute path inside it, or the session is working inside it and the command does not reach out. The second
  half is what makes it useful, since commands are written relative to where the session is and `go test ./...`
  names no path at all. Reaching out with an absolute path, or climbing out with `..`, still asks. This was
  previously only expressible as a command glob that had to account for the quoting itself, and
  `rm -f "C:/x/*"` silently fails against `rm -f "C:/x/y.db"` over the closing quote alone. Offered as a scope
  on any pending request, and as **allow a folder** in the rules toolbar. `POST /v1/rules` writes one by hand,
  which was not possible before: a rule could only be born from a request you had just read.
- **Board cards can be dragged and right clicked.** Drop between two cards for a midpoint rank, so an insert
  never renumbers the column; drop on a column to change status. Right click for open, attach, shelve, done,
  auto mode, review, terminate and forget, each of which previously cost opening the detail dialog.
- **Permission requests say who is asking.** With several sessions running, the same command means different
  things from different agents. On the pending card, in the decisions log, in the search, and in the CSV export.
- **Stopping is not killing.** `atrium stop` and `POST /v1/shutdown` reach the same wind-down that ctrl-c does:
  event streams released, supervised runners given ten seconds, listeners closed in order. Killing the process
  closes every pseudo terminal at once and takes the runners with it. Loopback only unless `--shutdown-token` is
  set, so a kill switch cannot be reached from a network the daemon was never meant to be on.
- **Resume ids are recorded.** Session hooks have always sent the harness's own session id and it was thrown
  away. Stored on every session event now, which is what makes a runner that was stopped, terminated, or lost
  with the daemon something to start again rather than something to lose.
- **A directory picker that browses the right machine.** `GET /v1/browse` lists the daemon's filesystem, with
  checkouts marked and sorted first. The browser's own picker reads whatever machine the browser is on, which is
  the wrong answer the moment the board is open on a phone. The launch form also gained recent directories, a
  resume checkbox that defaults to on, and enter to start.
- **Notifications take themselves down.** Permission notifications were sticky so a blocked agent could not
  scroll away unnoticed, and sticky on Windows means they never leave. There is now an expiry, default 30
  seconds, set under the gear. Choosing "never, until answered" restores the old behaviour on purpose. The
  service worker holds its own timer and the board sweeps expired ones whenever it is open, because a browser
  is free to shut a worker down before its timer fires.
- **Finished columns can be cleared.** `POST /v1/tasks/prune` deletes done and dead cards, optionally narrowed
  to one status and to those untouched for a given number of hours. A `clear` control sits in the done and dead
  column headers. Shelved is never swept, whatever it is asked, since shelving is the one act that says come
  back to this.
- **The command box fits the command.** It was two lines tall regardless of content, so a long command had to be
  scrolled inside a small window before it could be approved. It now grows to what it holds, up to 80% of the
  viewport, then scrolls.
- **The terminal fills the window.** Its height was a hardcoded `calc()` guess, which left a gap at one zoom
  level and overflowed at another. The header is measured instead, and the pane re-fits when anything around it
  changes size. An exited runner keeps its scrollback, since that holds the exit and the resume id, but stops
  presenting as a live session.
- **Runners installed as batch shims start.** `codex` and often `claude` resolve to a `.cmd` written by npm, and
  CreateProcess cannot start a batch file. It reported 0x80070002, "The system cannot find the file specified",
  for a file sitting on PATH, which sends you to look at PATH, permissions and the environment. `cmd /c` now
  goes in front of a `.cmd` or `.bat`, and PowerShell in front of a `.ps1`.
- **A launched runner has to prove it started.** The card was created before the process and nothing checked
  afterwards, so a misconfigured runner left a card in `running` describing a process that never got off the
  ground. A launch now waits two seconds, and a runner that falls over in that window puts its last terminal
  output on the card as the reason.
- **A new database is announced.** Opening the wrong path looks identical to every card and every rule having
  vanished, and `WORKTREE_ROOT` unset once made a hundred and twenty five rules appear to be gone. The daemon
  now says loudly when it created a database rather than found one.
- **Permission requests carry a dedup key.** The hook sends one built from the session and the exact request, so
  a retry after a daemon crash is recognised as the same question instead of being asked again.

## 2026-09-01 -- v2 prototype: durable state, a human-facing API, and a board

First working slice of `docs/architecture-v2.md`. New subcommand `atrium daemon` runs the whole thing.
`atrium hub` is untouched and still works exactly as before.

- **The hub is no longer amnesiac.** This reverses the "restart equals reset" invariant that
  `CLAUDE.md` and `docs/state-of-the-art.md` both declared. Restarting used to be the reset switch. It is now
  just a restart, and cards, history, and permission state survive it. The reversal is the entire point of v2:
  "how long has this been sitting" and "what was I even doing" cannot be answered from memory that dies with
  the process.
- **`internal/store`**: SQLite via `modernc.org/sqlite` (pure Go, so no cgo and no cross-compile pain). Schema
  written to stay Postgres portable: text ULID-ish keys, RFC3339 text timestamps, `CHECK` instead of enums,
  TEXT instead of JSONB, `?` placeholders. Tables are `task`, `event`, `permission`, `launch_spec`.
- **The halt.** Storage failure is not a degraded mode. Open or migration failure means the daemon refuses to
  start. `SQLITE_BUSY` is retried internally and never surfaces. Anything else halts: the agent-facing
  listener closes and stays closed, so runners see connection-refused and park on the backoff they already
  have, burning nothing. The process stays alive and the human-facing listener reports the cause.
- **Two listeners.** Agents on `--addr` (default `:7777`, same as the hub). Humans on `--http` (default
  `:7778`). Separate so a halt can kill the agent side without blinding the board.
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
  order, the permission dedup replay, rank placement, and the halt refusing further work.

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
- MCP `ServerOptions.Instructions` holds the loop bootstrap so the model knows to call `submit` on first turn
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
