# Review of df724652 (@runtime: the hub's hosts setting, no wildcard over a public suffix)

Reviewed by @review, 2026-09-30, from `git diff claude/main...df724652` (`internal/edge/edge.go`,
`internal/link/hosts.go`). Hub side. It changes the rebinding guard, so I read it closely. `go vet` on edge and link
passes. `go test ./internal/edge/` passes, and so does `go test -run 'Host|Edge|Named' ./internal/link/`. A scratch
probe of the suffix rule was run and removed.

## What holds

- **The public-suffix rule.** Scratch results from `CheckNames`:
  - Refused: `*.co.uk`, `*.duckdns.org`, `*.github.io`, `*.ngrok.app`, and `*.internal` (no dot).
  - Taken: `*.shares.zrok.io`, `*.zrok.io`, `*.me.duckdns.org` (a single user's own subdomain), and
    `*.corp.example.com`.

  So the e25a2c2f low is closed. The list `x/net/publicsuffix` uses includes the private section, which is what
  catches duckdns and github.io.
- **Exact names.** A name holding `*` outside a leading `*.` is refused, and every refusal is logged once.
- **No lock on the request path.** The setting is an `atomic.Pointer[hostSet]`, swapped whole, and read on the
  request path with no lock. It is applied before any listener starts (`LoadExtraHosts` in `serveAtrium`). A bad
  stored value applies nothing, which leaves the names the listeners had.
- **PUT is bounded and recorded.** It is loopback only, takes at most 64 KB, 100 hosts and 253 characters each, and
  writes an audit row. An ignored entry is saved as typed and answers nothing. GET is open, which shows only names.
- **The refusal message** is JSON-encoded now, so a Host header can no longer break the JSON.

## Findings

### Low

1. **"Loopback only" holds only when a share does not arrive from 127.0.0.1.** `serveHosts` gates PUT on
   `loopbackRemote(r.RemoteAddr)`, like launch caps and the notify command (f-024). The in-process SDK listeners
   (`overlay_native.go`) give a non-loopback address. A share run by a separate `zrok share` process proxying to
   `127.0.0.1:7778` arrives from loopback, so anyone using that share passes the gate. The exposure is small, since
   a share user already holds the whole board and widening the names helps only a page that controls a name's DNS.
   But this list is the guard the gate exists to protect. Is clint's share the SDK listener or the CLI? If it is
   the CLI, the f-024 gates all have the same gap, which is a design item, not this commit's.
2. **The setting covers the hub process only.** `extra` is shared by every `Named` listener in the hub process,
   which is what the setting wants. A room process does not load it, so a room's own listeners still answer only
   `$ATRIUM_HOSTS`. Should the gear say "the hub answers these", so nobody expects a room's loopback listener to
   take them?

HUB DEPLOY OK df724652
