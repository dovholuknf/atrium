# Security design: close the browser edge first, then tell the board apart from curl

Status: design, 2026-09-30, @rnd, revised the same day for codex's review (mercurius `s_dprc3T42546v` round 1, C1 to
C4 and A1, A2). Item `docs/backlog/rnd/rd-new-security-review.md`, PRIORITY. Built from @review's audit,
`docs/review/security-audit-2026-09-30.md` (16058425). Finding ids (C1, H2, M3 ...) are the audit's. Review ids are
written `codex C1` to keep the two apart. Built by @runtime, with @ui for the board's sign-in page and the phone.

Stage 0 is being built now by @runtime (audit fix item 1). Nothing else here is built.

## 1. The answer in ten lines

1. **Stage 0 closes the browser edge**: `http.CrossOriginProtection`, a loopback `Host` check, and the Origin check put
   back on both websocket accepts. It closes C1, C2, H1 and M1 for browsers. In flight at @runtime.
2. **Stage 1 makes a refused hook behave like an unreachable one.** Before anything can refuse a hook, the hook client
   treats every non-2xx answer as atrium being away, which it already does for `/gate`. Then no later stage can brick a
   session by mistake.
3. **Stage 2 tells the board apart from curl, and is not an auth layer** (section 3). Atrium mints its own local
   capability tokens. It has no users, no passwords and no login on loopback, and it holds nobody else's credential.
