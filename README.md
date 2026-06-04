# Atrium

_The single open hall every agent passes through._

Atrium is a single-pane-of-glass for many concurrent claude-code sessions. It exposes the state of every session
(what's idle, what's thinking, what's waiting on input, where each one lives on disk) via an MCP server, a CLI table,
and a tail-able event stream. It is reader-only by design: it observes state that the `gwt` session hooks already
write; it does not orchestrate or inject prompts.

The repo is a sibling of (not a fork of) `michaelquigley/mercurius`. Both are MCP servers, but they solve different
problems: Mercurius is a request/response workflow broker (design -> review -> findings); Atrium is an event-bus
aggregator (subscription to "any session changed state").

## What it surfaces

For each registered session:

- `branch` / label
- `state` -- one of `thinking`, `idle`, `needs-input`
- `worktree_path`
- `window_name` (the wt window hosting the tab)
- `pid`
- `last_state_change` timestamp
- `saved` flag

The data already lives on disk in `$env:WORKTREE_ROOT\sessions\*.json` (written by the `gwt` SessionStart hook) and
`$env:WORKTREE_ROOT\watch\state.log` (appended by the `set-session-state.ps1` hook on every transition). Atrium
reads both and exposes them through three interfaces.

## Three interfaces

### 1. CLI (one-shot status, scriptable)

```sh
atrium status                # print the current state table
atrium status --needs-input  # filter to sessions waiting on a prompt
atrium watch                 # tail the state log live (similar to 'gwt watch', native Go)
```

### 2. MCP server (for any claude session to query)

Add to your claude `settings.json`:

```json
{
  "mcpServers": {
    "atrium": {
      "command": "atrium",
      "args": ["serve"]
    }
  }
}
```

Tools exposed:

| Tool | Shape | Purpose |
| --- | --- | --- |
| `snapshot` | request/response | Returns every session's current state. |
| `wait_for_change` | long-poll (up to N seconds) | Blocks until any session transitions, then returns the change. |
| `focus_session` | request/response | Brings the wt window hosting `<session_id>` to the foreground via `wt -w <window-name>`. |

The MCP shape means any claude tab can ask: "what are the other agents doing?" or "tell me when any of them flips
to needs-input."

### 3. Single-pane dashboard (planned)

A long-running `atrium dashboard` mode that renders a live table in one wt tab and exposes keyboard shortcuts to
focus another session, mark something resolved, etc. Not built yet.

## Install

```sh
go install ./cmd/atrium
```

## Config

Atrium reads its base paths from env vars first, falling back to the conventional layout used by `gwt`:

| Var | Default | Meaning |
| --- | --- | --- |
| `WORKTREE_ROOT` | `D:\worktrees` | Where session JSONs and `watch\state.log` live. |
| `ATRIUM_LONG_POLL_TIMEOUT` | `30s` | Default upper bound for `wait_for_change`. |

No YAML config required for the MVP. If/when one grows in, it will be `atrium.yaml` in the project root.

## License

Apache 2.0.
