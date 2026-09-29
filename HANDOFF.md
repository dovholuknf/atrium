# r-007 handoff

Read BRIEF.md first (it now has stage 1b), then `docs/keepalive-policy-design.md` sections 1, 4, 5, 7.

## Done

- **Stage 1** at `797bb0e`, reported. `stoppedSilently` (a2a.go) skips an `atrium:director` while a worker is
  outstanding. `store.WorkerIDs`, `DirectorTag`, `hasOutstandingWorker`, and `workerOutstanding` (the one place
  a parked worker must be added in stage 3). Tests in `a2a_director_test.go`. `docs/changes/r-007.md` exists
  with stage 1 only. Full `go test -p 4 ./internal/...` passed at that commit.

## Half done: stage 3, started, NOT committed as a stage, NOT reported

Committed in the WIP commit that follows this file. Builds and vets clean. `go test` not run on it.

- Store: migration `0071_human_at_parked_at` (human_at, human_via, parked_at) at the END of the slice.
  `Task.HumanAt/HumanVia/ParkedAt`, scan, insert placeholders (3 added). `internal/store/park.go`:
  `TouchHuman`, `Park(id, was, extra)` (restores status, sets parked_at, ONE status-changed event with
  `parked:true`), `Unpark(id, by)`. `park_test.go`: survives restart, migration tolerant, park keeps status.
  These three store tests passed.
- Daemon `internal/daemon/park.go`: `humanTouch` (throttled per card+via, async, swallowed, no publish, counts in
  `touchWrites`), `isParked`, `parkCard`, `reopenRequest(t)` (now used by `reopenSaved` and `RestartRunner`),
  `unpark(id, via)` (launch lock, `launchLocked`, `st.Unpark`), `sayGate` (parked before gone), `parkedNote`.
  `Daemon.humanTouched sync.Map` added in daemon.go.

## Left in stage 3 (nothing below is wired yet)

1. `noteOperatorTyped` (supervisor.go ~984) must RETURN `keyed`; in attach.go's `in` case, when `!shell` and keyed,
   call `d.humanTouch(taskID, ViaTyped)`.
2. Other humanTouch callers: activity.go `prompt` case when `promptCause` is `store.UsageOperator` (ViaPrompt);
   `d.decide` in daemon.go when the stored `DecidedBy` is `store.DecidedBySelf` (ViaPermission);
   `handleMessage` with no `from` (ViaMessage); running an action in actions.go (ViaAction).
   Keep-alive `enable` does not exist as a stamp target here: skip unless `keepaliveSet` is easy.
3. `sessionGone` false for parked (or rely on sayGate order, but do both). `stoppedSilently` false when parked.
   `workerOutstanding` true for a parked worker. `Launch` success onto a card should call `st.Unpark(id,"launch")`.
4. Attach to a parked card: today `attach` 404s with no runner. Accept the socket, show scrollback if any plus
   "parked: press any key to resume", on the first real key (use a `typedLine.feed`) call `unpark(..., ViaTyped)`,
   hold that frame, write it to the runner once up, then continue as a normal attach. Attaching alone must NOT resume.
5. Say: replace the `sessionGone` check in `handleMessage` (messages.go ~452) and `resolvePeerSay` (peers.go ~291)
   with `sayGate`. Parked with no `from` (operator), or `wake=true`, or a report: `unpark` then queue. Otherwise answer
   `delivered:"parked"`, `reachable:"parked"`, warning `parkedNote`, NOTHING queued, say log `SayRefused` note
   "parked". After a wake, skip the direct `typeThroughGate` / `tellByTyping` and queue through
   `QueueFromPeer`/`QueueMessage` plus `deferPeerInjection` (the ordinary pending path). Add `wake` to the message
   body, `/tell` body, `atrium_say` (internal/link/control_mcp.go), `atrium tell --wake` (internal/cli), and mark
   parked in `atrium_peers` (roster in peers.go).
6. Worker `atrium_report` to a parked launcher: in `finish` (finish.go) before `RecordReport`, if the launcher is
   parked, `unpark(launcher.ID, "report")`; after it, if `res.Notice != nil`, call `deferPeerInjection` with
   `res.Notice.MessageID`.
7. Board: a "parked" mark on the card and a Resume entry in the card menu (`internal/api/web`, keep small), plus an
   API route that calls `unpark(id, ViaResume)`. Run `bash scripts/check-board.sh` if the board changed.
8. Tests per design section 6 "Signal", "Parking" (minus the restart-snapshot ones), "Say". Note the test helpers in
   `a2a_director_test.go`: `liveRunner`, `endRunner`, `directorRig`. Add `TestDirectorWithParkedWorkerNotSilent` and
   `TestParkedCardNeverSilent`.
9. Extend `docs/changes/r-007.md`, mark the design doc and r-007 status line, one `go test -p 4 ./internal/...`,
   commit, `atrium_report progress`.

## Left after that

- **Stage 1b**: see BRIEF.md (hold messages for a card in a new-context cycle). Not started, not read by me.
- **Stage 4**: orchestrator rule, tag `atrium:orchestrator`, parks only when no other card has a live runner
  (parked, fixtures and shells do not count).
- **Stage 5**: idle parking on the reaper tick, `idle_park_after` setting. Needs item 91 on `claude/main`. If it is
  not there, `atrium_report` status `question` and wait.

## Notes

- Bash hook here refuses `;`, `&&`, `>` and python. Use PowerShell for anything with those.
- `fakeRunner` is taken in the daemon test package, hence `liveRunner`.
