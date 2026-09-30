# Security design: close the browser edge first, then give every caller a name

Status: design, 2026-09-30, @rnd. Item `docs/backlog/rnd/rd-new-security-review.md`, PRIORITY. Built from @review's
audit, `docs/review/security-audit-2026-09-30.md` (16058425). Finding ids (C1, H2, M3 ...) are the audit's. Built by
@runtime, with @ui for the board's sign-in page and the phone. Nothing here is built.

## 1. The answer in nine lines

1. **Stage 1 closes the browser edge and ships alone.** `http.CrossOriginProtection` and a `Host` check on every
   listener a browser can reach, and the Origin check put back on both websocket accepts. It closes C1, C2, H1 and M1
   for browsers, and nothing atrium runs sends the headers it refuses.
2. **Then every request carries a credential, and a credential has a scope.** `operator` (clint: the board cookie, or
   the operator token for `atrium` in his own terminal), `card` (one session: a token atrium puts in that runner's
   environment), `guest` (a lent session), and `none`. It closes H2, H3, L3 and L4.
3. **Curl without a credential is refused**, on writes and on reads. `atrium <verb>` is the supported client, and
   `atrium token` prints the caller's own token for a script that really wants curl. A week in `warn` mode first, so
   nothing clint runs breaks without being seen.
4. **An agent is a card, never the operator.** From inside a session the CLI presents that card's token. A card may
   finish, report, say as itself, and launch and exit its own workers. It may not approve a permission, write a rule,
   change a setting, or edit anything atrium runs.
5. **Hooks stay fail-open.** A hook with no credential still posts activity and still asks for permission, and the
   board marks that request "caller not verified". It cannot speak as another session or end another session's work.
6. **The hard limit, stated once.** Agents run as the same OS user as clint. A hostile agent can read a file or
   another process's environment that clint can. Against that, the permission gate seeing the command is the
   boundary. So one new fixed step in the chain refuses reading atrium's credential files, ahead of auto mode.
7. **TCP on loopback with tokens, not named pipes.** A pipe fixes only callers from other OS accounts, which an ACL on
   the token file fixes too, and the browser, the phone and the hub link cannot use a pipe.
8. **Then revocation (M3), the link bind and firewall (M2), and what a room refuses from its hub (M4).**
9. **Web Push becomes possible on a zrok public share**, which is HTTPS already, once stage 2 lets a phone subscribe as
   the operator. Not over an OpenZiti intercept, which the browser sees as plain http.

## 2. The threats, and which stage answers each

| threat | findings | answered by |
| --- | --- | --- |
| a web page in any browser on the machine | C1, C2, H1, M1 | stage 1 |
| another OS account on the machine | H2 (the multi-account line), M3 key files | stage 2 (ACL'd token files), stage 4 |
| a local process that is not atrium, curl included | H2, H3, L4 | stage 2 |
| one agent acting as another agent, or as clint | H3, L3 | stage 2 (card scope) |
| an agent approving its own or another's tool calls | H2 (`decide`, rules, `global_auto`) | stage 2 (card scope), stage 3 (the chain step) |
| a hostile agent as the same OS user | beyond the audit | stage 3, and the permission gate. Not closed, see section 6 |
| a copied room key | M3 | stage 4 |
| the network reaching the link | M2 | stage 5 |
| the hub driving every room | M4 | stage 6 |
| a squatter on the agent port, a join token in a command line | L1, L2 | stage 7 |

## 3. Stage 1: the browser edge

### What each listener gets

| listener | `Host` check | `CrossOriginProtection` | websocket Origin |
| --- | --- | --- | --- |
| room human (7781, 7791), loopback | loopback names only | yes | checked |
| room agent (7777, 7787), loopback | loopback names only | yes | none served |
| hub board (7778), loopback | loopback names only | yes | checked |
| published board, lent session, hub share (overlay listeners) | none: the share decides the name | yes | checked |
| a room's link data handler (the hub's requests to a room) | none | none | none |
| pprof | already checks all three | | |

**Loopback names** are `127.0.0.1`, `localhost` and `[::1]`, with any port. Not configurable, and needing no share
hosts: a share is its own `net.Listener` and its own `http.Server`, so the check goes on the loopback listener's
handler and the overlay listeners never see it. That answers the audit's "plus the configured share hosts" without a
list to keep in step.

