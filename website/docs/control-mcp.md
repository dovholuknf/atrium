---
title: The control MCP server
description: The tools a session uses to see the board, talk to other sessions, and launch colleagues.
---

# The control MCP server

:::note Needs a hub
The control server runs on the hub, so it needs `atrium run`. See [Rooms: start a hub](./rooms.md#start-a-hub).
:::

The hub serves one MCP server at `/_hub/mcp`. Every session on the board connects to it from inside its own claude
process, so no session spawns a helper. Each request carries the caller's identity in headers, `X-Atrium-Agent` and
`X-Atrium-Room`, which Claude Code fills in per session. It answers loopback only.

## Registering it

Add it to a session's MCP configuration as an HTTP server:

```json
{
  "mcpServers": {
    "atrium-control": {
      "type": "http",
      "url": "http://localhost:7778/_hub/mcp",
      "headers": {
        "X-Atrium-Agent": "${ATRIUM_AGENT_NAME}",
        "X-Atrium-Room": "${ATRIUM_ROOM}"
      }
    }
  }
}
```

Use your hub's board address. `7778` is the default. Atrium sets `ATRIUM_AGENT_NAME` and `ATRIUM_ROOM` in the
environment of every session it launches.

The seeded claude runner starts sessions with `--strict-mcp-config --mcp-config ~/.atrium/mcp.json`, so a
launched session gets exactly the servers listed in that file, and never stops on a prompt from a server it was not
going to use. Put the entry above in that file.

## The tools

| Tool | What it does |
| --- | --- |
| `atrium_status` | Is atrium running, is its store healthy, and what is on the board. Scoped to your room. |
| `atrium_peers` | The other sessions: what each is called, what it is doing, where, and how long it has waited. |
| `atrium_say` | Say something to another session. Typed in where atrium owns the terminal, queued otherwise. |
| `atrium_report` | Report to the session that launched you: `done` with a commit, `blocked`, `question` or `progress`. |
| `atrium_task` | One card: its status, what its runner is doing, its recent events, and whether you saw its last turn. |
| `atrium_launch` | Start a new agent in a directory, on its own card, supervised by atrium. |
| `atrium_exit` | Ask a session to finish and leave. Asked, not killed. |
| `atrium_cull` | Retire a worker whose merged work you accepted: it leaves, its worktree and branch are removed. |
| `atrium_git_url` | Where to fetch code that is not in your directory: the hub's copy of finished work, or a room's work in progress. |
| `atrium_git_clone` | A clone of a repository on this room, fetched if it is not there yet. Returns the path. |
| `restart_atrium` | Wind a room down and bring it straight back on the same database. |

### Talking, not typing

`atrium_say` answers with how the message went: `terminal` when it was typed in, `queued` when it waits for the
session's next tool call or turn end. Neither is a reply. The receiving session is told who sent it, so a message
reads as one agent talking to another.

Call `atrium_peers` before saying anything. The handle it returns is what `atrium_say` takes. A handle read off a
card title is usually wrong.

`atrium_say` takes `wake`, which resumes a parked session and delivers, including one on another room
(`name@room`). It costs a cold start, so leave it off unless the message is worth it. A say to a parked session
without it is refused and nothing is queued.

`atrium_say` and `atrium_report` take `kind`. `fyi` is news the receiver need not act on, and a launcher that holds its
notices keeps it on its card instead of being interrupted. `needs` is the default, and `done`, `blocked`, `question`
and anything carrying an ask are always `needs`.

### Launching a colleague

`atrium_launch` starts a real session, not a subagent. It has its own conversation, its own permission gate and its
own card, it outlives the session that started it, and you can watch it, type into it or take it over.

- **It starts empty.** It inherits nothing of the caller's conversation. Hand it what it needs with `prompt`.
- **`brief`** is written to `BRIEF.md` in the new session's directory and read first, so it survives compaction.
- **`theme`** picks its terminal theme. Workers usually wear `active-work`.
- **Its permission requests go to you**, on your board.
- **`room`** launches on another room through the hub. `cwd` is then a path on that machine.
- **At most ten** agent-launched sessions run at once. A launch past the cap is refused and recorded in the audit
  tab.

A session another session launched should end every turn with `atrium_report` or `atrium_say` to its launcher. A
turn that ends with neither is a silent stop, and the launcher is told, then your board.

### Culling a finished worker

`atrium_cull` is how a worker's slot comes back once its work is done. Calling it is the acceptance, so call it after
the worker's branch is merged into `claude/main` and you are finished with the work, never before. A worker cannot
cull itself.

- **The room checks.** A card not tagged `atrium:subagent`, or one whose branch is not merged, is refused whole.
- **It asks, then removes.** The worker is asked to leave, then its worktree is removed and its branch deleted.
- **Uncommitted work is kept.** A dirty worktree stays with its branch, and `kept` says why. The worker still leaves.
- **Nothing is forced.** Git removes the worktree only when it agrees it is clean. The card and its history stay.

### Seen

`atrium_task` with no card answers about the caller's own card. Its `seen` block says whether you have read the
last turn and which of its Open Questions you have not answered. `atrium_peers` counts unseen turns and open
questions per peer. An agent checks this before assuming you read what it wrote.

### Restarting from inside

`restart_atrium` is scheduled, not immediate, because it takes down every terminal the room owns, possibly the
caller's own. The room parks its working agents first, spawns a restarter that outlives it, and comes back as the
same room. Staged binaries are installed on the way.

### Fetching code you do not have

`atrium_git_url` takes `repo` (`host/owner/repo` or a name), and optionally `branch` and `room`. It answers with the URL
to `git fetch`, and whether the code is the hub's copy of pushed work or a room's work in progress. A branch that is
both says which is ahead. A repository the hub does not know answers with the closest names. Use it instead of asking
for a paste.
