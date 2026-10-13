# r-agent-loop audit

Read-only audit of claude/main at c5940c34. SHAs are the first commit that introduced the symbol (`git log --reverse -S`). Nothing was run; this is read from code.

## Summary

| # | Item | Verdict |
|---|------|---------|
| 1 | Launch text carries the loop | done (the one-line part is not on main) |
| 2 | Report goes to the launcher's card | done |
| 3 | Next prompt to a stopped worker resumes it | partial |
| 4 | Accept is one verb | partial (the closest to absent) |
| 5 | Runner-native dialog shown with its text | partial |
| F1 | r-workers-never-auto-removed | done, with a wrinkle |
| F2 | r-close-leaves-worktree | done for `close`, not for `cull` |

## 1. Launch text carries the loop: DONE

- Shipped: 1f9f03ed (atrium_done/atrium_blocked and the launch text), d27405b5 (wait for every command), 05a94832 (launcher-not-operator wording).
- `internal/daemon/launch.go:1719` `LaunchEndingLine`: "Before you end your turn, tell your launcher either: atrium_done <sha or artifact> ... atrium_blocked <up to 50 words> ...", "Report to your launcher, never to the operator, unless your launcher is the operator", "A launcher's 'stop' is a valid end". Lean workers get the same text at `internal/daemon/lean.go:160-169`. The same constant is appended by `BriefPrompt` (`launch.go:1729`) and kept equal to `link.reportLine` by a test.
- Not on main: a rule that the report is ONE line. The 50-word cap on blocked is the only length rule, and `done` takes a free `recap`. grep for `r-report-is-one-line` or any such rule finds nothing in code. That is the item you said is landing.

## 2. Done or blocked goes to the launcher's card: DONE

- Shipped: f5cf529f (launched sessions report to their launcher), 1f9f03ed/4c267edc (atrium_done/atrium_blocked, one end message), d55e1c87 (no second notice for an end the worker already reported; `endonce.go`).
- `internal/daemon/finish.go:413-437`: the report is a notice queued to `launcherOf(task)` in the same transaction as the ledger write, or relayed if the launcher is on another room. `finish.go:438-449` unparks a parked launcher. `endonce.go:38` drops a worker's `atrium_say` that repeats its end.
- A report that reaches nobody is held for the orchestrator, not typed at the operator: `owed.go:391` `orphanReport` (f5cf529f lineage, `NoticeNoLauncher`).
- Gap, minor: this is enforced by the worker obeying the launch text for its final reply. A worker that ends with a plain terminal reply still lands in the operator's terminal, and the only room-side answer is the silent-stop notice (`a2a.go:441-462`, 9b47e39f). There is no suppression of that reply.

## 3. A launcher's next prompt to a stopped worker resumes it: PARTIAL

- Shipped: f1aa30ac (says kept on the card for parked, done and dead cards; wake on done), 0f958aa6 (family say wakes a PARKED card), `wake` flag on atrium_say.
- Works: `peers.go:318` `wakeGone := gate == sayGone && wake && resumable(target)` calls `keep.go:156` `wakeGone`, which relaunches from the resume id, queues, and injects once the runner is ready. `peers.go:332-334`: a say from the launcher or a worker of the same family to a PARKED card sets `wake = true` itself.
- Missing: that family auto-wake is only for `sayParked`. A worker that reported done is exited 5 s later (`exitonreport.go:38`, 1bcf830e), which makes it `sayGone`, not parked. So the launcher's plain `atrium_say` to a finished worker falls to `peers.go:319-330` `keepOnCard`, and the sender gets `queued:true, delivered:"queued"` and "kept on X: it has no running session ... delivered the next time it runs" (`keep.go:21`). That is the "queued on a dead session" outcome in the item. It resumes only if the launcher remembers `wake:true` (`link/control_mcp.go:848,922`).
- Change: in `internal/daemon/peers.go` around line 316-318, add `|| d.fromFamily(from, target)` to `wakeGone`'s wake condition, mirroring line 332. `fromFamily` (`park.go:292`) already says launcher-or-worker. Keep the `resumable` guard, and keep the plain queue for a non-family sender. Update `keptNote`/the tool description so `wake` is described as needed only from non-launchers.

## 4. Accept is one verb: PARTIAL

What exists, as two overlapping verbs:

- `atrium_cull` (e23ca803, abd00d42, c6578820): `internal/link/control_mcp.go:368`, `internal/daemon/cull.go:98` `CullProved`. For a done, agent-launched, `atrium:subagent` card whose branch is merged: exits it, `git worktree remove`, deletes its own branch, removes BRIEF.md and scratch dirs (`reclaim.go:95,112`), marks the work item accepted (`cull.go:206` `culled` calls `AcceptMerged` and `ArchiveCulled`, so the card leaves the board).
- `atrium_close` / `atrium close` (317d3b51, d21ac7e7): `internal/api/close.go`, face in `internal/cli/close.go:235` ("closing X frees N"). Frees the card's recorded inventory (worktrees, branches, ports, overlays, review folder), asks keep/stash/delete per dirty worktree, and tags the card `atrium:closed`.