**`CrossOriginProtection`** refuses a non-safe method whose `Sec-Fetch-Site` is not `same-origin` or `none`, and
falls back to comparing `Origin` with `Host` when there is no `Sec-Fetch-Site`. A request carrying neither passes. The
hooks, the CLI, the MCP server and curl send neither, so nothing atrium runs is refused. That holds only if the hook
client never sets `Origin`. A Go test on the hook's request asserts it.

**The websocket accepts** (`internal/daemon/attach.go:256-260`, `park.go:195`) drop `InsecureSkipVerify`.
`coder/websocket`'s default then refuses an `Origin` whose host is not the request's `Host`. The comment that said the
opposite is corrected.

**The link data handler is exempt on purpose, and the reason is the proxy.** The hub forwards a board request to a
room with the browser's `Origin` and its own `Host`. A room checking those would refuse every attach through the hub.
The hub checks at its edge. The room trusts its link data handler because the link is mutual TLS and only the hub can
open it. That trust is M4, and stage 6 narrows it. The room tells the two paths apart by the listener, not by a header,
because a header can be sent by anyone.

### The probes become the tests

Every "proof, run" and "proof, not run" line in the audit, sent with and without a cross-site `Origin`, a rebound
`Host`, and a websocket upgrade from a foreign origin. Each must answer 403 at the listeners in the table, and the same
request with no `Origin` and a loopback `Host` must still work. `/_hub/mcp` already passes (the audit's "what holds").

## 4. Stage 2: a credential on every request

### The four scopes

| scope | who holds it | how it is presented |
| --- | --- | --- |
| `operator` | clint | the board's session cookie (loopback board, published board, hub share), or the operator token in `atrium` run from a terminal that is not a card |
| `card` | one session | `ATRIUM_CARD_TOKEN`, set by atrium in the runner's environment. Sent by every hook and by `atrium` run inside the session |
| `guest` | whoever holds a lent session's address | the lent listener itself, as today. The allowlist in `overlay_guest.go` stays the whole of it |
| `none` | anything else, curl included | nothing |

`Authorization: Bearer <token>` for a token. The cookie is `HttpOnly`, `SameSite=Strict`, and scoped to the listener
that set it.

### What each scope may do

The route table lives beside the mux (`internal/api/scope.go`), one line per route family, and a route not in it is
`operator` only. The same allowlist rule the guest listener follows: a new route is closed until somebody opens it on
purpose.

| routes | operator | card | none |
| --- | --- | --- | --- |
| `/v1/health`, the page and its assets | yes | yes | yes |
| every other read (`/v1/tasks`, scrollback, files, browse, rules, settings, audit) | yes | yes, except `/v1/auth` and the audit feed | no |
| its own card: `finish`, `report`, `seen`, its own files | yes | its own card only | no |
| say or tell, as itself | yes | yes. `from` is the token's card, never the body's | no |
| launch | yes | yes, as the launcher, with the harness's own arguments and no `env` from the body (section 9, item 3) | no |
| exit, kill, cull, new-context, restart | yes | the cards it launched | no |
| permissions `decide`, rules, settings, harnesses, sources, fixtures, config import, auth, overlays, shutdown, `/_hub/*` writes | yes | no | no |

### Presenting it

- **The board.** A browser with no cookie gets a one-page sign-in: "open this board with `atrium board`". `atrium board`
  (new: `atrium open` is taken, it hands atrium a URL to recognise) mints a one-time code, opens
  `http://127.0.0.1:<port>/?code=<code>`, and the board trades it for the cookie and takes the code off the address. The cookie lasts 30 days and renews on use, so this happens about once
  per browser per month. The published board keeps its OIDC or basic login, and that login now sets the same operator
  cookie. The phone signs in the same way on the share it uses.
- **The operator token** is a file under the atrium profile (`%LOCALAPPDATA%\atrium\operator.token`), created with an
  explicit DACL naming only the current user. Go's `0o600` does nothing on Windows (the audit's M3 note), so the DACL is
  set through the Windows API, and a start that finds a wider ACL fixes it and logs it. This closes the multi-account
  line of H2.
