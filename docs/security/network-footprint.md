# Network footprint

Every port atrium listens on and every connection it opens, for someone deciding whether atrium may run on a
locked-down work machine, and for a security reviewer. Built from the code on 2026-11-16. Each row names the file
that holds it, so it can be checked.

## In short

- **Nothing binds every interface by default.** Every TCP listener defaults to `127.0.0.1`. The pty host uses no
  port at all.
- **One port may be opened wider on purpose:** the hub's room link, `--link`, which is mutual TLS with a
  certificate the hub signed. The hub's board refuses a non-loopback `--addr`.
- **No telemetry, no update check for atrium itself, no CDN.** The board is served from the binary and talks only
  to the origin it was loaded from.
- **Outbound calls happen only for features you turn on or use:** a forge (`gh`, `bb`, `git` over https), zrok or
  OpenZiti, Web Push, the npm registry for a runner's version, and a notify command you wrote.
- **Rooms dial the hub, never the reverse.** A room needs no inbound port.

## The processes

| Process | Started by | What it is |
| --- | --- | --- |
| hub, `atrium run` | the operator | the board, the room link, the control MCP, the hub's store and git mirror |
| room, `atrium room` | `atrium run` (detached) or the operator | the agents, terminals and the room's store, one per machine |
| pty host, `atrium ptyhost` | the room, with the `pty_host` setting on | holds the terminals across a room restart |
| CLI and hooks | a runner's hook config, a shell | short-lived clients of the room or the hub |
| `atrium daemon` | the operator | the older single process, no hub |
| `atrium preview` | the operator | a throwaway second room for looking at a board change |

Defaults are in `internal/cli/atrium_defaults.go`.

## Listeners

| Process | Address, default | Bind | Changed by | TLS | What it is for | Code |
| --- | --- | --- | --- | --- | --- | --- |
| hub | `127.0.0.1:7778`, and `[::1]:7778` | loopback, enforced | `atrium run --addr` | no | the board, `/v1/*` passed to rooms, the control MCP at `/_hub/mcp`, the CLI (`atrium backlog`, `atrium rooms`, `atrium ci`), `<name>.localhost` board views | `internal/cli/atrium_run.go` (`loopbackBoard`, the board listener), `internal/cli/loopbackv6.go` |
| hub | `127.0.0.1:7779` | loopback by default, may be wide | `--link`, `--link-advertise`, `--transport direct,ziti,zrok` | mutual TLS, hub-signed certificates | the room link: each room dials in and the hub sends its requests back down that connection | `internal/link/transport/direct.go` (`Listen`, `ServerTLS`) |
| room | `127.0.0.1:7777` | loopback by default, not enforced | `atrium room --agent`, `atrium daemon --addr` | no | the agent listener: hooks, the permission chain, `atrium join` and the rest, the card's git hub remote | `internal/daemon/daemon.go` (agent listener) |
| room | `127.0.0.1:7781` | loopback by default, a wide bind only warns | `atrium room --http`, `-` for none, `atrium daemon --http` (default `127.0.0.1:7778`) | no | the room's own board, for when the hub is down. Go's profiler sits on it only when it is loopback | `internal/daemon/daemon.go`, `internal/cli/cli.go` (`warnWideBoard`), `internal/daemon/roomconf/pprof.go` |
| room | `127.0.0.1:<random>` | loopback | none | no, a random token in the path | a git forwarder from a card's git to the hub's store over the link, for as long as one git command runs | `internal/gitsync/forward.go` |
| pty host | `\\.\pipe\atrium-ptyhost-<hash>` on Windows, a unix socket elsewhere | local only | the room's state directory | no | the room asking for terminals | `internal/ptyhost/channel_windows.go`, `internal/ptyhost/channel_unix.go` (mode 0600) |
| `atrium preview` | two ports the OS picks on `127.0.0.1` | loopback | `atrium preview --http`, `--agent` | no | a second room's board and agent listener | `internal/cli/preview.go` (`freePort`) |

The room checks a port is free with a bind and an immediate close in `internal/api/cardports.go` (`bindsFree`) and
`internal/cli/roomrestart.go`. Neither keeps a listener open.

### Only when you turn it on

| Process | Listens on | Turned on by | What it is for | Code |
| --- | --- | --- | --- | --- |
| hub | a ziti service, default `atrium-hub` | `--transport ziti` or the access screen | the room link inside OpenZiti, still mutual TLS inside | `internal/link/transport/ziti.go`, `internal/link/transport/overlaytls.go` |
| hub | a zrok private share | `--transport zrok` or the access screen | the room link inside zrok | `internal/link/transport/zrok_access.go` |
| hub | a ziti service, default `atrium-board` | `--board-transport ziti` or the access screen | the board off this machine | `internal/cli/atrium_run.go` (`TransportBoardZiti`) |
| hub | a zrok share, private or public | `--board-transport zrok`, `--board-share` | the board off this machine. A public share is behind the board's OIDC sign-in | `internal/cli/atrium_share.go` |
| hub | a ziti service | `--ci-hook-service` | CI webhooks | `internal/cli/atrium_cihook.go`, `internal/link/forge/cihook_bind.go` |
| room | a zrok or ziti share | the board's overlay settings | the room's own board off this machine | `internal/daemon/publish/overlay_native.go` |
| room | a zrok share per lent session | lending a session | one card, an allowlist of routes | `internal/daemon/publish/overlay_guest.go` |
| room | a zrok public share per document | sharing a document | a snapshot of one document, with a time limit | `internal/daemon/publish/docshare.go` |
| a card's `ziti` | a controller and a router on two card ports, `127.0.0.1` | a card asking for an overlay | a throwaway OpenZiti network for that card, run as `ziti edge quickstart` | `internal/api/cardoverlay.go` |