4. **Four scopes**: `operator` (the board's cookie, or the operator token), `card` (one session, from a token atrium
   put in that runner's environment), `guest` (a lent session, unchanged), and `none`. Every route has one line in the
   table in the appendix, and a route not in it is `operator`.
5. **An agent is a card, never the operator.** A card may finish, report, say as itself, and launch and exit its own
   workers. It may not approve a permission, write a rule, change a setting, or edit anything atrium runs.
6. **Hooks stay open and capability-limited** (section 5). A hook with no token still posts activity and still asks
   for permission. It cannot speak as another session or end another session's work.
7. **Stage 2 ships in `warn`**, which refuses nothing and counts what `enforce` would refuse. The switch is clint's.
8. **The hard limit**: agents run as clint's own OS user, so no local token is a sandbox. Stage 3 makes the plain reads
   of atrium's credential files a refusal that no rule and no auto mode can approve.
9. **Then inventory enforcement (M3), the link bind and firewall end state (M2), and scope on hub traffic (M4).**
10. **Web Push is possible on a zrok public share** (already HTTPS) once a phone can subscribe as the operator. Not over
    an OpenZiti intercept, which the browser sees as plain http.

## 2. The threats, and which stage answers each

| threat | findings | stage |
| --- | --- | --- |
| a web page in any browser on the machine | C1, C2, H1, M1 | 0 |
| another OS account on the machine | H2 (multi-account), key files | 2 (ACL'd token files), 4 |
| a local program that is not atrium, curl included | H2, H3, L4 | 2 |
| one agent acting as another agent, or as clint | H3, L3 | 2 (card scope) |
| an agent approving its own or another's tool calls | H2 (`decide`, rules, `global_auto`) | 2, 3 |
| a hostile agent as the same OS user | beyond the audit | 3, and the permission gate. Not closed, section 7 |
| a forgotten room's copied key | M3 | 4 |
| the network reaching the link, dev builds binding wide | M2 | 5 |
| the hub driving every room | M4 | 6 |
| a squatter on the agent port, a join token on a command line | L1, L2 | 7 |

**What is out of scope.** Mode A (`atrium hub`, `atrium agent`) and Mode B (`atrium serve`) are untouched. They are
interactive tools run by hand, and neither is part of the daemon's listeners the audit covered.

## 3. Why stage 2 is not an auth layer

`CLAUDE.md` puts authentication out of scope: single machine, loopback, and reaching the board from elsewhere is an
overlay's job. It names one exception, `--shutdown-token`, "a kill switch reachable from anywhere is worth ruling out by
accident". The published board's OIDC guard (`internal/daemon/auth.go`) is the second, and its header draws the line
this design keeps: atrium still owns no credentials, has no user table and no password, and holds nobody else's secret.

Stage 2 stays on that side of the line:

- **No identity.** A token says "this caller is the board" or "this caller is card X". It never says who a person is.
- **No login on loopback.** The board gets its cookie from a one-time code that the local `atrium` binary mints and
  opens in the browser. That is the same act as clicking a link atrium printed, not a sign-in.
- **Nothing held that belongs to anyone else.** Every token is minted by atrium, lives on this machine, and is worth
  nothing anywhere else.
- **It exists because clint asked for it**: "regular 'curl' should prolly be 'bad'" (the backlog item). Stage 0 alone
  does not deliver that, because it stops browsers and nothing else. Stage 2 is the smallest mechanism that tells the
  board apart from a program on the same machine.

If stage 2 is not wanted, stages 0, 1 and 4 to 7 stand on their own and close everything but H2, H3, L3 and L4.

## 4. Stage 0: the browser edge (in flight at @runtime)

| listener | `Host` check | `CrossOriginProtection` | websocket Origin |
| --- | --- | --- | --- |
| room human (7781, 7791), loopback | loopback names only | yes | checked |
| room agent (7777, 7787), loopback | loopback names only | yes | none served |
| hub board (7778), loopback | loopback names only | yes | checked |
| published board, lent session, hub share (overlay listeners) | none: the share names the host | yes | checked |
| a room's link data handler (what the hub sends a room) | none | none | none |
| pprof | already checks all three | | |

**Loopback names** are `127.0.0.1`, `localhost` and `[::1]`, with any port. A share is its own `net.Listener` and its
own `http.Server`, so the check wraps the loopback listener's handler and no share host list is needed.

**`CrossOriginProtection`** refuses a non-safe method whose `Sec-Fetch-Site` is not `same-origin` or `none`, and falls
back to comparing `Origin` with `Host`. A request carrying neither header passes, which is every hook, the CLI, the MCP
server and curl.

**Websockets.** `attach.go:256-260` and `park.go:195` drop `InsecureSkipVerify`, so `coder/websocket`'s default
refuses an `Origin` whose host is not the request's `Host`.

**The link data handler is exempt, and the reason is the proxy.** The hub forwards a board request to a room with the
browser's `Origin` and its own `Host`, so a room checking those would refuse every attach through the hub. The hub
checks at its edge. A room tells the two paths apart by the listener the request arrived on, never by a header.

**Acceptance test.** Every audit probe, "run" and "not run" alike, sent to each listener in the table with a
cross-site `Origin`, with a rebound `Host`, and as a websocket upgrade from a foreign origin, answers 403. The same
requests with no `Origin` and a loopback `Host` answer as before. An attach through the hub from the board still opens.
A Go test asserts the hook client's requests carry no `Origin` and no `Sec-Fetch-Site`.

## 5. Stage 1: a refused hook is an unreachable hook

**codex C3 asks for one of two behaviors, and this design takes both, in this order.**

1. **The hook client treats every non-2xx answer exactly as it treats no answer.** `hook_permission.go` already does
   this for `/gate` (a non-200 is "fail open"), and returning nothing makes Claude Code run its own permission flow,
   which asks the human in the runner's own terminal. Stage 1 makes the same true of every hook call: `/permission`,
   `/session`, `/activity`, `/stop`, `/telemetry`, `/hooks-changed`, and `atrium finish`. Nothing is retried, as rule
   3 of the daemon's resilience guarantees says.
2. **The hook routes never refuse for want of a token** (stage 2). They stay open, and what an unverified call can do is
   limited instead:

| hook route | with a valid card token | with none, or a wrong one |
| --- | --- | --- |
| `/activity`, `/telemetry` | recorded | recorded. A badge is harmless, and `/activity` answers before it works |
| `/session` start and end | recorded | recorded only when the pid check passes (`ownsSession`, `daemon.go:752`) |
| `/gate`, `/permission` | asked as that card | asked, marked "caller not verified" on the board. Approving it only unblocks the process that asked, which could have run the command without asking. The mark stops a human believing it came from the card |
| `/stop` (the turn-end hook) | recorded | recorded only when the pid check passes |
| `/hooks-changed` | recorded | recorded. It asks the room to re-read its own settings file |
| `/finish` | recorded | `200 ok`, nothing recorded, logged. Rule 7 as written |

So a refusal can reach a hook only through a bug, and a bug there falls back to the runner's own prompt. A session
never fails to start, finish a turn or make a tool call because of any stage here.

**Acceptance test.** With a stub listener answering 401, 403, 500 and a malformed body in turn, each hook exits 0,
prints nothing, and a permission falls to Claude Code's own prompt. A test run of a session with the agent listener
answering 403 to everything starts, runs a gated tool call through the native prompt, and ends.

## 6. Stage 2: the scopes

### Presenting a credential

- **The board.** A browser with no cookie gets a one-page sign-in: "open this board with `atrium board`". `atrium board`
  (new: `atrium open` is taken, it hands atrium a URL to recognise) mints a one-time code good for 60 seconds, opens
  `http://127.0.0.1:<port>/?code=<code>`, and the board trades it for the cookie and takes the code off the address.
  The mint is `POST /v1/board-code`, operator only, so `atrium board` presents the operator token. The trade is
  `POST /v1/board-code/redeem` with the code in the body, open to `all`, since the caller has nothing but the code. A
  code is good once, for 60 seconds, on the listener that minted it. The hub answers both itself for its own board
  and never proxies them. The cookie is `HttpOnly`, `SameSite=Strict`, scoped to the listener that set it, lasts 30
  days and renews on use. The published board's OIDC or basic login sets the same operator cookie. The phone signs in
  on the share it uses.
- **The operator token** is `%LOCALAPPDATA%\atrium\operator.token`, created with an explicit DACL naming only the
  current user. Go's `0o600` does nothing on Windows, so the DACL is set through the Windows API, and a start that finds
  a wider ACL narrows it and logs it. Atrium's own processes (a room calling its hub's `/_hub/nudge`, say) present it.
  `atrium` run from a terminal that is not a card presents it.
- **A card's token** is minted by the room at launch and set as `ATRIUM_CARD_TOKEN` in the runner's environment. It is
  one variable set for every runner alike, not a filter on some runners' environment, so the uniform-environment rule
  holds. A session that joined with `atrium join` has no such variable, so `join` writes its token to an ACL'd file
  keyed by the runner's pid, and the hook and the CLI find it by the pid they already find (`ancestry.go`). `atrium` run
  inside a card presents the card token and never the operator token, even though it could read the file.
- **`Authorization: Bearer <token>`** carries a token. **`atrium token`** prints the caller's own token for a script
  that wants curl.

### Across the hub

The hub checks the credential where the request enters and forwards it over the link data connection with
`X-Atrium-Scope: operator` or `X-Atrium-Scope: card <room~id>`. It strips any `X-Atrium-Scope` the caller sent. A room
believes that header only on its link data handler and strips it on every other listener, the same listener rule as
stage 0. A card token names its room (`<room>~<card>.<secret>`), so the hub asks that room over the control link
whether the token is good and caches the answer for five minutes. The MCP server's `X-Atrium-Agent` claim (L3) is
replaced: `ctlclass.go` reads the class off the verified card.

### Enforcement

Every route family is one line in `internal/api/scope.go` (room), `internal/link/scope.go` (hub) and the agent
listener's mux. The appendix is that table, route by route, taken from the muxes at 2d4e19b1. A route not in the table
answers `operator` only, so a new endpoint is closed until somebody opens it on purpose.

`card` is checked against the card: **self** means the path's `{id}` is the caller's own card, and **owned** means
the caller's own card or one whose `spawned_by` is the caller's card.

Two body rules sit beside the route table, because one route carries fields of different weight:

- `PATCH /v1/tasks/{id}` from a card may change `status`, `why`, `alias`, `rank` and tags. `auto_approve`,
  `auto_minutes`, `peer_typing` and `overrides` are `operator` only.
- `POST /v1/launch` from a card may not carry `env`, and `args` must be empty or on the harness's own list. So the
  audit's `--dangerously-skip-permissions` in `args` is refused (section 12, item 3).

### Rolling it out

`auth.local` is a daemon setting: `off`, `warn`, `enforce`. `warn` accepts everything and runs every check `enforce`
would: the route's scope AND the two body rules above. It records each call either kind would refuse, with which kind
it was (`route` or `body`), the route, the scope it needed, the field that tripped a body rule, the user agent, and the
process name when the port lookup finds one. The gear shows the count and the list. Stage 2 ships in `warn`. Clint's
scripts move to `atrium` verbs or `atrium token` while the count falls. `enforce` is clint's to switch on (section 12,
item 1).