- **A card's token** is minted by the room at launch and put in the runner's environment. It is one variable added for
  every runner alike, not a filter on some runners' environment, so the uniform-environment rule holds. A session that
  joined by `atrium join` was not launched by atrium and has no such variable. `join` writes its token to a per-user,
  ACL'd file keyed by the runner's pid. The hook and the CLI already find that pid (`ancestry.go`), and they read the
  token from there.
- **`atrium token`** prints the caller's own token, operator or card, for a script that wants curl:
  `curl -H "Authorization: Bearer $(atrium token)" ...`. It never prints another scope's token.

### Across the hub

The hub checks the credential where the request enters. It then forwards the request over the link data connection
with `X-Atrium-Scope: operator` or `card <room~id>`, and strips any `X-Atrium-Scope` the caller sent. A room believes
that header only on its link data handler, and strips it on every other listener. It is the same listener rule as
stage 1, and it is what stage 6 builds on.

A card token names its room (`<room>~<card>.<secret>`), so the hub knows which room to ask. It verifies the token by
asking that room once over the control link, and caches the answer for five minutes. The MCP server's `X-Atrium-Agent`
claim (L3) is replaced by the token: `ctlclass.go` reads the class off the verified card.

### Hooks stay fail-open

Rule 2 of the daemon's resilience guarantees holds as written. A hook never waits on an answer about its credential.
Refusing a hook's record does not fail a session, because no hook reads the answer. What changes is what an
unverified call is allowed to record:

| hook call | with a valid card token | with none, or a wrong one |
| --- | --- | --- |
| `/activity` | recorded | recorded. A badge is harmless, and `/activity` answers before it works |
| `/session` start or end | recorded | recorded only when the pid check (`ownsSession`, `daemon.go:752`) passes |
| `/permission` | asked as that card | asked, with the request marked "caller not verified" on the board. Approving one only unblocks the process that asked, which could have run the command without asking, so the harm is in a human believing it came from the card. The mark answers that |
| `/tell`, `/answer`, `/help` | delivered as that card | refused. The CLI prints why |
| `/finish` | recorded | `ok`, nothing recorded, logged. That is resilience rule 7 as written |

### Rolling it out without breaking clint

`auth.local` is a daemon setting: `off`, `warn`, `enforce`. `warn` accepts every call, records each unauthenticated
one (route, scope it would have needed, user agent, and the process name when the port lookup finds one), and the board
shows the count. Stage 2 ships in `warn`, and clint's scripts in `D:\tmp` and the scripts under `scripts/` move to
`atrium` verbs or `atrium token` while the count falls. The switch to `enforce` is a one-line setting change and is
clint's (section 9, item 1).

## 5. Stage 3: the chain refuses atrium's own credentials

A fixed step in `onPermRequest`, between the shelved card (3) and a standing rule (4). A tool call that reads, copies
or prints the operator token, the join-token files, `ca.key` or `room.key` is blocked with a reason. No rule and no auto
mode can approve it. The step matches paths in the command or the tool input (the Read tool, `Get-Content`, `cat`,
`type`, `copy`), not only one tool.

It sits before rules so that no allow rule can reach past it, and it is fixed so the board has no switch for it. It
adds a step to a chain whose order is documented as critical, so the step's placement is itself for clint to confirm
(section 9, item 2). The build ships it behind that confirmation's default: on.

It is not a wall. A command that reads the file by a path spelled some other way gets through. What it does is make the
obvious reads refused and visible, and every other read a command a human or the review sees in the audit.

## 6. The hard limit

Every session atrium runs is a process of clint's own OS account. A card's token is in an environment another process
of the same user can read (`/proc/<pid>/environ`, or `ReadProcessMemory` on Windows). The operator token is a file that
user can read. No port, pipe, ACL or token on one machine separates two processes of one user from each other.

So stage 2 is not a sandbox. It stops accident, it stops a browser, it stops other accounts and other programs, and it
makes an agent that wants to act as the operator do something unusual and visible first: read a credential file or
another process's memory. Stage 3 refuses the plain ways of doing that, and the permission gate sees the rest, except
under auto mode for the ones stage 3 does not match. A real boundary between agents means running each as its own OS
user or in a container, and that is its own design, not this one.

