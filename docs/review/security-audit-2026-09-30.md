# Security audit, 2026-09-30

The @review half of `docs/backlog/rnd/rd-new-security-review.md`. Every listener, every route family, who reaches
it, what identifies the caller and what it can change. @rnd designs the fix from this.

Audited at `claude/main` 7b069c66 and later (f82f3284). Live probes ran against the listeners on sg4 at 15:49 to
15:52 local, read-only: GETs, a dry-run import, a say to a handle that does not exist, and a websocket handshake
against the auditor's own card that sent no frames. Nothing was launched, exited, approved, patched or deleted.

## The short version

The board API has no caller check of any kind, and it takes cross-site requests. A web page open in any browser on
sg4 can start a shell on this machine without the operator clicking anything. That is the first finding and the
cheapest to close. Go 1.26 ships `http.CrossOriginProtection`, and the MCP SDK atrium already uses turns it on for
`/_hub/mcp`, which the probes show refusing exactly the requests the board accepts.

After that the problem is the one the brief names: every local process, and every agent, is the operator. Loopback
is the only credential, and a browser, an agent's `curl` and a share all arrive on loopback.

## The listeners

Observed with `Get-NetTCPConnection -State Listen` and each process's command line.

| Port | Bind | Process | What it serves | TLS | Caller identity |
| --- | --- | --- | --- | --- | --- |
| 7778 | 127.0.0.1 | hub, `atrium run --no-room` (pid 26060) | hub board: `/_hub/*`, and every room's board API by proxy | no | none |
| 7779 | `::`, every interface | hub (pid 26060) | room link: enrol, control, data | TLS 1.3, client cert | room certificate, CN is the room name |
| 7781 | 127.0.0.1 | room `claude-sg4` (pid 37540) | room board API, `internal/api` | no | none |
| 7777 | 127.0.0.1 | room `claude-sg4` (pid 37540) | agent listener: hooks, `/tell`, `/finish` | no | a name in the body |
| 7791 | 127.0.0.1 | room `sg4-control` (pid 20036) | room board API | no | none |
| 7787 | 127.0.0.1 | room `sg4-control` (pid 20036) | agent listener | no | a name in the body |

The room board handler is also served on every data connection a room dials to the hub
(`internal/link/room.go:30`), so the hub's 7778 reaches the full API of every attached room, remote rooms included.

## Findings, by severity

### C1. Critical. Any web page can run a command on the machine (cross-site request forgery)

**Where.** `internal/api/api.go:372-671` builds the board mux with no wrapper, and `internal/daemon/daemon.go:1038`
serves it as is. No handler checks `Origin`, `Sec-Fetch-Site`, `Host` or the request's `Content-Type`. The JSON
decoders read a `text/plain` body the same as `application/json`. The hub proxy forwards the same request to a room
unchanged (`internal/link/proxy.go:443-617`).

A browser sends a cross-site POST with a `text/plain` body without a CORS preflight. The page cannot read the
answer, and it does not need to. Three POSTs need nothing the page cannot guess:

- `POST /v1/launch` (`api.go:555`). The body takes `harness`, `cwd`, `prompt`, and `args` and `env` that are "used
  as given" and "not checked against a list" (`internal/daemon/launch.go:66-71`, appended at `launch.go:301-302`).
  This room has a harness with id `shell` whose command is `pwsh.exe`. The ids are short words.
- `POST /v1/config/import?apply=1&force=1` (`api.go:433`, `internal/api/export.go:57-74`). Writes harnesses,
  fixtures, sources and rules (`internal/daemon/import.go:122-160`). A source is a command on a timer
  (`internal/store/sources.go:23-35`), and the source loop runs it on its next tick.
- `POST /v1/say` (`api.go:618`, `internal/daemon/relay.go:182-215`). Types text into the terminal of the session
  with that handle, with a `from` the body chooses. Handles are words like `orchestrator`.

