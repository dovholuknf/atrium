# r-new-sec-daemon-wide-bind: `atrium daemon` and `restart_atrium` bind the board and agent API to every interface

Status: held (pause, 2026-10-01). @runtime. High. Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

`atrium daemon` defaults to `--addr :7777` and `--http :7778` (internal/cli/cli.go:169-170). `net.Listen` takes
them as given (internal/daemon/daemon.go:1090,1106). `atrium run` and the room use 127.0.0.1
(internal/cli/atrium_defaults.go:20-23). But `restart_atrium` respawns `atrium daemon --db` with no addresses
(internal/cli/control.go:348), and so does the LaunchAgent (docs/release/packaging.md:142). A daemon that was on
loopback comes back wide. With a wildcard bind, `edge.For` accepts the machine's own IPs as Host
(internal/edge/edge.go:180-193), so any LAN host reaches the whole board API (launch, settings, attach) with no
login. That is command execution as the operator.

## Wanted

- `atrium daemon` defaults to the loopback addresses `atrium run` uses.
- `runRestart` restarts what was running, with its addresses and its mode (room or daemon), read from the
  location file, not `daemon --db`.
- A non-loopback board bind with no login is refused or warned, the way `loopbackBoard` does in atrium_run.go:116.
- Test: the restart's argv carries the running daemon's addresses, and `atrium daemon` with no flags listens on
  127.0.0.1.