## 7. Named pipe or unix socket, weighed

| | loopback TCP and a token | named pipe or unix socket with an ACL |
| --- | --- | --- |
| a browser, the phone, a share | yes | no. A browser cannot open one |
| the hub link | unchanged | no |
| another OS account | refused by the ACL on the token file | refused by the pipe's ACL |
| telling two agents of one user apart | by token | no. The peer is the same user |
| cost | one transport, as today | a second transport for the local CLI and hooks only |

The pipe's one gain, identifying the OS user without a file, is already bought by the token file's ACL. The pipe
cannot serve the browser, which is where C1 and C2 live. Loopback TCP with tokens.

## 8. Stages 4 to 7

**Stage 4, M3: revocation and a shorter room life.** Forgetting a room records its leaf fingerprint as revoked in the
hub store, so forget means refuse. Attach refuses a certificate whose room is not in the inventory or whose fingerprint
is revoked, whether or not a room of that name is attached. A room leaf lasts one year, and the hub re-issues it over
the control link at half its life, so a live room never lapses. The CA keeps ten years. `ca.key`, `room.key` and the
hub leaf key get the same explicit DACL as the operator token.

**Stage 5, M2: the link bind and the firewall.** `--link` binds the advertised addresses (`--link-advertise`, and the
ziti tunnel's address when one is named), not `0.0.0.0`. `0.0.0.0` stays available and prints a warning. A binary under
`build.claude\` or a worktree binds loopback only, whatever its flags say, so a dev build is never on the network. The
firewall is operations, so it is a script: `scripts/firewall-trim.ps1` removes atrium's inbound rules for anything but
the installed binary, and allows that one binary on the link port only, on the Private profile. `-WhatIf` by default.
The workflow rule holds: the logic is in the script, runnable by hand.

**Stage 6, M4: what a room refuses from its hub.** With stage 2 the hub forwards a scope, so the room enforces the same
route table on what arrives from the hub as on what arrives on loopback. Whoever reaches 7778 without the operator
cookie no longer drives every room. Shutdown and git stay refused through the hub as today. An upgrade offered by a hub
stays opt-in (`--accept-upgrades`). Signing builds so a room can check the hub's upgrade came from clint's key is named,
not designed here.

**Stage 7, the lows.**
- **L1, a squatter on the agent port.** The daemon writes a per-start key into the whereami file (ACL'd). A hook's
  answer carries an HMAC of the request under that key, and a hook that gets an answer without a good one treats it as
  unreachable and fails open. A squatter can then only make atrium look down. It cannot type "block" in clint's voice.
- **L2, a join token in a command line.** `room join` reads the join string from stdin or `--from-file`, and prints a
  warning when it arrives as an argument.
- **L4, hub inventory writes.** `operator` scope from stage 2. Nothing more.

## 9. Web Push, and whether HTTPS makes it possible

**Web Push needs a secure context and a service worker.** A secure context is an `https://` origin, or `localhost`.
The board already registers `sw.js`. So the question is how the phone reaches the board.

| how the phone reaches the board | secure context | Web Push |
| --- | --- | --- |
| a zrok public share | yes. zrok's public frontend serves HTTPS | possible now, once the phone can subscribe as the operator (stage 2) |
| an OpenZiti intercept (`http://atrium.ziti`) | no. The overlay encrypts the traffic, but the browser sees `http://` | no. It would need atrium to serve HTTPS on the ziti listener with a certificate the phone trusts, which means installing atrium's CA on the phone. Not in this design |
| the LAN (`http://192.168...`) | no | no |

**What the platforms need.** Android Chrome: a subscription from any secure page. iPhone: the page must be installed
to the home screen (Safari 16.4 and later), and `/m` already ships the manifest that makes it installable.

**How it is built.** The hub notify trigger already expects a second sink ("Web Push is meant to arrive later as a
second one behind the same trigger", `internal/link/notify.go`). The push sink is that second sink, so the growler's
phone reminders (the growler design, section 7) reach the phone the moment it exists, with the same backoff and the
same "no desktop tab visible" rule.

- The hub makes a VAPID key pair once (RFC 8292) and keeps it. It is atrium's own key, not somebody else's credential,
  so the credential rule holds. It is on the never-exported list.
- The phone subscribes from `/m` with a button. The subscription (endpoint and keys) is stored on the hub, needs
  `operator` scope to create, and is deleted on unsubscribe or when the push service answers 404 or 410. It is a way to
  reach that phone, so it is treated as a secret and never exported.
- The payload is encrypted (RFC 8291) and carries the notify sink's four fields and nothing else: name, reason, card,
  room. Never a command, never a recap. It crosses Google's or Apple's push service, encrypted, on its way.
- A subscription is bound to the origin it was made on. A reserved zrok name keeps the origin stable across restarts.
  A new share name means subscribing again, and the board says so.

## 10. The build

Sizes: S is up to half a day for one worker, M is a day, L is two or more. In the audit's order of severity.

| stage | owner | size | what | closes | deploy |
| --- | --- | --- | --- | --- | --- |
| 1 | @runtime | S | section 3, with every audit probe as a test | C1, C2, H1, M1 | hub and room |
| 2a | @runtime | M | scopes, the route table, the operator cookie and token, `atrium board` codes, `atrium token`, `auth.local` in `warn`, the scope header across the hub | H2, L4 | hub and room |
| 2b | @ui | S | the board's sign-in page, the "caller not verified" mark on a permission, the unauthenticated-call count in the gear | | hub |
| 2c | @runtime | M | card tokens at launch and at join, the agent listener taking the caller from the token, hub verification with the cache, `X-Atrium-Agent` replaced | H3, L3 | room, then hub |
| 3 | @runtime | S | the chain step for atrium's credential files | part of section 6 | room |
| 4 | @runtime | S | revocation, the one-year leaf and its renewal, key DACLs | M3 | hub, then rooms |
| 5 | @runtime | S | the link bind, dev builds on loopback, `scripts/firewall-trim.ps1` | M2 | hub |
| 6 | @runtime | S | the room enforcing scope on hub-forwarded requests | M4 | room |
| 7 | @runtime | S | the hook answer MAC, join from stdin, L4 | L1, L2 | room |
| W1 | @runtime | M | the Web Push sink, VAPID, subscriptions | growler phone reminders | hub |
| W2 | @ui | S | subscribe on `/m`, the service worker's `push` handler reusing `notificationclick` | | hub |

**Stage 1 ships first and alone.** It needs no design beyond section 3 and breaks nothing that sends no `Origin`. 2a
and 2c can run in parallel after it. 2a in `warn` breaks nothing either. W1 needs 2a, because subscribing is an operator
act. Stages 4 and 5 do not depend on 2 and can go whenever a worker is free.

Test plan: a new section under the most recent letter in `docs/test-plan.md`. Stage 1 with every audit probe.
Stage 2 with each row of the scope table, from a card, from the CLI in clint's terminal, from bare curl, in `warn` and
in `enforce`, and every hook with and without a token, where a session must never fail to start, finish a turn or make
a tool call.

## 11. Questions for later

clint is away and asked for progress without him (2026-09-30). The build goes ahead on these defaults. They are also
in the day's questions-for-later list.

1. **When `enforce` comes on.** Stage 2 ships in `warn`, which refuses nothing and counts what would be refused. Moving
   to `enforce` breaks any script of his that still calls curl with no token, reads included. The default is to move
   after a week in which the count is only callers he has decided to leave broken. His to flip.
2. **The fixed chain step (section 5).** It adds a step between "a shelved card" and "a standing rule", and nothing on
   the board can turn it off. The chain's order is documented as critical, so this is his call. Default: on, in that
   place.
3. **What a card may pass to launch.** The default refuses `env` from a card's launch body and uses only the harness's
   own arguments. It closes the audit's `--dangerously-skip-permissions` in `args`. If a director needs to pass a model
   or an effort level, those two become an allowlist. Is that enough, or does something of his launch with custom
   arguments from inside a card?
4. **Web Push through Google and Apple.** The push payload is four encrypted fields, and it crosses the phone vendor's
   push service. Acceptable, given that the ntfy route (already parked) crosses ntfy's server the same way?