Board-wide auto mode is on in `claude-sg4` right now (`global_auto` is `true` in `GET /v1/settings`), so a prompt
typed into a session is also a tool call nobody is asked about.

**Proof, run.** A cross-site dry-run import is decoded and answered:

```
curl -s -H "Origin: https://attacker.example" -H "Content-Type: text/plain" \
  --data '{"version":1}' "http://127.0.0.1:7778/v1/config/import?atrium_room=claude-sg4"
# 200 {"applied":false,"changes":null,...}
```

A cross-site say to a handle that does not exist is resolved against the peer list and answered with it (empty
`from` and `text`, so nothing is recorded, see `internal/daemon/saylog.go:173`):

```
curl -s -H "Origin: https://attacker.example" -H "Content-Type: text/plain" \
  --data '{"to":"no-such-handle-audit"}' "http://127.0.0.1:7778/v1/say?atrium_room=claude-sg4"
# 404 {"error":"no session called no-such-handle-audit...","peers":[12 entries]}
```

**Proof, not run.** The same delivery with a launch body:

```
# DO NOT RUN. Opens pwsh on the room with an attacker's arguments.
curl -s -H "Origin: https://attacker.example" -H "Content-Type: text/plain" \
  --data '{"harness":"shell","cwd":"C:/Users/claude","args":["-c","<command>"]}' \
  "http://127.0.0.1:7778/v1/launch?atrium_room=claude-sg4"
```

In a page this is `fetch(url, {method: "POST", mode: "no-cors", body})`. Recent Chromium builds put a permission
prompt in front of a public page reaching loopback. Firefox does not. Atrium cannot rely on either.

### C2. Critical. Any web page can attach to a terminal (cross-site websocket hijacking)

**Where.** `internal/daemon/attach.go:256-260` accepts the websocket with `InsecureSkipVerify: true`, which in
`coder/websocket` turns off the Origin check. The comment says "Loopback only, and the board is served from the same
origin", and the flag means the opposite. `internal/daemon/park.go:195` does the same for a parked card. Websockets
are not covered by CORS, so the page reads the scrollback and writes keystrokes. `?kind=shell` attaches to the card's
plain shell (`attach.go:225`).

The only barrier is the card id, a UUIDv7 with 74 random bits. It is not guessable. It leaks through H1 below,
through the audit feed, and through anything an agent writes to a file.

**Proof, run** on the auditor's own card, handshake only, no frames sent:

```
curl -s --http1.1 -m 2 -D - -o NUL -H "Origin: https://attacker.example" \
  -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  http://127.0.0.1:7781/v1/tasks/01a0f281-51d2-717e-9803-fa9f1b7bdd5c/attach
# HTTP/1.1 101 Switching Protocols   (the same through the hub on 7778)
```

### H1. High. Any web page can read the whole board (DNS rebinding)

**Where.** No handler on 7778, 7781 or 7791 checks `Host`. A page on a name the attacker controls, rebound to
127.0.0.1, becomes same-origin with the board and reads every GET: cards, scrollback text, card files, the browse
listing, rules, the audit feed, `/v1/auth`. With ids in hand it drives C2 and every id-scoped POST, and PUT, PATCH
and DELETE too, since a same-origin page needs no preflight. That includes `PUT /v1/auth`, which turns off the login
on the published board.

**Proof, run:**

```
curl -s -o NUL -w "%{http_code} %{size_download}\n" -H "Host: attacker.example:7778" http://127.0.0.1:7778/v1/tasks
# 200 396358
curl -s -o NUL -w "%{http_code} %{size_download}\n" -H "Host: attacker.example:7781" http://127.0.0.1:7781/v1/tasks
# 200 315758
```

The same request to `/_hub/mcp` is refused, see "What holds".

### H2. High. Every local process and every agent is the operator on the board

**Where.** The same unwrapped mux (`api.go:372-671`) and the hub proxy that fans it to every room. The routes the
brief names, with no caller check:

| Route | Line | What it changes |
| --- | --- | --- |
| `POST /v1/launch` | `api.go:555` | starts a runner with any harness, directory, args and env |
| `POST /v1/tasks/{id}/exit`, `/kill`, `/cull` | `api.go:556-561` | ends a session |
| `POST /v1/permissions/{id}/decide` | `api.go:540` | approves or denies a pending tool call |
| `POST /v1/rules`, `/v1/rules/import` | `api.go:543`, `546` | standing approvals |
| `POST /v1/settings` | `api.go:377` | includes `global_auto`, `shell_command`, `editor_command`, `terminal_command` |
| `PUT /v1/harnesses/{id}`, `PUT /v1/sources/{id}`, `POST /v1/fixtures` | `api.go:552`, `507`, `380` | commands atrium runs |
| `POST /v1/tasks/{id}/files`, `PUT .../files/text` | `api.go:484`, `505` | writes into a card's directory |
| `GET /v1/tasks/{id}/files`, `/scrollback/text`, `/v1/browse` | `api.go:485`, `594`, `529` | reads |
| `PUT /v1/auth` | `api.go:431` | the published board's login |
| `POST /v1/overlays/{kind}/start` | `api.go:389` | publishes the board |
| `POST /v1/tasks/{id}/message`, `/v1/say` | `api.go:615`, `618` | types into a terminal |

Through the hub, the same calls reach remote rooms by name (`?atrium_room=` or `X-Atrium-Room`,
`internal/link/proxy.go:248-274`).

The code already knows. `internal/link/deps.go:32-36`: "an agent can curl loopback without the header, the way it
could curl a permission decision. What stops that is the permission gate seeing the command". With `global_auto` on
the gate approves that `curl` too. A worker can approve a director's pending request, add an allow-everything rule,
or launch a card with `--dangerously-skip-permissions` in `args`. On a machine with more than one Windows account,
every account reaches these ports.

**Proof, run:** every GET in the table above answered 200 with no credential. Two samples:

```
curl -s -o NUL -w "%{http_code}\n" http://127.0.0.1:7781/v1/rules        # 200
curl -s -o NUL -w "%{http_code}\n" http://127.0.0.1:7778/_hub/audit      # 200, 89 KB
```

**Proof, not run:**

```
# DO NOT RUN. Approves a pending tool call that belongs to another session.
curl -s -X POST -d '{"decision":"approve"}' http://127.0.0.1:7781/v1/permissions/<id>/decide
```

### H3. High. The agent listener believes whatever name it is given

**Where.** `internal/daemon/daemon.go:1013-1037`. `/tell` takes `from` from the body (`internal/daemon/peers.go:382`)
and types the text into the target's terminal when it is free (`peers.go:394-419`). `/finish`, `/help`, `/answer`,
`/session` and `/permission` take the agent's name the same way. Any local process can speak to any session as any
other session, declare another session's work over, or put a fake permission request on the board under a real
card's name for a human to approve. The CLI's `atrium tell` is the intended client and nothing distinguishes it from
`curl`. A browser reaches this port too, and the body is decoded as `text/plain`.

**Proof, run:** the body is decoded from a `text/plain` POST and refused only for being empty:

```
curl -s -H "Content-Type: text/plain" --data '{"to":"no-such-handle-audit"}' http://127.0.0.1:7777/tell
# 400 {"error":"there is nothing to say"}
```

**Proof, not run:**

```
# DO NOT RUN. Types into the orchestrator as if clint said it.
curl -s --data '{"from":"clint","to":"orchestrator","text":"<instruction>"}' http://127.0.0.1:7777/tell
```

### M1. Medium. "Loopback only" does not stop a browser

