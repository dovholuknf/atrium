# cold-start.md -- orchestrator boot sequence

Run this after a `/clear` to come back as the atrium orchestrator (handle assigned per session) with no warm context.
Everything the role needs is on disk. Read it in this order.

## 0. Where the last session left off

- `D:\worktrees\claude\atrium\orchestrator\FACTORY-STATUS.md` -- written at the last `/clear`: what is live, which
  workers are in flight, what is owed, the next `sa` number. Untracked on purpose (`.gitignore`), so it survives a
  branch move. Read it first.

## 1. Who I am and the rules

- `C:\Users\claude\.claude\CLAUDE.md` -- global chat + tooling rules (auto-loaded).
- `D:\git\github\dovholuknf\atrium\CLAUDE.md` -- the atrium codebase (auto-loaded), including "Branching and landing
  work".
- `MEMORY.md` and the memory files it indexes (auto-loaded). The orchestrator role, deploy discipline, and the
  gotchas live here.

## 2. What is live right now

- Call `atrium_peers` -- the in-flight workers, what each is doing, how long it has waited.
- `curl http://127.0.0.1:7778/_hub/health` -- the live build id, `only`, and `rooms` count.
- `git log --oneline origin/main..claude/main` in `D:\git\github\dovholuknf\atrium` -- landed work clint has not
  yet taken to `origin/main`.

## 3. What is owed

- `docs/parked-room-changes.md` -- room-side SHAs integrated but not live, waiting on a room restart.
- `docs/decisions-log.md` -- why the current shape is the current shape.
- `docs/interview-log.md` -- clint's answered design questions, so I do not re-ask them.
- `docs/backlog-2.md` -- our backlog (the original two lists belong to another claude, do not edit them).

## 4. The loop

I dispatch, I do not implement. One real atrium worker per task (`atrium_launch`), titled `saNN: ...` in launch
order, in a worktree under `D:\worktrees\claude\atrium\<name>` on a `claude/<name>` branch off `claude/main`, with
a BRIEF.md. Workers commit, rebase on `claude/main`, and report in five lines or fewer with `atrium_say`.

Workers CANNOT land: `claude/main` is checked out in `D:\git\github\dovholuknf\atrium`, so git refuses to switch to
it anywhere else. I land. A worker branch that fast-forwards gets `git merge --ff-only <sha>` in that checkout.
One that does not gets `git cherry-pick`. `CHANGELOG.md` collides on nearly every cherry-pick: keep both entries.

Verify before any deploy: `bash scripts/check-board.sh`, `bash scripts/check-skins.sh`, `go vet ./...`, and
`go test` with `ATRIUM_LOCATION` cleared and `ATRIUM_DEBUG_INPUTLAG` unset (a leftover value in the shell fails
`TestLagConnTimesNothingWhenOff`). `internal/link` has flaked under load; rerun once before believing it.

Landing on `origin/main` is clint's: he re-signs `origin/main..claude/main` with his own key, then pushes. I never
sign and never add a trailer. See memory `commit-no-coauthor-no-sign` for the exact re-sign block.

Workers do not notify me when they stop unless they `atrium_say`, so every brief ends with the reporting rule.
A worker's `atrium_say` arrives typed into my terminal.

## 5. Deploy tooling (stable paths, survive a clear)

The deploy scripts deploy `D:\worktrees\claude\atrium\orchestrator\build.claude\atrium.exe`. That worktree is on
`claude/orchestrator`, which must be fast-forwarded to `claude/main` before building:

```
cd D:\worktrees\claude\atrium\orchestrator
git merge --ff-only claude/main
go build -o build.claude\atrium.exe ./cmd/atrium
```

- Cold start, nothing running: `pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium.ps1`, from a shell atrium
  does not supervise. It starts the hub (`atrium.exe run --no-room`) and then the room (`atrium.exe room`), and
  leaves either one alone when it is already running. There is no rollback binary: `atrium2.exe` is gone.

