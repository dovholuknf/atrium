# r-new-review-f16ff0c8. Fix for the 5edc1821 review: a listener answers only its names

Status: open, three lows. Filed by @review 2026-09-30. Read-only review of f16ff0c8 (merge of
`claude/r-review-fixes`), the fix for finding 1 of `r-new-review-5edc1821.md`. Owned by @runtime.

The medium is closed where atrium can know the name. `Shared` is gone, and every listener goes through `Named`
(`internal/edge/edge.go`), which answers loopback names plus the names it is given plus `$ATRIUM_HOSTS`:

- a zrok share, board or lent session, answers its `FrontendEndpoints` (`overlay_native.go:266`,
  `overlay_guest.go:407`, and the hub's share in `atrium_run.go`)
- a board bound to one address answers that address, and one bound to every interface answers each interface
  address and the host name (`For`, `machineNames`)
- a ziti service with `$ATRIUM_HOSTS` unset falls back to `Unnamed`, origin checks only, and says so once in the log

A rebound page carries its own name in `Host` and matches none of those. The loopback hub still refuses one (probed,
`Host: attacker.example:7778` answers 403, `localhost:7778` answers 200). `go vet` is clean on edge, daemon, cli and
api, and no `edge.Shared` caller is left.

## 1. Low. A ziti board with no `$ATRIUM_HOSTS` is still open to rebinding, and only the log says so

`zitiEdge` and `zitiBoardEdge` choose `Unnamed` when the variable is empty, which is the default. A ziti client
resolves the service to an intercept address on the tunnel, and a rebound name pointed at that address gets the same
same-origin page the audit described. The log line is written once at start, which nobody reads. Put it on the
overlay panel's ziti row as a standing warning, or refuse to start the ziti board without a name and say which
variable to set. The first costs nothing to anyone already using ziti.

## 2. Low. The names are fixed when the listener starts

`machineNames` reads the interfaces once, when `For` wraps the handler. An address that arrives later, such as the
ziti tunnel interface coming up after the daemon or a DHCP renewal, is refused with 403 until a restart. That is safe,
but it looks like the board broke. Read the interfaces on a miss, bounded to once a minute.

## 3. Low. A proxy that rewrites `Host` hides the attacker's name

The host check sees only what arrives. `zrok access private` on the reading machine is a local proxy on
`127.0.0.1:9191`. If it forwards the browser's `Host`, a rebound page is refused here. If it rewrites `Host` to the
share's frontend name, which `Named` allows, the rebound page passes. Which one zrok does is not pinned by any test
here. The new test-plan row 5a checks a rebound `Host` against the share itself, not through the access proxy. Add
that case to 5a. If the proxy rewrites `Host`, the board login is the only thing in the way.

## Carried over

Low 1 of `r-new-review-3c354ed4.md` is still open. The hub's name routing still treats a websocket upgrade as a read.
This merge's `cardnames.go` change is the room side, where a missed name on an upgrade goes on to the attach handler,
and it does not touch that.

## Tests

`go test ./internal/edge/` passes. `./internal/api/ -run 'Name|Alias|Handle|Attach'` and `./internal/cli/ -run
'Hook|Refused|Edge|Board'` pass. `go vet` on the four packages is clean.
