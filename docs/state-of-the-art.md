# State of the art (atrium)

Read this before touching anything. Snapshot of where the project sits as of the last commit. Pair with
`CLAUDE.md` (architectural conventions), `CHANGELOG.md` (full iteration log), `README.md` (user reference),
and `docs/test-plan.md` (manual scenarios that must pass).

## What atrium is, in one paragraph

A localhost broker that lets one human in one terminal interact with many concurrent claude-code sessions.
Each session loads a small MCP server (`atrium-agent`) whose single `submit(kind, content)` tool POSTs to the
hub and long-polls for a reply. The hub is a Bubble Tea inline TUI on top of a `net/http` server. There's
also a separate read-only mode (`atrium serve`) that exposes the gwt session ledger as MCP tools, but the
hub/agent loop is where the action is.

## What works today

### Mode A (hub + agent loop)

- **Loop**: agent calls submit -> hub displays + long-polls -> human types -> hub returns prompt as tool
  result -> agent acts -> loops. Indefinitely.
- **Opt-in activation**: agent does NOTHING with the atrium tool until the human says "atrium" / "run
  atrium" / "atrium go" / etc. Activation message content beyond the trigger phrase is ignored. Exit with
  "stop atrium" / "leave atrium".
- **Disconnect resilience**: agent silently retries on hub-down with exponential backoff (5s -> 60s). LLM
  never sees connection errors. Stderr nag once on first failure, then every `ATRIUM_DISCONNECTED_LOG_INTERVAL`
  (default 10m).
- **Long-poll absorption**: hub timeout returns empty prompt -> agent transparently re-polls as
  `kind="keepalive"`. LLM never sees an empty prompt. Hub does not display keepalives.
- **Multi-agent**: default agent name is `<cwd-leaf>-<pid-mod-100000>`. Two agents in the same dir get
  distinct names. Override via `--name` or `$env:ATRIUM_AGENT_NAME`. Hub-side `/rename <wire> <alias>` adds a
  UI-only display name; routing still uses the wire name.
- **Permission gating**: PreToolUse hook (`dotfiles/claude/hooks/atrium-perm-hook.ps1`) auto-activates when
  `.mcp.json` containing `atrium-agent` is in cwd or any ancestor. Posts to hub `/permission`, blocks until
  human runs `/approve` or `/deny` (or `y`/`n`/`a`/`d`). Returns decision to claude-code. Fails OPEN when hub
  unreachable. Skips `mcp__*` tools (any MCP-provided), `ToolSearch`, and the read-only built-ins.
- **Inline TUI**: messages flow into the terminal's native scrollback via `tea.Println`. The TUI itself is
  a small floating bottom panel: optional perm banner, optional list view (perms/agents), header, tabs,
  input, status.
- **Loud perms**: bordered flashing banner at top of frame when pending. Terminal BEL on each new arrival.
  Status-bar nudge. Multi-pending shows "(N more queued -- see perms tab)".
- **Cursor nav**: in perms and agents views, up/down moves a cursor, enter / a / d act. In agents view,
  `delete` / `x` forgets a stale entry.
- **Switcher**: Ctrl-K opens an overlay; ↑/↓ + Enter to pick. `1`-`9` quick-switches to the Nth agent OR
  picks from `{choices}` if the active agent has unresolved choices.
- **Formatting affordances**: `{red}...{reset}` ANSI sentinels in agent content; `{choices}...{/choices}`
  blocks render as a numbered picker box and `1`-`9` send the chosen line back.
- **Slash commands**: `/agents`, `/perms`, `/chat`, `/approve [id]`, `/deny [id]`, `/rename`, `/help`.

### Mode B (read-only aggregator)

- `atrium serve`: MCP stdio server with `snapshot`, `wait_for_change`, `focus_session` tools, backed by
  `$env:WORKTREE_ROOT\sessions\*.json` and `watch\state.log` from gwt.
- `atrium status`: tabular snapshot to stdout, `--needs-input` / `--alive` filters.
- `atrium watch`: native Go tail of state.log.

## Layout

```
cmd/atrium/main.go        entry, wires cobra root
internal/cli/cli.go       subcommands: hub, agent, serve, status, watch
internal/hub/hub.go       HTTP server + simple stdin TUI + per-agent routing + perm state
internal/agent/agent.go   MCP stdio server with submit tool, silent retry, ServerOptions.Instructions
internal/tui/tui.go       Bubble Tea inline-mode floating bottom panel
internal/server/server.go Mode B MCP server (snapshot, wait_for_change, focus_session)
internal/state/state.go   gwt session JSON + state.log reader
```

External hook lives in the **dotfiles repo** at `<dotfiles-repo>\claude\hooks\atrium-perm-hook.ps1`.
It's wired into `settings.json`'s `PreToolUse` array via the same dotfiles. The atrium repo itself does NOT
own that file.

