# Factory status (orchestrator branch)

Autonomous backlog run while clint was AFK. Orchestrator handle `atrium-87300`. Everything below is on
`claude/orchestrator`, authored as clint (no Claude attribution), build + vet + `go test ./...` green at each
integration. Cherry-pick individual commits or merge the branch to `dovholuknf/main`.

## MORNING BRIEFING (overnight 2026-09-20 -> 21)

READ THIS FIRST when you wake. Live hub build `92dcf1e7bbaf8067`, room `claude-sg4` never restarted.

- **#1 bug FIXED and DEPLOYED: the attach-WS flicker.** Root cause was HUB-side, in the proxy: `roomHolding`
  waited for EVERY attached room to answer before forwarding a bare-id attach upgrade, so one slow/asleep room
  stalled the socket in CONNECTING, it got torn down and retried = the flicker, worse per room. Fix returns on the
  FIRST room that claims the (globally-unique) id. Shipped hub-only (`e09967e`, Go-only so the board-hash build id
  did not change; live-binary hash verified against the build). Reproduced + regression test in `internal/link`,
  done on an in-process repro, live atrium never touched. This UNBLOCKS multi-room. Bringing sgg/others online is
  now your call.
- **Board-share auth (your 3 answers, all implemented, hub-only live):** public zrok share auth = OIDC or basic
  (zrok updb) with the user/pass settable on the SETTINGS screen; a hub public share gets its creds from zrok updb
  only (hub grows no login); JWT-enroll-at-join implemented for both transports (join client is ROOM-SIDE/PARKED).
- **Expose-the-board REDO: three surfaces coexist, compare them live.** classic + menu 2 (collapsed rows) + menu 3
  (goal chooser). Both Mercurius and a cold Claude judge ranked menu 2 #1; its confirmed correctness + a11y bugs
  are fixed (public-share gate, id-collision, keyboard access, full OIDC fields). Pick a winner and delete the
  losers. New interview answers are in `docs/interview-log.md` (the new canonical record - read before any future
  interview so nothing is re-asked).
- **Mobile:** terminals tab no longer blank; font no longer gigantic; the list/terminal split is DECOUPLED (list
  collapses to a dropdown over a stable terminal, no tied resize). All hub-only, live.
- **Parked for ONE planned ROOM restart** (all committed, NOT deployed - a room restart kills your live sessions,
  so run it when you are present, detached): msg-truncation, say-caller, width-note removal, audit
  session-lifecycle events, ziti `join` transport flags, public-zrok gate, multi-pane echo (display-only keystroke
  fan-out, default OFF), the attach preamble removal + resize-churn guard, and the peer-typing race-fix (bus +
  relay: a peer message never types into a line you are mid-composing). See the batch section below.
- **Rooms:** held at ONE (sg4) all night per your rule. With the flicker fixed, multi-room is safe to try when you
  want it.

## Deployed live this session (hub-only, browser-verified where board JS)

Live hub build `cf99ce3f17b38cf7`. All of the below is on `claude/orchestrator` and running on the hub now, the
room was never restarted.

- **room-drop flood fix** — one room dropping no longer announces every card as a fresh arrival.
- **going-down scoped** — one room shutting down no longer makes the whole merged board say "atrium is restarting".
- **room picker restyle + rows lead with the name** — a session named `doer1` reads as `doer1`, not as its repo
  path. Popped-out header and alt-tab title lead with the name too.
- **rooms chip keeps the count** — a hub-link blink shows on its own indicator instead of hiding the room count.
- **survives a flapping room** — board `refresh()` is debounced, single-flight, aborts superseded fetches, and
  `api()` caps concurrency with backoff, so a flapping room cannot exhaust the tab's sockets
  (`ERR_INSUFFICIENT_RESOURCES`).
- **hung fetch cannot wedge the board** — a fetch that never returns can no longer pin the concurrency cap and
  leave `refresh()` stuck, which had blanked the whole board until a manual cancel. Proven with a headless browser
  test (`scripts/test-board-headless.js`, wired into `check-board.sh`).

## Parked for the next planned ROOM restart (room-side, NOT deployed)

These need a room restart, which interrupts the orchestrator and clint's live sessions, so they wait for a planned
window. Both are committed and revertible on `claude/orchestrator`.

- **message truncation fix** — long multi-line messages arrive intact as one bracketed-paste block.
- **atrium_say carries the caller** — the recipient sees who sent a message and the reply handle automatically, so
  agents no longer self-announce. Daemon-framed (not a hub prefix, which would carry operator authority).

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
- **split board vs room-link listeners** — `--addr` (the board a human uses) is loopback-guarded, while `--link`
  (what rooms dial) can bind wide (0.0.0.0) with a `--link-advertise` address and the same zrok/ziti/mtls
  transport options. This is what lets the board stay local while a second machine joins as a room.
- **message truncation fix** — a long multi-line message typed into a supervised terminal arrives intact as one
  bracketed-paste block instead of losing everything but the tail. Room-side, so it deploys on the next room
  restart.
- **css nits** — terminal icon set, path chip, fixtures toggle pill width, runners nav clip, rooms tab titles,
  disconnected rooms in the picker.
- **room-drop notification flood fix** — when one room disconnects while another stays, the board no longer
  announces every card as a fresh arrival. notify diffs on a tag-stripped stable id and re-seeds when the
  attached-room set changes. Board JS only, deployed hub-only on build `ccff08200936ba15` (2026-09-19).

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
