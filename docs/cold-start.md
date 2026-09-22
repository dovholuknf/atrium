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
`TestLagConnTimesNothingWhenOff`). `TestRealSessionsKeepTheirText` is known noise. `internal/link` has flaked
under load; rerun once before believing it.

Workers do not notify me when they stop unless they `atrium_say`, so every brief ends with the reporting rule.
A worker's `atrium_say` arrives typed into my terminal.

## 5. Deploy tooling (stable paths, survive a clear)

The deploy scripts build from `D:\worktrees\claude\atrium\orchestrator\build.claude\atrium2.exe`. That worktree is
on `claude/orchestrator`, which must be fast-forwarded to `claude/main` before building:

```
cd D:\worktrees\claude\atrium\orchestrator
git merge --ff-only claude/main
go build -o build.claude\atrium2.exe ./cmd/atrium2
```

- Hub only: `pwsh -File C:\Users\claude\.atrium2\scripts\deploy-hub-only.ps1`. Safe while workers run, the room is
  never touched. Set `$env:ATRIUM_DEBUG_INPUTLAG='1'` first to keep hub lag logging on.
- Hub and room together, for any ROOM-SIDE change: `C:\Users\claude\.atrium2\scripts\deploy-batch.ps1`, run
  DETACHED because stopping the room ends my terminal:
  `Start-Process pwsh -ArgumentList '-NoProfile','-File','C:\Users\claude\.atrium2\scripts\deploy-batch.ps1' -WindowStyle Hidden`.
  Logs to `C:\Users\claude\.atrium2\deploy-batch.log`. Wait for workers to be idle first.
- Room down, hub up: `pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium-room.ps1`. The auto mode classifier
  may refuse to let me start the room directly. Then clint runs it.
- `maintenance-window.ps1` is the older hub-and-room script. Prefer `deploy-batch.ps1`.

At the next wave boundary, run `docs/wrapup.md` before the next `/clear`.
