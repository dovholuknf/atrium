# Join a hub

Someone already runs an atrium hub and you want this machine to be a room on it, with your own coding agent doing the
setup. Copy the block below, paste it into your agent (any agent that can run shell commands), and let it walk you
through. It installs atrium if the machine has none, joins with the string you were given, checks that the room is
attached, and checks each step before it reports back.

What you need first: an atrium binary or git and Go to build one, the join string the hub's operator gave you (it
starts with `atr1_`), and a network path to the hub's link port. If the join string is not in hand yet, ask the
operator to run `atrium rooms add <name>` on the hub's machine. The board cannot mint one.

The prompt is written to stay inside atrium's own rules. It builds into `build.claude/`, runs from `~/.atrium/bin`, and
uses `127.0.0.1` rather than `localhost`.

```text
You are joining this machine to an existing atrium hub as a room. Do every step below in order, verify each one, and
tell me plainly what worked and what did not. Do not skip a step because it looks done. Check it.

WHAT YOU ARE DOING
Atrium organizes coding agents. Two processes matter:
- The hub is `atrium run`. It serves the board and holds no work. Somebody else runs it, on another machine.
- A room is `atrium room`. It owns the database, the terminals and the agents on this machine. There is one room per
  machine, and it dials the hub, never the reverse. The hub names every room.
I was given a one-time join string that starts with `atr1_`. It carries the hub's address, a fingerprint of its
certificate authority and a secret. It is good once and for an hour. Treat it like a password. Do not print it into
any file, log or message other than the command that uses it.

STEP 0: ASK ME FOR WHAT YOU NEED
Ask me for:
- the join string,
- which transport it is for. Direct mutual TLS is the default and needs no flag. A zrok string needs the share's
  access token for `--zrok-private`. An OpenZiti string needs an identity file or a JWT for `--openziti`.
If I do not have a join string, stop. Tell me to ask the hub's operator to run `atrium rooms add <name>` on the hub's
machine and send me the line it prints. Nothing else on this machine can create one.

STEP 1: IS ATRIUM ALREADY HERE
Run `atrium version`. If it prints a version, commit and platform, atrium is installed and you can go to step 3. Then
run `atrium room --help` and check that the `join` subcommand is listed. If the command is missing or the version is
very old, go to step 2.
Also check whether a room already runs on this machine: http://127.0.0.1:7777 and http://127.0.0.1:7781 are where a
room listens by default, so see whether anything answers there. If one does, stop and tell me. A machine has one room,
and joining over it would replace what it was attached to. Do not stop it for me.

STEP 2: INSTALL ATRIUM (only if step 1 found none)
- git, and Go at the version the `go` line in go.mod names. Run `go version` and compare. If it is older, stop and
  tell me. Do not edit go.mod.
- git clone https://github.com/dovholuknf/atrium, then cd atrium. If the clone fails with "not found" or an auth error,
  stop and tell me. The repo may need an invite.
- go build -o build.claude/ ./cmd/atrium
  Build into build.claude/ only. Never build into the repo root. Report any compile error verbatim.
- Copy the binary to a stable place and always run it from there, never from build.claude/:
  - macOS or Linux:  mkdir -p ~/.atrium/bin && cp build.claude/atrium ~/.atrium/bin/
  - Windows:         New-Item -ItemType Directory -Force $HOME\.atrium\bin
                     Copy-Item build.claude\atrium.exe $HOME\.atrium\bin\
- Run ~/.atrium/bin/atrium version and show me the output. A version of `dev` is normal for a build from source.
Use the installed path for every command below if `atrium` is not on my PATH.

STEP 3: CHECK THE HUB CAN BE REACHED
The join string holds the hub's address but you cannot read it, so ask me for the hub's host and port if I know them.
The usual port is 7779. If I know them, test that the port answers from here, for example
`nc -vz <host> 7779` on macOS and Linux, or `Test-NetConnection <host> -Port 7779` on Windows.
If it does not answer, stop and tell me. Common causes are a hub that only binds loopback, a firewall, or a join
string minted with an address this machine cannot reach. The hub's operator fixes that with `--link` and
`--link-advertise`, then `atrium rooms token <name>` for a fresh string. Do not try to work around it.

STEP 4: JOIN
Run, with the string I gave you in place of the placeholder:
  atrium room join <the atr1_ string>
Add a transport flag only when the string is not a direct one:
  --zrok-private <access token>     a private zrok share
  --openziti <identity .json or jwt>  an OpenZiti network, with --service <name> if the service is not "atrium"
Give at most one of those. There is no public zrok option, on purpose.
What it does: it pins the hub's certificate authority, makes this room's own key (which never leaves this machine),
gets a certificate from the hub, saves both under ~/.atrium/room, and then runs the room in this terminal. The hub
decides the room's name. The command prints `joined over <transport> as "<name>"`. Show me that line.
Joining takes the room's name from the hub, so if the printed name is not the one I was told, stop and tell me.
If I want the join saved without running the room in this terminal, add `--no-run` and use step 6 to start it.
If it fails, report the error verbatim. An expired or already used string cannot be retried. I need a fresh one from the
operator via `atrium rooms token <name>`.
If this machine already runs a second room that must keep its own hooks, add `--isolated`. For one room per machine,
leave it off.

STEP 5: PROVE IT IS ATTACHED
- The room is a full atrium on its own. Check its own board answers: http://127.0.0.1:7781.
- Ask me to open the hub's board and confirm this room appears on the **rooms** tab and is not dimmed. If the hub's
  operator can run `atrium rooms ls` on the hub's machine, that lists every room and whether it is connected.
- Tell me the first time that agents on this room report through http://127.0.0.1:7777.

STEP 6: THE NEXT TIME
The join is saved. From now on `atrium room` with no arguments starts the room from what was saved. The room keeps
running in the terminal that started it. To run it in the background instead, use `atrium room --detach`, which logs to
room.log beside the room's directory and returns once it answers. Do not start the room at logon unless I ask.
If I want the hooks of my coding agent wired so its sessions report to the room, run `atrium hook install`
(`--runner codex` for codex, claude is the default) and show me the diff of the settings file it writes against a backup
you made first. It also keeps its own copy beside it, named settings.json.atrium-<time>.bak. That is optional.

STEP 7: THINGS I MUST KNOW
- Stop the room with `atrium stop`. Never kill it with taskkill or kill. Without the pty_host setting the room owns the
  agents' terminals, and killing it ends every running agent at once.
- The hub restarting is not a problem. The room notices the closed socket and dials again.
- Atrium has no login. The board is on loopback and the hub's operator controls who joins. Do not expose any port to a
  network.
- The room's key and certificate are in ~/.atrium/room. Do not copy that folder to another machine. Each machine joins
  with its own string.
- To leave: the hub's operator runs `atrium rooms mark <name>` and then `atrium rooms rm <name>`. Then I run
  `atrium stop` here. I delete ~/.atrium only if I want my state gone too.
- More: docs/fabric/hub-and-rooms.md in the repo, "Joining a room to a hub".

STEP 8: REPORT
Give me a short list: each step, passed or failed, and for any failure the exact error and what you think caused it.
Then list anything in README.md or the docs that was wrong, unclear or out of date, so I can send it back to the
author.

RULES FOR YOU WHILE DOING THIS
- Do not edit any file in the atrium repo.
- Do not change any setting outside atrium except the hook settings file in step 6, and only after the backup.
- Do not open any port beyond loopback.
- If a step fails, stop at that step and tell me. Do not improvise workarounds that touch my system.
```

