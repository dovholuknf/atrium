# Loopback is not the operator behind a local proxy

Status: built. Every "is the caller loopback" gate is `edge.LocalOperator` (`internal/edge/local.go`).

Origin: designed by @rnd 2026-09-30, for @runtime. Nothing here is built. Asked for by @runtime, flagged by the
orchestrator for clint.

## The answer

Replace every "is the caller loopback" gate with one function, `edge.LocalOperator(r)`, that is true only when all
three hold:

1. `RemoteAddr` is loopback (what the gates check today),
2. `Host` names loopback (`localhost`, `127.0.0.1`, `::1`, with or without a port), and
3. the request carries none of `Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`, `X-Real-Ip` or `X-Proxy`.

That is `internal/daemon/pprof.go`'s check today, lifted into `internal/edge` and used everywhere. Headers are used
**only to refuse, never to allow**. A caller can add a header but cannot remove one a proxy in front of it added. So a
header can make a local request look remote, which costs the operator nothing, and can never make a remote one look
local.

The share clint runs today is caught by it on two counts. That share is `zrok share public localhost:7778`, started by
hand as clint and proxying to the hub. zrok's proxy backend (`endpoints/proxy/backend.go` in `zrok/v2` v2.0.4) is
`httputil.NewSingleHostReverseProxy` with the default Director. That mode appends `X-Forwarded-For`, zrok adds
`X-Proxy: zrok`, and the default Director leaves the public `Host` (`atrium.shares.zrok.io`) on the request.

The other two options, briefly:

- **A separate listener per share** works only if every share is pointed at it. A share started by hand at the
  board port, which is what is running now, is not, and nothing tells atrium. It adds a port and fixes nothing the
  check above does not.
- **Shares through atrium's own overlay listener** (`internal/daemon/overlay_native.go`, and `internal/link/zrok.go`
  on the hub, which is private-share only today) is right for shares atrium starts. Atrium knows the listener, so it
  needs no headers. Build it as well: every overlay listener marks its connections through `http.Server.ConnContext`,
  and `LocalOperator` is false for a marked connection whatever its addresses say. It does not cover a share started
  by hand, which is why it cannot be the whole answer.

What stays open after this, and closes with security stage 2: a forwarder that adds no header and keeps a loopback
`Host`. That is a raw TCP tunnel (`ssh -L`, `socat`, zrok's `tcpTunnel` backend). Each of those already needs an
account on the machine or a private token, so it is not the internet behind basic auth. Stage 2's operator cookie and
token (`docs/rnd/security-design.md`) close it, and they plug into the same function: `LocalOperator` gains "and holds
the operator capability" when stage 2 ships, so every gate gets it at once.

## 1. The gates

Every caller of `loopbackRemote` (`internal/link/control_mcp.go:1730`) and the two daemon checks, on claude/main at
2026-09-30 19:30:

| gate | file | guards | weight |
| --- | --- | --- | --- |
| control MCP server, `/_hub/mcp` | `link/proxy.go:1293` `serveControl` | `restart_atrium` and the tools that drive other sessions | high |
| notify command, PUT and test | `link/notifyapi.go:62` | sets a program the hub runs, and runs it | high |
| hub restart ask | `link/restartgate.go:511` | the deploy script's door to a hub restart. Pause stays open on purpose | medium |
| deploy owner, PUT | `link/deploy_mcp.go:431` | who owns the active deploy hold | medium |
| hosts, PUT `/_hub/hosts` | `link/hosts.go:101` | the names the hub answers, which is the DNS rebinding allowlist (00b9dc0d) | medium |
| launch caps, PUT | `link/launchcaps.go:127` | how many workers a room may run | medium |
| git sync | `link/git_hub.go:62` | starts sync between the hub and its rooms, and reads its status | medium |
| item gates, writes | `link/deps.go:557` | changing an item's dependency gates | low |
| store nudge | `link/proxy.go:1318` `serveNudge` | "re-read your store", no arguments | low |
| room shutdown | `daemon/shutdown.go:84` | `POST /v1/shutdown` on a room. Already refused while atrium's own share runs (`d.sharing()`), and the hub never proxies it (`link/proxy.go:473`). A share started by hand at a ROOM's board port passes today | high |
| profiling | `daemon/pprof.go:64` | `/debug/pprof`. Already has all three checks. It is the template | done |

Not gates, and left alone: `edge.go:170` (which names a listener answers, the rebinding check), `cli/atrium_run.go:704`
(the CLI deciding whether a URL is local, client side).

Designs on file that lean on "the request came over the loopback listener" and must call `LocalOperator` instead:
`docs/rnd/open-with-system-design.md` section 4. `internal/api/CLAUDE.md` says every request over a share presents as
loopback, so the check is the listener. That holds for atrium's own overlay listener, and fails for a share started by
hand at the loopback listener, which is the case here. That file should point at this design. CLAUDE.md files are
clint's to edit, so it is parked for him below.

## 2. What a refused request says

The gates keep their own messages, with one change: when the refusal is because of a forwarding header or a
non-loopback `Host`, the message ends `this request came through a proxy (zrok or similar). Run it from a browser or
shell on the machine itself.` So clint, on his phone through the share, is told why the settings that worked on the
desktop do not work there.

The board reads `GET /v1/whoami` (new, open, no side effects): `{"operator": true|false, "why": "..."}`, computed by
`LocalOperator`. The board greys the operator-only controls when false, with the `why` as their title, rather than
letting a press fail. This is a hint: the server check is the gate.

## 3. A share started by hand

What happens to `zrok share public localhost:7778`, the case clint has:

- Everything a share user could do before still works: cards, terminals, messages, approvals. None of it was ever
  loopback-gated, and a share user already has terminals, so nothing here narrows what the share is for.
- The operator-only rows in section 1 refuse through it, by `X-Forwarded-For`, `X-Proxy` and `Host` (any one is
  enough).
- Atrium cannot see the share exists, so `d.sharing()` stays false and nothing on the board says a share is up. That
  is not this design's problem. The overlay listener in the answer above is how atrium learns about a share, and
  `docs/fabric/overlays.md` already recommends it.

## Stages

| stage | what | owner | size | acceptance test |
| --- | --- | --- | --- | --- |
| LP1 | `edge.LocalOperator`, and every gate in section 1 calls it | @runtime | small | table test: loopback with `Host: localhost:7778` and no headers passes. Each of the five headers alone refuses. `Host: atrium.shares.zrok.io` from loopback refuses. A non-loopback `RemoteAddr` refuses. One test per gate that a request with `X-Forwarded-For` is 403. A real `httputil.NewSingleHostReverseProxy` in front of the hub handler is refused at `/_hub/mcp` |
| LP2 | overlay listeners mark their connections, and `LocalOperator` refuses a marked one | @runtime | small | a request served from an overlay listener with a loopback `RemoteAddr` and no headers is refused |
| LP3 | `GET /v1/whoami`, and the board greys operator-only controls | @runtime, @ui | small | headless: `operator: false` greys the notify, hosts, caps and restart controls with the reason as their title |

LP1 alone closes clint's case and ships with the next hub deploy. LP2 and LP3 follow. Stage 2 of the security design
later adds the capability check inside `LocalOperator`.

## Questions for later

One for clint, not blocking: add a sentence to `internal/api/CLAUDE.md` that a share started by hand at the loopback
listener also presents as loopback, and that `edge.LocalOperator` is the check. Decided here: headers only refuse,
the pprof check generalized, the overlay mark as well, no separate listener.
