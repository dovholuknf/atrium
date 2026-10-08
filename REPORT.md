# r-graceful-room-restart report

A room restart now asks its working cards to wrap up first. Design and test plan: docs/changes/r-graceful-room-restart.md.

- `internal/daemon/restartwrap.go`: the wrap-up. Working cards get a labelled prompt through the cycle's typing gate,
  are waited on for `atrium ready` or idle, get a restart wake (own wake kept), and the unanswered are named in the log
  and on the card's history. `POST /v1/restart-wrap` runs it alone.
- `internal/daemon/shutdown.go`: `/v1/shutdown` wraps up first by default, `?now=1` skips it and ends one waiting.
  `ready.go` accepts the ack with no handoff file. `internal/store/restartwrap.go`: setting `restart_wrap_wait_s`
  (300 default, 10 to 3600), also in the settings API.
- `internal/cli`: `atrium stop --now`, and the hub-forwarded restart wraps up before it spawns the restarter, falling
  back to the old park-and-refuse when `immediate` is set or the call fails. `restart_atrium` takes `immediate`.
- `scripts/live`: `Stop-Room` waits wrap setting + 45 s before forcing, `-Immediate` on deploy-batch and stop-atrium.
  Not run here (no pwsh on this machine).

Decisions: idle, dialog and finished cards are not prompted or woken. A card in a context cycle is skipped. The
standalone `atrium control` restart tool uses `now`. `atrium_deploy` itself is unchanged, its restart is the script.

Tests: new `restartwrap_test.go` (ready, idle, timeout, idle card left alone, own wake kept, shutdown graceful, `now`,
`now` ending a wait, setting). daemon wrap/shutdown/cycle/hold tests, store, cli, api run 3 times with -count=1, all
pass. Full daemon run fails only TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP, which fails the same on main.