## What the prompt was checked against

Every command and name in it was read against the code, so it is safe to hand over as it stands.

- `atrium room join <string>` takes one argument, the `atr1_` string, and the flags `--mtls`, `--zrok-private`,
  `--openziti`, `--service`, `--identity`, `--no-run`, `--isolated`, `--dir`, `--db`, `--http`, `--agent` and
  `--accept-upgrades`: `internal/cli/roomrun.go` and `internal/cli/roomjoinflags.go`. Only one transport flag is
  allowed. There is no `--zrok-public`.
- The name comes from the hub and is printed as `joined over <transport> as "<name>"`.
- The room's key and certificate go under `room/` in the state dir (`~/.atrium/room`),
  `internal/cli/atrium_defaults.go`.
- The default ports are 7777 for the room's agents, 7781 for its own board and 7779 for the hub's link, in the same
  file.
- `atrium rooms add`, `atrium rooms token`, `atrium rooms ls`, `atrium rooms mark` and `atrium rooms rm` exist and run
  on the hub's machine. `--link` and `--link-advertise` are on `atrium run`, `rooms add` and `rooms token`.
  `rooms add` refuses a wide `--link` with no `--link-advertise`.
- `atrium room --detach` logs to `room.log` beside `--dir` and returns once the room answers.
- `atrium hook install --runner claude|codex` keeps a backup, and `atrium hook uninstall` takes the atrium entries out
  again: `internal/cli/hook.go`, `internal/cli/hook_uninstall.go`.
- `atrium stop` reads the recorded board address and winds the room down: `internal/cli/stop.go`.
- The board menu is the **rooms** tab. The board cannot mint a join string.
- Not checked against a running hub: the hub was offline, so the prompt has not been walked end to end.