**Until 2c lands, every agent is `none`.** 2a has no card tokens, so a session's hooks, CLI calls and MCP calls carry
no credential, and `warn` counts them. So `enforce` must not be switched on before 2c passes its test. The setting
refuses `enforce` while card tokens are not being minted, and says why.

**Acceptance test for 2a** (operator and `none` callers only). Each row of the appendix from `atrium` in a terminal that
is not a card (operator token), from the board (cookie), and from curl with no token, in `warn` and in `enforce`, on a
room's loopback port and through the hub. In `warn` every call succeeds and each would-be refusal is recorded with the
right kind, including a `PATCH` carrying `auto_approve` and a launch carrying `env` from a `none` caller. In `enforce`
operator succeeds everywhere and `none` succeeds only on `all` rows. A spoofed `X-Atrium-Scope` from a caller is
stripped on the hub and on a room's loopback listener. `POST /v1/board-code/redeem` accepts a fresh code once and
refuses it a second time and after 60 seconds. An authenticated request with a cookie older than a day comes back with
a renewed cookie and a fresh 30-day expiry. A cookie issued by one listener (the hub's 7778, say) is refused by another
(a room's 7781, or the published board), and a cookie set on the published board is refused on loopback. Every hook,
with and without a token, still passes the stage 1 test.

**Acceptance test for 2c** (card scope). Each `C`, `Cs` and `Co` row from a card, on its own room's port and through
the hub from another room, for its own card, a card it launched, and a card it did not launch. The two body rules from
a card, in `warn` (recorded as `body`) and in `enforce` (refused). The hub's verification: a good token is accepted and
cached, a token for a card that has ended is refused within the cache's five minutes, and a token naming one room
presented for another room's card is refused. `from` on `/tell` and `/v1/say` is the token's card whatever the body
says. The MCP tools' class comes from the verified card, and `X-Atrium-Agent` alone gets nothing more than `none`.

## 7. The hard limit

Every session atrium runs is a process of clint's OS account. A card's token sits in an environment another process of
the same user can read (`/proc/<pid>/environ`, or `ReadProcessMemory` on Windows). The operator token is a file that
user can read. No port, pipe, ACL or token on one machine separates two processes of one user.

So stage 2 is not a sandbox. It stops accidents, browsers, other accounts and other programs. It makes an agent that
wants to be the operator do something unusual first, and stage 3 refuses the plain ways of doing it. A real boundary
between agents means running each as its own OS user or in a container, which is its own design.

## 8. Stage 3: the chain refuses atrium's own credentials

A fixed step in `onPermRequest`, between the shelved card (3) and a standing rule (4). A tool call whose command or
input names the operator token, a join-token file, `ca.key` or `room.key` is blocked with a reason. No rule and no auto
mode can approve it.

**The hook has to ask for it.** `permSkipTools` in `hook_permission.go` never gates `Read`, `Grep` or `Glob`, so the
chain would never see the Read tool open the token. Stage 3 makes the hook gate those tools when their path matches the
same list, which the hook checks itself with no round trip.

It is not a wall. A path spelled some other way gets through. It makes the obvious reads refused and visible, and every
other read a command the permission gate or the review sees.

**Acceptance test.** With auto mode on and an allow-everything rule, `Get-Content`, `cat`, `type`, `copy` and the Read
tool on each listed path are blocked, and the same commands on any other path are approved as before.

## 9. Stages 4 to 7

**Stage 4, M3: enforce inventory membership first** (codex A2). Attach refuses a certificate whose room is not in the
hub's inventory, whether or not a room of that name is attached. Forgetting a room records its leaf fingerprint as
revoked, so forget means refuse, and a room that dials again after a forget is refused rather than "written down
afresh" (`proxy.go:994-999`). `ca.key`, `room.key` and the hub leaf key get the same explicit DACL as the operator
token. **Stage 4b, the follow-up:** room leaves last one year, and the hub re-issues each over the control link at half
its life. The CA keeps ten years.

*Acceptance test.* A forgotten room's copied key is refused while the real room is offline. A room in the inventory
attaches as before. A re-join with a fresh token after a forget is accepted. 4b: a leaf past half its life is replaced
on the next attach without a re-join.

**Stage 5, M2: the link bind and the firewall.** `--link` binds the advertised addresses (`--link-advertise`, and the
ziti tunnel's address when one is named), not `0.0.0.0`. `0.0.0.0` still works and prints a warning. A binary under
`build.claude\` or a worktree binds loopback only, whatever its flags say. The firewall is operations, so it is a
script, `scripts/firewall-trim.ps1`, `-WhatIf` by default, runnable by hand.

**The firewall end state** (codex A1):

- One inbound Allow rule for atrium: the installed binary (`%USERPROFILE%\.atrium\bin\atrium.exe`), TCP, the link
  port only (7779 on sg4), local address the advertised LAN address and the ziti tunnel's address, profile Private.
- No rule for any binary under `build.claude\`, a worktree, or any other path.
- No rule on the Public profile. sg4's two networks are Public today, so the script also names the one command that
  makes the LAN network Private, and does not run it: that is a machine-wide change.
- The board and agent ports have no rule at all. They are loopback.

*Acceptance test.* After the script, `Get-NetFirewallApplicationFilter | ? Program -match 'atrium'` lists one rule
matching the end state. The hub's link socket is bound to the advertised address, not `::`. A room on the LAN and one
over ziti still attach. A dev build started with `--link 0.0.0.0:7799` is bound to loopback.

**Stage 6, M4: what a room refuses from its hub.** The room enforces the appendix on what arrives over the link, using
the forwarded scope. Whoever reaches 7778 without the operator cookie no longer drives every room. Shutdown and git
stay refused through the hub. An upgrade offered by a hub stays opt-in (`--accept-upgrades`). Signed builds are named,
not designed. Two halves, because card scope over the hub only exists once 2c does:

- **6a, after 2a: operator and `none`.** The room enforces the table on link traffic for those two scopes. A forwarded
  request with no scope header is `none`.
- **6b, after 2c: card scope over the hub.** The room enforces `C`, `Cs` and `Co` on link traffic, checking self and
  owned against the card the hub verified.

*Acceptance test, 6a.* Over the link: a request forwarded as `operator` succeeds on an `O` route. One with no scope
header is treated as `none` and refused on every route that is not `all`, in `enforce`, and recorded in `warn`.

*Acceptance test, 6b.* The card-over-hub cases of the 2c test, sent through the hub to a room on another machine: a
`card` scope on an `O` route is refused, a `Cs` route for another card is refused, a `Co` route for a card it launched
succeeds, and a `card` scope whose card is on a third room is refused.

**Stage 7, the lows.** L1: the daemon writes a per-start key into the ACL'd whereami file, and an answer to a hook
carries an HMAC of the request under it. A hook that gets an answer without a good one treats it as unreachable
(stage 1), so a squatter can make atrium look down and cannot answer "block" in clint's voice. L2: `room join` reads the
join string from stdin or `--from-file`, and warns when it is an argument. L4 is closed by stage 2.

*Acceptance test.* A listener on 7777 that is not atrium gets every hook falling to the native prompt. `room join` with
the string as an argument prints the warning and still joins.

## 10. Web Push, and whether HTTPS makes it possible

**Web Push needs a secure context and a service worker.** A secure context is an `https://` origin, or `localhost`.
The board already registers `sw.js`.

| how the phone reaches the board | secure context | Web Push |
| --- | --- | --- |
| a zrok public share | yes. zrok's public frontend serves HTTPS | possible, once the phone can subscribe as the operator (stage 2) |
| an OpenZiti intercept (`http://atrium.ziti`) | no. The overlay encrypts, but the browser sees `http://` | no. It would need HTTPS on the ziti listener with a certificate the phone trusts, meaning atrium's CA installed on the phone. Not here |
| the LAN (`http://192.168...`) | no | no |

Android Chrome subscribes from any secure page. An iPhone needs the page installed to the home screen (Safari 16.4 and
later), and `/m` already ships the manifest for that.

The hub notify trigger already expects "Web Push ... as a second one behind the same trigger"
(`internal/link/notify.go`). The push sink is that sink, so the growler's phone reminders reach the phone with the
same backoff and the same "no desktop tab visible" rule.

- The hub makes a VAPID key pair once (RFC 8292). It is atrium's own key, so the credential rule holds, and it is on
  the never-exported list.
- The phone subscribes from `/m`. The subscription needs `operator` scope, is stored on the hub, is never exported, and
  is deleted on unsubscribe or when the push service answers 404 or 410.
- The payload is encrypted (RFC 8291) and carries the notify sink's four fields only: name, reason, card, room.
- A subscription is bound to its origin. A reserved zrok name keeps it stable, and a new share name means subscribing
  again, which the board says.

*Acceptance test.* A phone on the zrok public share subscribes, locks, and receives a growler raise and its first
reminder. Unsubscribing stops them. A subscribe attempt without the operator cookie is refused in `enforce`.

## 11. The build, in order

Sizes: S is up to half a day for one worker, M is a day. Each stage has its acceptance test above, and a stage is done
when that test passes on claude/main, not before.

| order | stage | owner | size | closes | depends on | deploy |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | browser edge (section 4) | @runtime | S | C1, C2, H1, M1 | nothing. In flight | hub and room |
| 1 | a refused hook is unreachable (section 5) | @runtime | S | codex C3 | nothing | CLI (the hook binary) |
| 2a | scopes, route table, operator cookie and token, `atrium board`, `atrium token`, `warn`, scope header across the hub | @runtime | M | H2, L4 | 1 | hub and room |
| 2b | the sign-in page, "caller not verified", the would-be-refused count in the gear | @ui | S | | 2a | hub |
| 2c | card tokens at launch and join, the agent listener taking the caller from the token, hub verification, `X-Atrium-Agent` replaced | @runtime | M | H3, L3 | 2a | room, then hub |
| 3 | the chain step and the hook gating reads of credential paths (section 8) | @runtime | S | part of section 7 | 1 | room and CLI |
| 4 | inventory enforcement, forget means refuse, key DACLs | @runtime | S | M3 | nothing | hub |
| 4b | one-year leaves renewed at half life | @runtime | S | M3 follow-up | 4 | hub, then rooms |
| 5 | link bind, dev builds on loopback, `scripts/firewall-trim.ps1` | @runtime | S | M2 | nothing | hub |
| 6a | the room enforcing operator and `none` on hub traffic | @runtime | S | M4 | 2a | room |
| 6b | the room enforcing card scope on hub traffic | @runtime | S | M4 | 2c, 6a | room |
| 7 | hook answer HMAC, join from stdin | @runtime | S | L1, L2 | 1 | room and CLI |
| W1 | the Web Push sink, VAPID, subscriptions | @runtime | M | growler phone reminders | 2a | hub |
| W2 | subscribe on `/m`, the service worker's `push` handler | @ui | S | | W1 | hub |

Stages 1, 4 and 5 can run beside stage 0 now. Stage 2a in `warn` changes no answer, so it can land without a hold. 2a
is tested with operator and `none` callers only, and 2c carries the whole card-scope test (section 6). `enforce` waits
for 2c.

## 12. Questions for later

clint is away and asked for progress without him (2026-09-30). The build goes ahead on these defaults. They are also
in the day's questions-for-later list.

1. **When `enforce` comes on.** Stage 2 ships in `warn`. Moving to `enforce` breaks any script of his that calls curl
   with no token, reads included. The default is to move after a week in which the count holds only callers he has
   decided to leave broken. His to flip.
2. **The fixed chain step (section 8).** It adds a step between "a shelved card" and "a standing rule", with no switch
   on the board. The chain's order is documented as critical. Default: on, in that place.
3. **What a card may pass to launch.** The default refuses `env` and takes `args` only from the harness's own list,
   which closes `--dangerously-skip-permissions` from a card. If a director needs a model or an effort level, those
   two go on the list. Does anything of his launch with other arguments from inside a card?
4. **Web Push through Google and Apple.** Four encrypted fields cross the phone vendor's push service. Acceptable, as
   the parked ntfy route would cross ntfy's server?
5. **Restarting atrium from a card.** The default: `POST /_hub/restart` and `restart_atrium` are `operator` only, with
   one exception, the deploy owner through the deploy-hold path (the appendix, "the deploy owner's exception"). That
   keeps the orchestrator's deploys and `restart_atrium` working and nothing else. Should the exception exist at all,
   or should every restart go through clint?

## Appendix: every route, and who may call it

From the muxes at 2d4e19b1: `internal/api/api.go` (the room board), `internal/daemon/daemon.go` (the agent listener)
and `serveHubAPI` in `internal/link/proxy.go` (the hub). Scopes: **O** operator, **C** any card, **Cs** the caller's own
card, **Co** own or launched card, **H** hook-open (section 5), **all** anyone including `none`. Operator is always
allowed. A guest keeps the allowlist in `overlay_guest.go`, which this table does not widen.

### The room board API

| scope | routes |
| --- | --- |
| all | `GET /v1/health`, `GET /` and the board's static files, `POST /v1/board-code/redeem` |
| C (read) | `GET /v1/room/stats`, `/v1/settings`, `/v1/fixtures`, `/v1/themes`, `/v1/rooms`, `/v1/dispatch`, `/v1/tasks`, `/v1/state`, `/v1/tasks/{id}`, `/v1/tasks/{id}/asks`, `/v1/offered`, `/v1/history`, `/v1/tasks/{id}/files`, `/files/list`, `/files/zip`, `/files/text`, `/icon`, `/sessions`, `/sessions/{session}/export`, `/v1/sources`, `/v1/recognisers`, `/v1/providers`, `/v1/providers/{name}/repos`, `/v1/providers/{name}/worktrees`, `/v1/browse`, `/v1/tasks/{id}/events`, `/review`, `/v1/waiting`, `/v1/permissions`, `/v1/permissions/history`, `/v1/rules`, `/v1/rules/export`, `/v1/rules/preview-claude`, `/v1/hooks`, `/v1/harnesses`, `/v1/harnesses/discover`, `/v1/tasks/{id}/scrollback/older`, `/scrollback/raw`, `/scrollback/text`, `/typing`, `/v1/actions`, `/v1/tasks/{id}/says`, `/v1/peers/rooms`, `/v1/peers/card`, `GET /v1/tasks/{id}/restart-wake`, `GET /v1/hold`, `GET /v1/tasks/{id}/new-context`, `/v1/tasks/{id}/usage`, `/replies`, `/messages`, `/v1/usage`, `/v1/usage/limits`, `/v1/usage/items`, `GET /v1/events` |
| C (read-shaped POST) | `POST /v1/tasks/{id}/files/probe`, `POST /v1/recognise`, `POST /v1/preflight` |
| C (as itself) | `POST /v1/say` (`from` is the token's card), `POST /v1/launch` (body rule, section 6), `POST /v1/dispatch`, `POST /v1/merged`, `POST /v1/merge-proof`, `POST /v1/tasks/archive-workers`, `POST /v1/hold` |
| Cs | `POST /v1/tasks/{id}/report`, `POST /v1/tasks/{id}/files`, `PUT /v1/tasks/{id}/files/text`, `POST` and `DELETE /v1/tasks/{id}/restart-wake`, `POST /v1/tasks/{id}/keepalive`, `POST` and `DELETE /v1/tasks/{id}/icon` |
| Co | `PATCH /v1/tasks/{id}` (body rule, section 6), `POST /v1/tasks/{id}/exit`, `/kill`, `/cull`, `/cull/hold`, `/restart`, `/resume`, `POST` and `DELETE /v1/tasks/{id}/new-context`, `POST /v1/peers/exit` |
| O | everything else, which is: `POST /v1/board-code` (the mint), `POST /v1/settings`, fixtures `PUT`, `POST`, `DELETE` and `/start`, themes `PUT`, `DELETE` and `/import`, `GET /v1/overlays` and every overlay write (`PUT`, `/start`, `/stop`, `/setup`, `/teardown`, `/inspect-token`, `/zrok/reserve`, `/zrok/endpoint`, `GET /zrok/account`, `GET /ziti/services`), `POST /v1/rooms`, `DELETE /v1/rooms/{name}`, `GET /v1/rooms/join`, `POST /v1/rooms/{room}/permissions/{id}/decide`, `DELETE /v1/dispatch/{id}`, `POST /v1/dispatch/{id}/result`, `GET` and `PUT /v1/auth`, `GET /v1/config/export`, `POST /v1/config/import`, `GET /v1/shares`, `POST` and `DELETE /v1/tasks/{id}/share`, `POST /v1/tasks/{id}/seen`, `/questions/dismiss`, `DELETE /v1/tasks/{id}/asks`, `DELETE /v1/tasks/{id}`, `/promote`, `POST /v1/tasks/prune`, `/pin-order`, `POST /v1/intake`, `POST /v1/tasks/{id}/files/open`, `/open-terminal`, `DELETE /v1/tasks/{id}/files`, `DELETE /v1/tasks/{id}/sessions/{session}`, sources `PUT`, `DELETE` and `/run`, recognisers `PUT` and `DELETE`, providers `PUT`, `DELETE`, `/discover`, `/check-worktrees`, `/worktree`, `PUT` and `DELETE /repos`, `POST /v1/shutdown`, `POST /v1/permissions/{id}/decide`, rules `POST`, `DELETE` and `/import`, `POST /v1/hooks/install`, harnesses `PUT`, `DELETE` and `/setup/fix`, `GET /v1/tasks/{id}/attach`, `POST` and `DELETE /v1/tasks/{id}/shell`, actions `PUT` and `DELETE`, `POST /v1/tasks/{id}/action`, `/note/send`, `/message` |

`attach` is `O` because a terminal takes keystrokes, and a card typing into another card's terminal is that card
speaking as the other. A card says through `/v1/say`. `seen` and `questions/dismiss` are `O` because they record that
a human looked, which is the fact seen exists to keep. `message` is `O` because it counts as the operator for seen.

### The agent listener

| scope | routes |
| --- | --- |
| H (section 5) | `/activity`, `/telemetry`, `/session`, `/gate`, `/permission`, `/stop`, `/hooks-changed`, `/finish` |
| C (as itself) | `/tell`, `/answer`, `/help` (`from` is the token's card), `/peers` |

### The hub

| scope | routes |
| --- | --- |
| all | `/_hub/health`, the board's static files, `POST /v1/board-code/redeem` (answered by the hub, not proxied) |
| C (read) | `/_hub/rooms`, `/_hub/inventory`, `GET /_hub/deps`, `GET /_hub/growls`, `GET /_hub/launch-caps`, `GET /_hub/deploy-owner`, `/v1/events/hub`, `/v1/events/room/<name>`, `/v1/events` |
| C (as itself) | `/_hub/mcp` (tools by the verified card's class, except the restart and deploy tools below), `/_hub/deps/ready`, `/_hub/deps/rename`, `/_hub/deps/clear`, `POST /_hub/growls/{id}` |
| O, with the deploy owner's exception | `POST /_hub/restart`, and on `/_hub/mcp` the tools `restart_atrium` and `atrium_deploy` with `start`, `wait` or `cancel`. `atrium_deploy request` and `status` stay `C` |
| O | `POST /v1/board-code` (the mint, answered by the hub, not proxied), `/_hub/audit`, `/_hub/nudge` (atrium's own rooms present the operator token), `/_hub/inventory/mark`, `/_hub/inventory/forget`, `/_hub/notify`, `/_hub/notify/test`, `/_hub/git/sync`, `/_hub/git/collect`, `/_hub/git/status`, writes to `/_hub/launch-caps` and `/_hub/deploy-owner`, `/_hub/presence`, `/_hub/restart/pause`, `/_hub/restart/resume`, `/_hub/restart/input` |
| as the room it names | every proxied `/v1/...` route, checked by the hub against the room table above and forwarded with its scope |

The hub's loopback-only guards stay as they are, and they add to scope rather than replacing it.

### The deploy owner's exception

The one place a card may restart atrium. It exists because the orchestrator's deploys (`atrium_deploy`, the room deploy
hold design) and `restart_atrium` depend on it. It is checked on the hub, at request time, and all three conditions
must hold:

1. **The caller is the deploy owner.** The verified card's handle or alias equals the `owner` that
   `GET /_hub/deploy-owner` answers at that moment, compared the resolver's way (case, a leading `@`). Setting the owner
   is `operator` only, so no card can name itself.
2. **The call is on the deploy-hold path.**
   - `atrium_deploy start`, `wait` and `cancel` need only condition 1. `start` is what sets the hold.
   - `restart_atrium` for a room, and `POST /_hub/restart`, need a `deploy` hold that is active on the room being
     restarted and was started by this card (`by_card` on the hold row). For `POST /_hub/restart`, which restarts the
     hub rather than a room, that means an active `deploy` hold this card started on any room.
3. **It is recorded.** The audit gets `restart-by-deploy-owner` or `deploy-by-deploy-owner` with the card, the hold and
   the room.

Anything else, including the deploy owner with no active hold of its own, is `operator` only. In `warn` a call that
fails a condition is recorded as a `route` refusal naming the condition that failed.

The stdio `atrium control` server's `--restart-now` swaps a binary as a local process and is not an HTTP route, so no
table here can gate it. It sits behind the same limit as everything a same-user process can do (section 7).

*Acceptance test* (in 2c, since it needs a verified card). The deploy owner with an active hold of its own restarts its
room and the hub. The same card without a hold, with a hold another card started, or on a room it holds nothing on, is
refused. A card that is not the owner is refused with a hold active. Changing the owner at `/_hub/deploy-owner` takes
effect on the next call. Each accepted call has its audit line.