**Where.** The routes guarded by the caller's address: `/v1/shutdown` (`internal/daemon/shutdown.go:52-85`),
`POST /_hub/restart` (`internal/link/restartgate.go:511`), `/_hub/launch-caps` (`launchcaps.go:127`), deps writes
(`deps.go:557`), `/_hub/notify` (`notifyapi.go:62`), `/_hub/deploy-owner` (`deploy_mcp.go:431`), `/_hub/git/*`
(`git_hub.go:62`), `/_hub/nudge` (`proxy.go:1322`). A browser on the machine is loopback, so each passes C1. The
guard is right against a share and wrong against a web page. A cross-site `POST /v1/shutdown` to a room's own port
stops that room, and a cross-site `POST /_hub/restart` restarts the hub. `restartgate.go:525` ignores a body that
does not decode, so any body works.

**Proof, not run:**

```
# DO NOT RUN. Stops the claude-sg4 room.
curl -s -X POST -H "Origin: https://attacker.example" -H "Content-Type: text/plain" \
  --data x http://127.0.0.1:7781/v1/shutdown
```

### M2. Medium. The link listener is on every interface, and the firewall lets far more in than atrium needs

**Where.** The hub runs `--link 0.0.0.0:7779`, bound as `::`. Windows Firewall has inbound Allow rules on the
Public profile for the live binary (`C:\users\claude\.atrium\bin\atrium.exe`), every port, and for nine development
builds under `D:\git\github\dovholuknf\atrium\build.claude\` and two worktrees. Both network profiles on sg4 are
Public, including `ziti-tun0`. So 7779 is reachable from any network the machine joins and from the ziti tunnel, and
any dev build that binds wide is reachable too.

The link itself holds (see below). What faces the network is the enrolment path, which reads a JSON frame from an
unauthenticated peer under a handshake deadline (`internal/link/direct.go:174-181`), and whatever a dev build
happens to bind.

**Proof, run:**

```
Get-NetFirewallApplicationFilter | ? Program -match 'atrium' | Get-NetFirewallRule |
  select DisplayName, Action, Profile