An overlay listener opens no local port. The OpenZiti and zrok SDKs dial out to their controller and edge routers
and hand back a `net.Listener`.

Card ports are a range of 200 from 20000 to 47999, picked from the room's name or set by `ports.card_range`, and
50000 to 50999 is kept for previews (`internal/api/cardports.go`). Atrium hands these numbers to a card's agent and
does not listen on them itself.

## Outbound connections

| From | To | When | TLS | Code |
| --- | --- | --- | --- | --- |
| room | the hub's room link, the address in the join string | always, while attached. Retries with backoff | mutual TLS | `internal/link/transport/direct.go` (`Dial`), `internal/link/conn/room.go` |
| room | a ziti controller and edge routers, from the identity file | the room link over ziti | yes | `internal/link/transport/ziti.go` |
| room, hub | the zrok API, `https://api-v2.zrok.io` unless the zrok environment names another, and its ziti network | any zrok use, and `atrium zrok enable` | yes | `internal/link/transport/zrok.go`, `internal/daemon/publish/overlay_root.go`, `internal/cli/zrok_cmd.go` |
| room | this machine's sshd, `127.0.0.1:22` by default | another room opens ssh through the hub, and remote access is on | ssh's own | `internal/link/conn/stream.go` (`serveSSH`), `internal/base/remoteaccess/remoteaccess.go` |
| room | `127.0.0.1:<port>` in the card range or 50000 to 50999 | the board opens a port preview through the hub | no | `internal/link/conn/preview.go`, `internal/api/cardports.go` (`PreviewPort`) |
| room | `https://registry.npmjs.org` | before a launch, for a harness row with a `package`. Cached 6 hours, waits 4 seconds at most | yes | `internal/daemon/runnerupdate.go` |
| room | the forge, through `gh` or `bb` | the pulls view and pull request review | the CLI's | `internal/daemon/pulls/prrunner.go`, `internal/base/forge/` |
| room | the forge, through `git` over https | a clone when the hub's copy is not available | yes | `internal/gitsync/scm.go` |
| hub | the forge, through `gh` or `bb` | `atrium ci`, CI and pull request state | the CLI's | `internal/link/forge/forgeroute.go`, `internal/base/forge/` |
| hub | the forge, through `git` over https | the git mirror | yes | `internal/gitsync/store.go` |
| hub | the browser's push service: `fcm.googleapis.com`, `*.push.apple.com`, `updates.push.services.mozilla.com`, `*.notify.windows.com` | a phone or browser subscribed to Web Push. No other host is allowed | yes | `internal/link/notify/webpush.go` |
| hub | whatever the operator's notify command reaches | a notify command is set. Atrium runs the argv and holds no credential | the command's | `internal/link/notify/notify.go` |
| hub | rooms | never dialled. Every request rides the connection the room opened | | `internal/link/conn/hub.go` |
| hooks, CLI | the room's agent listener, from the address file, else `$ATRIUM_HUB_URL` or `localhost:7777` | every hook event, `atrium join`, `finish`, `tell` | no | `internal/cli/hook.go`, `internal/daemon/roomconf/whereami.go` |
| `atrium control` | the hub's `/_hub/mcp`, per request | a runner's control tools | no, loopback | `internal/cli/mcp_relay.go` |
| `atrium ssh-proxy` | the hub board or the room's board | ssh to another room | no, carries ssh | `internal/cli/sshproxy.go` |
| CLI | the room board and websocket | `atrium` terminal attach | no | `internal/cli/termattach.go` |
| pty host client | the pipe or socket above | the room asking for terminals | no | `internal/ptyhost/client.go` |

A room takes a newer binary only from its hub, over the link, and only when started with `--accept-upgrades`
(`internal/link/conn/upgrade.go`). Nothing checks GitHub releases.

## What the browser connects to

The board is compiled into the binary, xterm.js included (`internal/api/web/vendor/`). The page loads nothing from
another host. It talks to the origin it came from: HTTP for `/v1/*`, an `EventSource` for the event stream and a
WebSocket for terminal attach (`internal/api/web/js/`). By default that is `http://127.0.0.1:7778`, or `:7781` for a
room's own board.

Each browser-facing listener refuses a cross-origin write, a `Host` it is not known by (DNS rebinding) and a
cross-origin WebSocket (`internal/base/edge/edge.go`). `$ATRIUM_HOSTS` adds names it answers to.

Turning on Web Push on a phone makes the browser register with its own vendor's push service, which is the browser's
connection and not atrium's (`internal/api/web/sw.js`, `internal/api/web/m/js/push.js`).

## For a locked-down machine

With the defaults and no overlay, a machine running the hub and its room opens:

- loopback listeners on 7777, 7778 (and `[::1]:7778`), 7779 and 7781, and one named pipe or socket
- no inbound port from the network
- outbound only to the npm registry before a launch, and to the forge when a forge feature or the git mirror is used

A room on a second machine needs one outbound TCP connection to the hub's `--link` address, or an overlay, and no
inbound port. To stop the npm lookup, clear the `package` field of the harness rows.
