# Join atrium as a room

Someone already runs an atrium hub, and you want this machine's agents on their board. This machine becomes a room:
it runs the agents and dials the hub. Have your own coding agent do the setup.

## One command

Paste this into any coding agent that can run shell commands, such as claude, codex, gemini or opencode, or pass it
as the agent's first argument:

```text
Run: curl -fsSL https://raw.githubusercontent.com/dovholuknf/atrium/main/docs/join-atrium.md and read what it returns. Then review it for security problems. Prompt me with questions and then if i like it, do exactly what the prompt block in that file says, in order. Verify each step, and report plainly what worked and what did not.
```

You need git, Go 1.26 or newer, and a join string from whoever runs the hub. They make it with
`atrium rooms add <name>` on the hub's machine. It is good once and for an hour. To try atrium alone on one machine
instead, use [`try-atrium.md`](try-atrium.md).

## The prompt

```text
You are setting this machine up as an atrium room, joined to a hub someone else runs, so its agents show on their
board. Do every step below in order, verify each one, and tell me plainly what worked and what did not. Do not skip
a step because it looks done. Check it.

WHAT ATRIUM IS
Atrium is one Go binary that organizes coding agents. `atrium run` is the hub: it serves the board. `atrium room` is
a room, one per machine: it holds the database, the terminals and the agents, and it dials the hub. This machine runs
a room only. The hub and its board are on the other machine. The room binds loopback only (127.0.0.1) and keeps its
state on disk under ~/.atrium.

STEP 0: READ FIRST
After cloning (step 3), read README.md, AGENTS.md, docs/how-atrium-works.md and docs/fabric/hub-and-rooms.md in the
repo. If anything in those files disagrees with this prompt, the repo wins. Tell me where they disagreed.

STEP 1: PREREQUISITES
- git.
- Go 1.26 or newer. If go.mod names a newer patch than yours, go downloads it by itself. Tell me if that fails. Do
  not edit go.mod.
- Ports 7777 and 7781 free on 127.0.0.1. Check with `netstat -ano | findstr 77` on Windows or
  `lsof -iTCP -sTCP:LISTEN | grep 77` on macOS/Linux. If something holds one, tell me what it is. Do not kill it.
  If it is already atrium, stop and tell me: this machine may already be a room or a hub.

STEP 2: THE JOIN STRING
Ask me for the join string. It starts with `atr1_` (or is an OpenZiti join URL). Ask me too whether the hub
operator said to use direct TLS, a private zrok share or OpenZiti. With direct TLS, the hub's link port (7779 by
default) must be reachable from this machine. Treat the string as a secret: do not write it to a file or echo it
back in full.

STEP 3: GET THE CODE
  git clone https://github.com/dovholuknf/atrium
  cd atrium
If the clone fails, stop and tell me.

STEP 4: BUILD AND INSTALL
  go build -o build.claude/ ./cmd/atrium
Build into build.claude/ only. Then copy the binary to a stable place and always run it from there:
- macOS/Linux:  mkdir -p ~/.atrium/bin && cp build.claude/atrium ~/.atrium/bin/
- Windows:      New-Item -ItemType Directory -Force $HOME\.atrium\bin
                Copy-Item build.claude\atrium.exe $HOME\.atrium\bin\
Run `~/.atrium/bin/atrium version` (or the .exe) and show me the output. A build from a clone prints a version like
`0.0.4+<sha>`.

STEP 5: JOIN
Do not run `atrium run` on this machine. That starts a hub and a room of its own, which is not what I asked for.
  ~/.atrium/bin/atrium room join --no-run <join string>
With no transport flag the join string decides. For a private zrok share add `--zrok-private <token>`, for OpenZiti
`--openziti <identity .json or jwt>`, and for a direct address the operator gave `--mtls <host:port>`. The hub names
the room. Report the name it printed. A string used once, or older than an hour, is refused: ask the operator for a
fresh one (`atrium rooms token <name>` on the hub).

STEP 6: START THE ROOM
  ~/.atrium/bin/atrium room --detach
It returns once the room answers. The PATH of the shell that starts the room becomes the room's PATH, and every
agent atrium launches gets it, so the agents I want to launch (claude, codex and so on) must be on it. Then check:
- the room's own board answers: curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:7781
- ask me to have the hub operator run `atrium rooms ls` and confirm this room shows as connected.
From now on `atrium room --detach` starts it. It needs no join string again.

STEP 7: WIRE THE AGENTS IN
Before you change anything, copy the agent's settings file aside (for Claude Code, ~/.claude/settings.json) and show
me the backup path. Then run `~/.atrium/bin/atrium hook install` for Claude Code, or
`~/.atrium/bin/atrium hook install --runner codex` for codex. `atrium hook status` lists what is wired. Show me the
diff against my backup. Sessions already running keep their old settings until restarted. If the room is not
running, the hooks fail open.
Optional, Claude Code only, and only if I say yes: set statusLine in ~/.claude/settings.json to a command entry whose
command is the installed binary's path followed by ` statusline`. It posts the session's context and usage to the
card. If I already have a statusLine, show it to me and ask before replacing it.

STEP 8: PROVE IT WORKS
Start a new agent session in a scratch directory (not the atrium repo) and ask it to list the files. Ask me to
confirm with the hub operator that a card for it appeared on their board, tagged with this room's name.

STEP 9: THINGS I MUST KNOW
- Stop the room with `atrium stop`. Never kill it with taskkill/kill: that can end every agent it holds at once.
- `atrium stop` and `atrium launch` take `--url` when the room is not on the default ports.
- The room binds loopback and dials out. Do not open any port on this machine for it.
- State lives in ~/.atrium (the room's key and certificate in ~/.atrium/room). Deleting it forgets the join.
- To leave the hub: `atrium stop`, restore the settings backup from step 7, delete ~/.atrium/bin, and ~/.atrium if
  I want my state gone. The operator removes the room on the hub with `atrium rooms mark <name>` then
  `atrium rooms rm <name>`.

STEP 10: REPORT
Give me a short list: each step, passed or failed, and for any failure the exact error and what you think caused it.
Then list anything in the docs that was wrong, unclear or out of date.

RULES FOR YOU WHILE DOING THIS
- Do not edit any file in the atrium repo.
- Do not change any setting outside atrium except the agent settings file in step 7, and only after the backup.
- Do not open any port beyond loopback.
- If a step fails, stop at that step and tell me. Do not improvise workarounds that touch my system.
```

## What the prompt was checked against

- `atrium room join` takes `--no-run`, `--mtls`, `--zrok-private` and `--openziti`, and `atrium room` takes
  `--detach`: `internal/cli/roomrun.go`.
- The room's ports 7777 and 7781: `internal/cli/atrium_defaults.go`.
- `atrium rooms add`, `ls`, `token`, `mark` and `rm`: `docs/fabric/hub-and-rooms.md`.
- `atrium hook install --runner claude|codex`: `internal/cli/hook.go`. `atrium hook uninstall`, and `atrium run`
  wiring claude's hooks on first start unless `--no-hooks`: `internal/cli/hook_uninstall.go`.