## Wire protocol

POST `/submit`:

```json
{ "agent": "string", "kind": "greeting|response|keepalive", "content": "string" }
```

Response (blocks up to hub long-poll timeout):

```json
{ "prompt": "string" }
```

POST `/permission` (from the PreToolUse hook, NOT from atrium-agent):

```json
{ "agent": "string", "command": "string", "tool": "Bash|Write|Edit|..." }
```

Response (blocks until human resolves):

```json
{ "decision": "approve|block", "reason": "string" }
```

## Load-bearing invariants. DO NOT REGRESS

1. **LLM never sees a hub disconnect.** Agent's `post` retries forever with backoff. If you tighten this,
   token burn returns.
2. **LLM never sees an empty prompt.** Long-poll timeouts absorbed as keepalives in the agent client. If
   you remove the keepalive loop, expect "still here, what next?" filler on every minute of idle.
3. **No token burn while idle.** Items 1+2 together.
4. **Activation is opt-in.** The agent does NOT auto-start the loop on first user message. Required so the
   same .mcp.json can sit in a project where you sometimes want normal claude and sometimes want the loop.
5. **Activation message content is ignored.** The trigger phrase is JUST a trigger. Any task in the same
   message is invisible to the hub and must not be acted on.
6. **Permission hook fails OPEN.** Hub down -> claude's normal permission flow takes over rather than the
   agent getting bricked. Failing closed is worse.
7. **Permission hook does NOT gate `mcp__*` or `ToolSearch`.** Gating the agent's own `submit` is circular
   and causes a perm-request per loop turn.
8. **Perm-requests never enter scrollback.** They're transient banner entries only.
9. **Default agent name includes the PID suffix.** Two agents in the same dir must NOT collide on a shared
   prompt channel.
10. **Wire name vs display name are separate.** `/rename` is UI-only; the wire name is the routing key and
    only the agent process can change it (via `--name` / env var).

The test plan codifies each of these. Run section A through F before tagging a build.

## Known rough edges (not bugs, just things we haven't done yet)

- **No persistence.** Hub state is all in memory. Restarting the hub wipes the conversation log; agents
  re-greet on reconnect. Adding per-agent JSONL transcripts under `$env:WORKTREE_ROOT\hub\` is on the
  short-list.
- **No auth.** Single-machine localhost only. Binding to a non-loopback address exposes the hub to anything
  on the LAN. Don't.
- **No proper unit tests yet.** Pure-Go helpers (`extractChoices`, `wrapLines`, `visibleLen`,
  `parseEvent`) are easy to test. Test plan section "Notes for future automation" calls this out.
- **Multi-agent UX still rough.** Switching with ctrl-k works but inline scrollback now interleaves all
  agents' messages without per-agent filtering. Could add a `/history <agent>` to dump just one agent's
  archive into scrollback on demand.
- **No "broadcast" yet.** Can't send the same prompt to N agents simultaneously.
- **TUI scrollback interleaving.** Inline-mode trade: native scrollback works perfectly, but you lose
  per-agent isolation. Acceptable for now.

## How to build and run

```powershell
pushd <atrium-repo>
go mod tidy
go build -o build.claude\atrium.exe .\cmd\atrium

# hub
.\build.claude\atrium.exe hub

# agent (usually loaded by claude-code as an MCP child; not run by hand)
# .mcp.json wires it. See README.

# read-only aggregator (Mode B)
.\build.claude\atrium.exe status
.\build.claude\atrium.exe watch
popd
```

For a friendlier wrapper, add this to your pwsh profile:

```powershell
function atrium-hub { pushd <atrium-repo>; go build -o build.claude\atrium.exe .\cmd\atrium; .\build.claude\atrium.exe hub; popd }
```

## Where the user wants this to go

Based on conversation history (not commits):

- The MCP server pattern is the right primitive. Don't fold this into Mercurius (which is workflow-shaped,
  request/response). Atrium is subscription/event-bus shaped. Sibling, not parent.
- For cross-machine, the answer is OpenZiti / Agora as transport, not extending atrium with networking
  code.
- Pi (earendil-works) is single-agent extensible; complementary, not competing. Ideas worth borrowing if
  this matures: message queue (Enter vs Alt+Enter modes), session-tree JSONL, an extensions framework.

## When you take over

Read in this order:

1. `README.md` -- 5 minute orientation.
2. `CLAUDE.md` -- architecture + conventions.
3. This file (`docs/state-of-the-art.md`) -- where we are.
4. `CHANGELOG.md` -- how we got here.
5. `docs/test-plan.md` -- what must keep working.

When you add a feature: update CHANGELOG with a new dated section at the top, extend test-plan with the new
scenario, update README user-facing reference if any user-visible behavior changed.

Do not break any of the load-bearing invariants without a reason recorded in the CHANGELOG.
