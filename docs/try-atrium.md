# Try atrium

You want to see atrium work and you would like your own coding agent to do the setup. Copy the block below, paste it
into your agent (Claude Code, codex or any other that can run shell commands), and let it run. It builds atrium from
source, starts it on loopback, wires Claude Code's hooks in after backing up `settings.json`, and checks each step
before it reports back. It touches nothing else.

What you need first: git, Go at the version `go.mod` names, and four free loopback ports. Claude Code is optional but
it is the quickest way to see a card appear.

The prompt is written to stay inside atrium's own rules. It builds into `build.claude/`, runs from `~/.atrium/bin`,
and uses `127.0.0.1` rather than `localhost`.

```text
You are setting up atrium on this machine so I can try it. Do every step below in order, verify each one, and tell me
plainly what worked and what did not. Do not skip a step because it looks done. Check it.

WHAT ATRIUM IS
Atrium is one Go binary that organizes coding agents (Claude Code, codex, gemini, opencode, ollama, a plain shell).
Every agent session becomes a card on a web board. Every tool call an agent makes passes a permission gate on that
board. Agents can run in terminals atrium owns, which I can open in the browser, type into, or pop out. It binds
loopback only (127.0.0.1), has no cloud and no account, and keeps its state on disk so it survives a restart.

Two processes matter:
- `atrium run` is the hub. It serves the board on http://127.0.0.1:7778.
- `atrium room` is the room, one per machine. It holds the database, the terminals and the agents. `atrium run`
  starts this machine's room by itself the first time, in the background.
Stopping `atrium run` never stops the room or its agents.

STEP 0: READ FIRST
After cloning (step 2), read README.md, AGENTS.md and docs/how-atrium-works.md in the repo before doing anything
else. If anything in those files disagrees with this prompt, the repo wins. Tell me where they disagreed. (Some files
name docs/agents.md, which may not exist. If it is missing, say so and carry on.)

STEP 1: PREREQUISITES
- git.
- Go at the exact version go.mod names (1.26.x at the time of writing). Run `go version` and compare with the `go`
  line in go.mod. If it is older, stop and tell me. Do not edit go.mod.
- Claude Code installed and logged in (`claude --version`), if I want Claude sessions on the board.
- Ports 7777, 7778, 7779 and 7781 free on 127.0.0.1. Check with `netstat -ano | findstr 777` on Windows or
  `lsof -iTCP -sTCP:LISTEN | grep 777` on macOS/Linux. If something holds one, tell me what it is. Do not kill it.

STEP 2: GET THE CODE
  git clone https://github.com/dovholuknf/atrium
  cd atrium
If the clone fails with "not found" or an auth error, the repo may need an invite. Stop and tell me.

STEP 3: BUILD
  go build -o build.claude/ ./cmd/atrium
Build into build.claude/ only. Never build into the repo root. Report any compile error verbatim.

STEP 4: INSTALL THE BINARY
Copy it to a stable place and always run it from there, never from build.claude/. Hooks and the self-restart record
the binary's path, and every build rewrites build.claude/.
- macOS/Linux:  mkdir -p ~/.atrium/bin && cp build.claude/atrium ~/.atrium/bin/
- Windows:      New-Item -ItemType Directory -Force $HOME\.atrium\bin
                Copy-Item build.claude\atrium.exe $HOME\.atrium\bin\
Then run `~/.atrium/bin/atrium version` (or the .exe) and show me the output. It prints the version, commit, board
hash, platform and Go version. A version of `dev` is normal for a build from source.

STEP 5: START IT
  ~/.atrium/bin/atrium run
Leave it running in its own terminal (or run it in the background and tell me how you did it). Wait until it says it
is listening, then check:
- http://127.0.0.1:7778 answers (curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:7778).
- `~/.atrium/bin/atrium --help` lists the subcommands.
Use 127.0.0.1, not localhost. On some machines localhost costs a 2 second IPv6 timeout per request.
Tell me to open http://127.0.0.1:7778 in my browser.

STEP 6: WIRE CLAUDE CODE IN
Run this after step 5, because the command writes the path of the running atrium into the hooks. Before you run it,
copy ~/.claude/settings.json aside (show me the backup path) so I can undo it. Then run
`~/.atrium/bin/atrium hook install`, or tell me to use the board: the **rooms** tab, the claude row under runners,
its **hooks** chip, then **wire it** on each row. Both write the same entries. `atrium hook status` lists which hooks
are wired. The command also keeps its own copy of the old file beside it, named settings.json.atrium-<time>.bak,
and prints the path. After it runs, show me the diff of settings.json against my backup.
From then on every new Claude Code session on this machine reports to the board, and every tool call it makes asks
the gate. Sessions already running keep their old settings until restarted. If atrium is not running, the hooks fail
open: Claude keeps working as if atrium were not there.
Then tell me to go to the **perms** tab and press **import rules from claude**. That turns the allow and deny lists I
already have into standing rules, so the gate does not ask me about things I already trust.

STEP 7: PROVE IT WORKS
- Start a new Claude Code session in some scratch directory (not the atrium repo) and ask it to list the files.
  Confirm a card appears on the board for it, and that its tool call either matched a rule or showed up as a
  permission request on the board.
- Launch one agent under atrium so atrium owns its terminal: either the board's **+ new agent** button or
  `~/.atrium/bin/atrium launch --cwd <scratch dir>` (`atrium launch --help` for the flags, the runner defaults to
  claude). Confirm I can open its terminal in the browser and type into it.
Report what I should see at each point.

STEP 8: THINGS I MUST KNOW
- Stop atrium with `atrium stop`. Never kill it with taskkill/kill: without the pty_host setting the room owns the
  agents' terminals, and killing it ends every running agent at once. `atrium stop` winds the room down and gives
  agents time to finish. The hub holds no work, so ctrl-c in its terminal is fine.
- The board has no login. It binds loopback on purpose. Do not expose port 7778 to a network. If I want it from my
  phone or another machine, docs/fabric/overlays.md covers zrok and OpenZiti.
- A second machine joins as a room: on this machine `atrium rooms add <name>` prints a one-time join string, good
  once and for an hour. On the other machine run `atrium room join <string>` the first time and `atrium room` after.
  See docs/fabric/hub-and-rooms.md.
- State lives in ~/.atrium (the room's database, atrium.db, is there). Deleting it resets everything.
- Atrium does not start at logon unless I ask it to. `atrium run` installs no logon task, LaunchAgent or user unit.
  If I want one later, scripts/atrium-service.ps1 (Windows) and scripts/atrium-service.sh (macOS and Linux) take
  install, uninstall, start, stop, restart and status. Do not run them now.
- To undo step 6: there is no `atrium hook uninstall`. Copy the backup of settings.json from step 6 back over
  ~/.claude/settings.json (this also drops anything else I changed in that file since), or delete the hook entries
  whose command names the atrium binary by hand. To remove the rest: `atrium stop`, then delete ~/.atrium/bin, and
  ~/.atrium if I want my state gone too.

STEP 9: REPORT
Give me a short list: each step, passed or failed, and for any failure the exact error and what you think caused it.
Then list anything in README.md or the docs that was wrong, unclear or out of date, so I can send it back to the
author.

RULES FOR YOU WHILE DOING THIS
- Do not edit any file in the atrium repo.
- Do not change any setting outside atrium except Claude Code's settings.json in step 6, and only after the backup.
- Do not open any port beyond loopback.
- If a step fails, stop at that step and tell me. Do not improvise workarounds that touch my system.
```

## What the prompt was checked against

Every command and name in it was read against the code, so it is safe to hand over as it stands.

- Ports 7777, 7778, 7779 and 7781: `internal/cli/atrium_defaults.go`.
- `atrium version`, `atrium stop`, `atrium launch`, `atrium rooms add`, `atrium room join`, `atrium hook install` and
  `atrium hook status` all exist. `atrium launch` defaults to the claude runner and takes `--cwd`.
- `atrium hook install` has no undo. It keeps a `settings.json.atrium-<time>.bak` copy and that is the way back.
- `atrium run` installs no logon task. The scripts in `scripts/` do, and each has an `uninstall`.
- The board menu is **rooms** (the tab), the claude row under runners, its **hooks** chip, and **perms**, then
  **import rules from claude**. The launch button is **+ new agent**.
- `docs/agents.md` is named by `README.md` and `AGENTS.md` but is not in the tree, so the prompt reads `AGENTS.md` and
  `docs/how-atrium-works.md` instead.
