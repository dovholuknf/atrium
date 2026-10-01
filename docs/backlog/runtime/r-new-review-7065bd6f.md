# Review of 31278a38 + 7065bd6f (@runtime LP1: `edge.LocalOperator`)

Reviewed by @review, 2026-09-30, from `git diff e0b6c860 7065bd6f` (claude/r-local-operator). The design is
`docs/rnd/local-proxy-trust-design.md`. Hub side (every gate in `internal/link`) and room side (`/v1/shutdown`).

## What holds

- **The predicate.** A request passes only if three things hold. The far end is a loopback IP, with a port, so a
  RemoteAddr that does not parse is refused rather than read whole. The Host is `localhost` or a loopback IP. And none
  of `Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`, `X-Real-Ip` or `X-Proxy` is set. A header can only refuse,
  so a caller cannot use one to get in. zrok's backend fails on the Host and on two headers.
- **Every `loopbackRemote` gate is replaced**, and `loopbackRemote` and the daemon's `isLoopback` are deleted, so no
  gate is left on the old rule. Replaced: notify PUT and test, hosts PUT, launch caps PUT, deps writes, deploy owner
  PUT, `/_hub/git`, `/_hub/mcp`, nudge, the restart ask, and the room's `/v1/shutdown` on its no-token path. The
  token path is unchanged.
- **Every in-repo caller still passes.** Launched sessions use `http://127.0.0.1:7778/_hub/mcp`
  (`~/.atrium/mcp.json`). The hub's control tools call their own board through `loopbackBase`, which turns a wildcard
  bind into `127.0.0.1` and copies no header from the incoming request. `atrium rooms` (nudge, git) defaults to
  `127.0.0.1:7778`. `hub-restart-gate.ps1` defaults to `http://127.0.0.1:7778`. `atrium stop` reads the room's
  location file. Remote rooms never reached `/_hub/mcp`, and they still use stdio control.
- **The refusals say why.** `ProxyNote` adds the proxy sentence when a header is present, or when a loopback far end
  sends a public Host. It stays quiet for a plain remote caller. JSON bodies that were hand-built are now `%q`.
- **The behavior change is the intended one.** Over clint's zrok share he loses the notify set and test and the
  hosts PUT. Nothing else he uses from the phone is gated.

## Tests

In a detached worktree at 7065bd6f, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet ./internal/edge/ ./internal/link/ ./internal/daemon/`: clean
- `go test -count=1 ./internal/edge/`: ok
- `go test -count=1 -run 'Loopback|Operator|Notify|Hosts|LaunchCap|Deps|Git|Restart|Nudge|DeployOwner|Control'
  ./internal/link/`: ok (24s)
- `go test -count=1 -run 'Shutdown|Stop' ./internal/daemon/`: ok (16s)

@runtime reports edge, link, daemon and cli green in full. I did not rerun the whole packages.

## Findings

### Low

1. **A `--board-addr` passed by hand that is not loopback is now refused.** `hubStoreFlags.nudge` and `hubCall`
   (`atrium rooms git`) use the address exactly as given. A hub bound wide and named as `0.0.0.0:7778` or by its LAN
   name sends that as the Host, so the call fails. Nudge fails silently. Before, it passed on the loopback far end
   alone. The default is fine. Running the address through `loopbackBase`, as the control tools do, would close this.

### Nit

2. No room-side test sends a proxied request to `/v1/shutdown`. `shutdown_share_test.go` only gained a loopback
   Host. `edge`'s table covers the predicate, so this is only coverage at the call site.
3. As the design says, a raw TCP tunnel (`ssh -L`, socat) still passes. That is security stage 2, and the doc
   comment on `LocalOperator` says so.

HUB DEPLOY OK and ROOM DEPLOY OK 7065bd6f