# 20 rules, all Allow, Inbound, Public
```

### M3. Medium. A room certificate lasts ten years and cannot be revoked

**Where.** `internal/link/certs.go:54`, `caLife = 10 * 365 * 24 * time.Hour`, used for the CA, the hub leaf and every
room leaf (`certs.go:95`, `131`, `479`). There is no revocation list and no check against the inventory on attach.
The key pin at `internal/link/hub.go:375` refuses a second key only while a room with that name is attached. "Forget"
removes the record and a room that dials again "is written down afresh" (`proxy.go:994-999`). A copied
`room.key` from any machine that ever joined attaches as that room whenever the real one is offline, and then
receives every proxied board request for that name.

`certs.go:504`, `521`, `560` write keys with `0o600`, which Go on Windows does not enforce. The profile directory's
ACL is what protects `ca.key`.

### M4. Medium. The hub is trusted completely by every room

**Where.** A room serves its full board handler to whatever arrives on a data connection (`internal/link/room.go:30`).
The hub proxy refuses exactly two things on the way through: `/v1/shutdown` (`proxy.go:466`) and `/v1/git`
(`proxy.go:477`). So whoever reaches 7778 on sg4, which is H2's "any local process", drives every room on every
machine. The offered upgrade is checked against a hash the same hub supplies (`internal/link/upgrade.go:23-42`),
which is integrity and not authenticity. It is opt-in with `--accept-upgrades`, and none of the three local
processes were started with it.

### L1. Low. A process squatting the agent port answers every hook

**Where.** The permission hook fails open when atrium does not answer (`internal/cli/hook_permission.go:23`, `172`),
which is resilience rule 2. A process that binds 7777 while the room is down is not "down". It receives every
command every session tries to run, and it can answer `block` with text, which the chain frames as the human
talking (`CLAUDE.md`, permission chain step 2). Needs the room to be down and a second local account or a hostile
process.

### L2. Low. A join token sits in a process command line

The `sg4-control` room runs `atrium.exe room join atr1_eyJ0...` with the join string on its command line, readable
by any local account through `Win32_Process`. This one is spent, so it is worth nothing now. The pattern leaks a live
one if a room is ever started from an unspent token that way.

### L3. Low. `X-Atrium-Agent` is a claim

`internal/link/ctlclass.go:14-21` and `ctlaudit.go:19` say so. It decides which control tools a caller sees and how
the audit line reads. It is not a boundary and does not pretend to be. Listed so @rnd's caller identity replaces it
rather than sitting beside it.

### L4. Low. Hub inventory writes have no loopback check

`/_hub/inventory/mark` and `/_hub/inventory/forget` (`proxy.go:1353-1356`) take a POST from any caller of the hub
board. On loopback that is H2. Over a hub board share it is anybody holding the share. Marking starts nothing and is
reversible, and forgetting refuses an attached room, so the damage is a room hidden from the picker.

## What holds

Each of these was read, and where marked, probed.

- **The room link.** TLS 1.3 only (`direct.go:118`, a TLS 1.2 hello gets `protocol version`, probed). Client
  certificate asked for and required for control and data before a name is read (`hub.go:253-258`). The room name
  comes from the certificate, never the frame (`hub.go:280`). Enrolment pins the CA fingerprint from the join string
  and verifies the leaf against it (`direct.go:310-348`). The secret is single use and says which room it is
  (`direct.go:187-195`).
- **`/_hub/mcp`.** Loopback only (`proxy.go:1297`), and the MCP SDK's DNS-rebinding and cross-origin protection are
  on by default. Probed: a rebound `Host` gets `403 invalid Host header`, a cross-site `text/plain` POST gets `403
  cross-origin request detected`. This is the fix C1 and H1 want, already in the binary.
- **pprof.** Loopback listener, loopback caller and a loopback `Host` (`internal/daemon/pprof.go:63-90`).
- **Two routes the hub will not proxy.** `/v1/shutdown` and `/v1/git` answer 403 through the hub (`proxy.go:466`,
  `477`, probed). Git is mounted only on the link handler, so a room's loopback board answers 404 (probed).
- **The published board.** A login in front of the overlay listener and nothing else (`internal/daemon/auth.go:40-50`),
  basic enabled with a password set on `claude-sg4`. A public zrok hub share is refused without a login
  (`internal/cli/atrium_share.go:86-90`). The share password is never read back (`internal/link/fanout.go:827`).
- **A lent session.** An allowlist, not a denylist (`internal/daemon/overlay_guest.go:31`).
- **Files.** Download resolves through `internal/safepath` (`safepath.go:48`), which follows symlinks on both sides.
  Upload takes no path. Outside a card is 403 whether it exists or not.
- **Browse.** Bounded by `browse_roots` (`internal/api/browseroots.go`).
- **Hooks install.** The target is one of a fixed list (`internal/claudeconf/target.go:146-153`) and the binary is
  chosen by the daemon, so the body cannot point either anywhere.
- **Config export.** Refuses to write anything that looks like a secret (probed, 400).

## Where the fix starts

For @rnd, ranked by what each closes. Sizes are guesses for the design to correct.

1. **Refuse cross-site and rebound requests on every browser-facing handler.** Wrap the room's `BoardHandler` and the
   hub `Proxy` in `http.CrossOriginProtection` and a `Host` allowlist (loopback names plus the configured share
   hosts), and replace `InsecureSkipVerify` with `OriginPatterns` on both websocket accepts. `curl` and the CLI send
   no `Origin` or `Sec-Fetch-Site`, so they are unaffected, and the board is same-origin. Closes C1, C2, H1 and M1
   for browsers. Small, and it can ship before any identity design.
2. **A caller identity on every mutating route**, which is the brief's design question. Closes H2, H3 and L3. What the
   hooks send has to stay fail-open, so the design has to say what an unidentified hook call may still do.
3. **Revocation and a shorter room life.** Refuse a certificate whose room is not in the inventory, and make forget
   mean refuse. Closes M3.
4. **Bind the link to the addresses rooms use, and trim the firewall rules** to the installed binary on the ports it
   needs. Closes M2. Mostly operations.
5. **Say what a room refuses from its hub**, beyond shutdown and git. M4.
