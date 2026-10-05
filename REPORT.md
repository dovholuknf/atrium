# m-new-deploy-queue report

## Done
- `internal/deployready/queue.go`: `Checker.Queue(hub, rooms)` lists landed non-merge commits not in the hub's running
  commit or a room's, that touch shipped code. Each gets sha, subject, item (from its changelog path) and needs
  `hub`, `room` or `both` plus the rooms behind. Hub-only code (internal/hubstore) needs the hub alone.
- Running sha: rooms had none, only a `Version` string. The hello now carries `Commit` (`link.Room.Commit`, set from
  `runningCommit()` in `internal/cli/version.go`). The hub keeps it and shows it as `commit` in `Hub.Rooms()`. The hub
  reports its own via `Proxy.SetRunningCommit`, falling back to the installed binary's commit.
- `GET /_hub/deploy-queue` (`internal/link/deployqueue.go`), JSON, or `?format=md` for the HANDOFF table.
- Board: the deploy-ready dialog (the pill's click) shows the queue (`js/deployready.js`, `css/deployready.css`).
- `deploy-ready.ps1` and `deploy-batch.ps1` call `Write-DeployQueue` (live-common.ps1) before changing anything. It
  prints the queue and saves `DEPLOY-QUEUE.md` under the atrium dir for HANDOFF.
- Tests: `internal/deployready/queue_test.go`, two in `internal/link/deployready_test.go`.
- Changelog `changelog/fabric/2026-10-04-m-new-deploy-queue.md`. Item file status set to BUILT.

## Left
- A room built before this change sends no commit, so it shows under `unreported` and is taken as behind the hub
  until that room is deployed once. No design question for clint.
- HANDOFF itself is not auto-edited. The script saves the table, and whoever writes HANDOFF pastes it in.
- Board test (test-board-headless.js) not extended. Not run, only `node --check` on the JS.

## Verify
- `go test ./internal/deployready` and `go test ./internal/link -run DeployQueue` pass.
- `./internal/link` has 57 failures with and without this change. Same set, compared by name against a clean worktree at
  7a0522cc. `./internal/cli` passes.
- After a hub and room deploy: `curl http://127.0.0.1:7778/_hub/deploy-queue?format=md`.
