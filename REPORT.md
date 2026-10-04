# f-forge-access report

Branch `claude/f-forge-access`, based on claude/main 5fc5b390.

## Built

- `internal/requirements`: a `forges:` section, `gh|bb|glab: { host, scopes }`. Unknown CLI, missing host, a path as host,
  a token-shaped scope and unknown keys are refused. `atrium.requirements.yaml` now names `gh` on github.com with
  `repo` and `read:org`. Nothing in the room-check script reads it yet.
- `internal/store/forgecfg.go`: per room `forge.<tool>.host` and `forge.<tool>.cmd` settings. The command must be a bare
  name (no path, no arguments), the host a plain host name.
- `internal/daemon/forgeaccess.go`: the check, the alert and its message, the open set.
- `POST /v1/preflight` takes `forges: [{tool, host, scopes}]` and answers `forges` keyed `tool@host` with state
  `ok`, `not_installed`, `logged_out`, `missing_scope` (names the scope), message and fix. The status command is fixed
  per tool: `gh|glab auth status --hostname <host>`, `bb auth status`. The command NAME comes from the room's setting,
  never from the body. Output is read only to look for a `Token scopes:` line and is not returned.
- `POST /v1/forge/check` (human listener): asks every forge the room is configured with, for the settings button.
- Alert: the daemon broadcasts `forge-access` (once per change, `cleared` when a later check is ok). The board raises it
  through the existing toast, bell and desktop notify path. The open set is also in `GET /v1/settings` as
  `forge_access`.
- Configuration: a "forge logins" block in the room settings (cog), host and command name per CLI, a check now button
  and the open messages.

## The runtime hook

`func (d *Daemon) RaiseForgeAccess(tool, host, detail string)` in `internal/daemon/forgeaccess.go`. Wiring the forge
package is `d.RaiseForgeAccess(e.Tool, e.Host, e.Detail)`. Detail containing "not found", "executable file", "not
installed" or "no such file" gives not installed, anything else logged out. An unknown tool is ignored.

## Decisions

- A missing scope is the same alert as logged out, with the scope named and `gh auth refresh --scopes` as the fix.
- A fine-grained token prints no scope line, so it is ok and never a failure.
- `bb auth status` and `glab auth status --hostname` are my reading of those CLIs, unverified against real ones.
- The alert is the room's own board alert, not a hub growler. Growlers are hub-derived from cards and room health, and a
  growler reason would need hub work out of scope here.
- An empty host in the settings means the room does not need that CLI, so check now skips it.
- A repeat of an identical open alert is not broadcast again, so a retried action does not toast every time.

## Not done

- No `scripts/room-check.ps1` row calling the new preflight key. The script still has no gh row.
- No hub-level aggregation or growler for forge access.
- No board section added to test-board-headless.js. I checked the UI by hand against a preview daemon instead.
- Bitbucket and GitLab are named by the check only, nothing calls them. No polling anywhere.

## Tests

- `go test ./internal/requirements/` ok (new TestForgesParseAndRefuse).
- `go test ./internal/daemon/ -run "Forge|Preflight"` ok (ok, logged out and cleared, not installed, missing scope, no
  scope line, command override and refused names, unknown tool and bad host run nothing, RaiseForgeAccess, scope parse).
- `go test ./internal/api ./internal/store` ok, and go vet clean on the four packages.
- `HEADLESS_ONLY=bootClean node scripts/test-board-headless.js` ok.

## Pictures (notes/)

- `f-forge-access-config-before.png` and `f-forge-access-config-after.png`: room settings dialog, before and after
  (the new block is below the fold there).
- `f-forge-access-forge-block-after.png`: the forge logins block.
- `f-forge-access-alert-after.png`: the alert toast from a real preview daemon with gh not installed.