- Hub only: `pwsh -File C:\Users\claude\.atrium2\scripts\deploy-hub-only.ps1`. Safe while workers run, the room is
  never touched. Set `$env:ATRIUM_DEBUG_INPUTLAG='1'` first to keep hub lag logging on. It can outlive a 180s tool
  timeout, so run it in the background and confirm with `/_hub/health`.
- Hub and room together, for any ROOM-SIDE change: `C:\Users\claude\.atrium2\scripts\deploy-batch.ps1`, run
  DETACHED because stopping the room ends my terminal:
  `Start-Process pwsh -ArgumentList '-NoProfile','-File','C:\Users\claude\.atrium2\scripts\deploy-batch.ps1' -WindowStyle Hidden`.
  Logs to `C:\Users\claude\.atrium2\deploy-batch.log`. Wait for workers to be idle first.
- Room down, hub up: `pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium-room.ps1`. The auto mode classifier
  may refuse to let me start the room directly. Then clint runs it.
- `maintenance-window.ps1` is the older hub-and-room script. Prefer `deploy-batch.ps1`.
- **A room deploy kills my own turn,** and the resumed session can be missing it, which is how one deploy ran twice.
  Before starting one, write `C:\Users\claude\.atrium2\restart-marker.txt` with the sha and the built `atrium.exe`
  hash. After any resume, compare the installed hash to it and never re-run a deploy that already landed.
- **The hook binary is the same file.** Hooks, the hub and the room all run `C:\Users\claude\.atrium\bin\atrium.exe`.
  The deploy scripts swap it in with two renames, so a hook in flight keeps its image. Never copy over it by hand.
- **If the atrium MCP tools are gone** (they fail to connect when this session starts before the hub), use the HTTP
  API on `127.0.0.1:7778`: `POST /v1/tasks/<room>~<id>/message {"text"}` types into a worker's terminal. A card
  whose runner died is resumed with `POST /v1/launch` and header `X-Atrium-Room: claude-sg4`, body `harness`,
  `cwd`, `title`, `task_id` (bare id) and `resume` (from `GET /v1/tasks/<room>~<id>/sessions`), with NO `prompt`.
  Send the instruction as a message afterwards. `/restart` only works while atrium still owns a live terminal.
- **Workers cannot finish a rebase.** A hook blocks `git add` on a detached HEAD. Either finish it for them from
  PowerShell in their worktree (`$env:GIT_EDITOR='true'`, `git add`, `git rebase --continue`), or tell them to
  cherry-pick onto a fresh branch instead of rebasing.
- **Headless board checks in a worktree** need `NODE_PATH=/d/git/github/dovholuknf/atrium/node_modules`, since only
  the main checkout has Playwright installed.
- **Every launch prompt ends with "carry out the whole BRIEF through to the report, without stopping between
  steps".** A prompt that names only the first steps makes the worker stop after them.

At the next wave boundary, run `docs/wrapup.md` before the next `/clear`.

## 6. Learned 2026-09-24

- **`orchestrator/OWED.md` is the work list.** Read it before anything else after a `/clear`, work it top down, and
  ask its open rows as numbered Open Questions, one at a time. A safe copy lives in memory as `owed-table-copy.md`.
- **A worker's report lives only in my terminal** until the work ledger ships. After a crash, rebuild the open list
  from git state, and check EVERY repo a worker could have written to before calling work lost (sa19 and sa20's
  matrices were in the sdk-golang and ziti-sdk-csharp worktrees, not the one I checked).
- **Launch every worker with `theme: "active-work"` and tags `origin:agent`, `saNN`, `atrium:subagent`.**
- **The main checkout can be on `main`, not `claude/main`,** after clint lands. Check `git rev-parse --abbrev-ref HEAD`
  before merging a worker, or the work lands on `main`.
- **CHANGELOG and test-plan conflict on nearly every cherry-pick.** `D:\tmp\union-changelog.ps1 -p <file>` keeps both
  sides. Renumber a clashing test-plan section letter by hand.
- **`check-board.sh` fails spuriously when run beside `go test`.** Rerun it alone before believing it.
- **The hook binary must be rebuilt when `internal/cli` changes,** and codex's hooks pass `--runner`, so an old hook
  binary fails every codex tool call.