What is missing against the item:

- They are two verbs, and neither is "accept". Cull needs the branch merged into claude/main and is the launcher's word. Close is the operator's and works from the card's inventory, which is written by `open` (294b5891), not by a launch, so for a launched worker it frees little.
- **Disk freed is not reported by cull.** `CullResult` (`cull.go:73`) has no bytes field and `cullOutput` (`control_mcp.go:1888`) has none either. Only `close` reports (`close.go:92,241`).
- **node_modules**: cull's `removeWorktree` (`cull.go:391`) is a bare `git worktree remove`. It deletes ignored trees in the simple case but has none of the leftover cleanup d21ac7e7 added to `close` (`internal/api/worktreeleft.go`, long-path delete on Windows). On the case that bit clint (0.41 GB node_modules on Windows) a cull would report `Kept: "git kept the worktree: ..."` or leave the folder.
- **Prune merged branches**: cull deletes only the card's own branch (`cull.go:175`, `deleteMergedBranch`). It does not prune other merged branches.
- **Wire name**: there is no `accept` tool or CLI verb. Auto-cull on merge is off by default (see F1), and `atrium_launch_worker` cards are only exited on merge, not reclaimed (`mergedcull.go:30,141`).

## 5. Runner-native dialog shown on the card with its text: PARTIAL

- Shipped: 82272752 / a81bf521 (permission hook: `needs-permission` status, `PermissionRequest` carries Tool, Command and Details at `permwait.go:24-38`, shown on the card), 9afb8074 (launch-prompt and terminal-menu escalations read from the screen).
- `internal/daemon/launchstuck.go`: folder trust, login, update and model-switch are recognised by name, and any other Claude Code selection menu is shown with its TITLE as the escalation text (`menuFooter` at line 60, `PromptOther`, `launchstuck.go:202`). It is derived each tick and cleared when answered. Both card paths call it (`a2a.go:705`, `a2a.go:768`). The web board draws it (`api/web/js/stack.js:593-626`).
- Gaps: (a) A native tool-approval dialog is shown through the hook path as a permission request, not through the screen scan: `launchstuck.go:179` skips the scan while a permission is pending. If the hook did not fire (for example approval mode set outside atrium's hooks) the screen scan skips nothing, but only a selection menu with the footer is matched, so a yes/no approval drawn without that footer is not shown. (b) The launcher is never told that its worker is stuck on a dialog: "A launcher never answers a worker's permission" (`a2a.go:690`), and the launcher notice list covers silent stop and stuck tool, not a menu or permission. Not verified end to end.

## F1. r-workers-never-auto-removed: DONE, with a wrinkle

- Shipped: 02b28057. `mergedGrace` (`mergedcull.go:45-64`) treats unset as off (`store.DefaultMergedCullGrace`), so `Merged()` marks nothing and only `atrium_cull` or `atrium_exit` removes a worker.
- Wrinkle: a card launched with `atrium_launch_worker` (0856d734, lifetime `work`) is still EXITED on merge regardless of the grace (`mergedcull.go:30,141` `exitMergedWork`). That exits the runner and keeps card and worktree, so nothing is removed from disk. If the item means "no process is ever ended automatically", this is the exception.

## F2. r-close-leaves-worktree: DONE for close, NOT for cull

- Shipped: d21ac7e7, a1d02b22 (merged in 0c03aaf0). `removeLeftover` in `internal/api/worktreeleft.go` and a re-measure before removal (`close.go:261-265`) make the row freed only when the path is gone, size includes ignored files, and the card carries `atrium:closed` so it leaves Terminals.
- The same fix is not in `daemon.removeWorktree` (see 4).

## Smallest next change

**Make the launcher's say to a finished worker resume it (point 3).** It removes the most noise: today the launcher gets "queued", the worker sits dead, the launcher asks again with `wake` or goes to the operator. One worker, a few lines plus a test:

- `internal/daemon/peers.go` (the `wakeGone` condition near line 318): also wake when `d.fromFamily(from, target)`.
- `internal/daemon/keep.go` (`keptNote`, line 21): a say from the launcher is never kept for a resumable card, so the note stays only for non-family senders.
- `internal/link/control_mcp.go:848` (the `wake` description): say a launcher need not pass it for its own worker.
- Test beside `internal/daemon/park_family_integration_test.go`: a done, exited, resumable worker and a say from its launcher without `wake` ends with `wakeLaunch` called and the text injected.

Next after that, by size: give `cull` a bytes-freed field and the `removeLeftover` cleanup (`cull.go`, `link/control_mcp.go:1888`, `api/worktreeleft.go` shared), then fold cull and close into one accept verb.
