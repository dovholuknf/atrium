# Factory status (orchestrator branch)

Autonomous backlog run while clint was AFK. Orchestrator handle `atrium-87300`. Everything below is on
`claude/orchestrator`, authored as clint (no Claude attribution), build + vet + `go test ./...` green at each
integration. Cherry-pick individual commits or merge the branch to `dovholuknf/main`.

## Shipped and integrated (mergeable)

- **control-mcp phase 1 + 2** — one HTTP MCP server on the hub replaces the per-session `atrium-control.exe`
  children; phase 2 adds `restart_atrium` (hub forwards, room restarts itself), restart-a-session (exit + resume
  onto the same card), launch `BRIEF.md` on the room, and `ATRIUM_ROOM` export.
- **fixtures on/off toggle** — the board pill enables/disables a fixture without deleting it.
- **terminal theme through launch** — `atrium_launch` takes a theme so a session comes up in the right palette.
- **event sink phase 1** — `EventSink` interface, a rolling-JSONL `file` cold sink, and an `event_sink` setting,
  default `db` so nothing changes unless opted in.
- **db-shrink** — new dbs open in incremental auto-vacuum and a timer reclaims freed pages, so the file shrinks
  instead of sitting at its high-water mark.
- **zrok board share** — the hub can optionally serve its board over a zrok share for remote access, with a failed
  share isolated so it never takes the local board down.
- **restart concurrency fix** — a security review found a CRITICAL race (two restarts/launches on one card could
  braid one transcript from two). Fixed with a keyed mutex per card and resume id, duplicate-ask dedup, wind-down
  waiting for the kill, explicit store close, and a concurrency test. This is why the branch is safe to merge.

- **restart wheel item** — a restart in the terminal cog menu that exits and resumes the session on the same card.
- **`--isolated` room flag** — a second/throwaway room on a machine keeps off the shared hooks file (the hazard the
  verify run hit and remediated).
- **event sink phase 2** — the db event window can be bounded (opt-in, default unbounded), so with db-shrink the
  operational database plateaus instead of growing forever.

## Verification

A throwaway atrium2 (isolated ports/dirs/db) confirmed via headless Playwright: the board loads clean, the fixtures
on/off toggle persists, and a launched session comes up in its theme. The phase-2 restart code passed an
adversarial security review, which found and got fixed a critical concurrency race. See VERIFY-REPORT.md.

## Deploy plan (when you are back)

- Hub-side (control MCP, zrok share, board assets for fixtures/theme): deploys on a HUB restart alone, room stays
  up. Use `C:\Users\claude\.atrium2\start-atrium2.ps1` patterns; build atrium2 from this branch first.
- Room-side (restart_atrium receiver, ATRIUM_ROOM, launch brief, event sink, db-shrink): needs ONE room restart,
  which interrupts the live sessions (they resume). Do it from an unsupervised shell via the detached script.
- After deploy, the mcp.json flip to the http `/_hub/mcp` entry becomes valid again (needs the hub up AND
  ATRIUM_ROOM exported, both true post-deploy). It is currently reverted to the stdio child for v1/rollback safety.

## Needs your decision

- **sgg second room (Goal B)** — blocked. sgg is a bare Windows amd64 box (ssh as `localai`), no overlay client.
  The sgg room must dial the hub link (loopback here), so it needs a zrok/ziti client on sgg, which is a
  credential/account step only you can do. Goal A (remote board via zrok share) is done and integrated. See
  `sgg-STATUS.md`.

## Running while you were out

- Room watchdog: scheduled task `atrium2-watchdog`, every 2 min, restarts hub/room if down (idempotent,
  room-safe). Remove with `schtasks /delete /tn atrium2-watchdog /f` when you no longer want it.
- The live instance was never restarted by the factory; your sessions stayed up.
