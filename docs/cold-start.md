# cold-start.md -- orchestrator boot sequence

Run this after a `/clear` to come back as the atrium orchestrator (handle assigned per session) with no warm context.
Everything the role needs is on disk. Read it in this order.

## 1. Who I am and the rules

- `C:\Users\claude\.claude\CLAUDE.md` -- global chat + tooling rules (auto-loaded).
- `D:\git\github\dovholuknf\atrium\CLAUDE.md` -- the atrium codebase (auto-loaded).
- `MEMORY.md` and the memory files it indexes (auto-loaded). The orchestrator role, deploy discipline, and the
  gotchas live here.

## 2. What is live right now

- Call `atrium_peers` -- the in-flight doers, what each is doing, how long it has waited.
- `curl http://127.0.0.1:7778/_hub/health` -- the live build id, `only`, and `rooms` count.

## 3. What is owed

- `docs/parked-room-changes.md` -- room-side SHAs integrated but not live, waiting on a room restart.
- `docs/decisions-log.md` -- why the current shape is the current shape.
- `docs/interview-log.md` -- clint's answered design questions, so I do not re-ask them.
- `docs/backlog-2.md` -- our backlog (the original two lists belong to another claude, do not edit them).

## 4. The loop

Spawn a real atrium doer per task (`atrium_launch`) in a worktree under `D:\worktrees\claude\atrium\<name>` on a
`claude/<name>` branch off `claude/orchestrator`, with a BRIEF.md and CLAUDE.local.md. Doers commit and report SHAs
tagged HUB-ONLY or ROOM-SIDE. I integrate onto `claude/orchestrator`, verify (`bash scripts/check-board.sh`, build,
`go vet`/test with ATRIUM_LOCATION + ATRIUM_SHARED_LOCATION unset), and deploy HUB-ONLY. ROOM-SIDE is parked.

## 5. Deploy tooling (stable paths, survive a clear)

- Hub-only: `pwsh -File C:\Users\claude\.atrium2\scripts\deploy-hub-only.ps1`
- Hub + room maintenance window (detached, kills my terminal, resumes cards): `...\scripts\maintenance-window.ps1`

At the next wave boundary, run `docs/wrapup.md` before the next `/clear`.
