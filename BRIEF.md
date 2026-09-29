# r-007: park idle cards (directors included), and quiet the directors' silent-stop noise

You are a worker for @runtime (handle `runtime-director-of-the-daemon-and-store`). Branch `claude/r-007`, worktree
`D:/worktrees/claude/atrium/r-007`, off `claude/main` dd0901c with the r-007 design merged in. Read `CLAUDE.md`,
`internal/daemon/CLAUDE.md` and `internal/store/CLAUDE.md` first, then `docs/keepalive-policy-design.md` in full,
then r-007 at the end of `docs/backlog-2.md`.

clint approved building this on 2026-09-29: "it needs to be able to start a director up or spool them down, yes,
that is an atrium task, yes build it now".

## The job, in STAGES. Report after each stage, then carry on

Commit at the end of each stage and `atrium_report` status `progress` to @runtime with the sha and the tests you ran,
so each stage can land on its own. Then go straight on to the next stage. Do not wait for an answer unless a stage
says so.

### Stage 1: the directors' silent-stop noise (FIRST, it is the noise reaching the orchestrator today)

Section 7, "The silent-stop notice for a resident director". clint chose option 2. In `stoppedSilently`
(`internal/daemon/a2a.go`), so the notice and the board's STUCK mark keep one definition:

- An `atrium:director` card is not silently stopped while any of its own workers is outstanding. Its workers are the
  cards whose `work_item.launcher_id` is the director (`internal/store/ledger.go`). Outstanding means a live runner
  (in the supervisor's `runners` map) in any status, including `done` at its prompt. A culled or dead worker is not
  outstanding. Once stage 3 exists, a parked worker is outstanding too, so leave a clear place for that.
- Once every worker has ended, the director's silent stop works exactly as today (one notice, the usual backoff).
- A worker's own silent stop is unchanged.
- Tests: `TestDirectorWithLiveWorkerNotSilent`, `TestDirectorAllWorkersEndedIsSilent`, `TestWorkerSilentStopUnchanged`,
  and one showing the STUCK mark (`stuckNow`) agrees with the notice in each case.

### Stage 1b: hold every message for a card in a new-context cycle (added 2026-09-29, from clint)

clint: while a card is in a new-context cycle (Ctrl+Alt+N, item 66: capture, clear, wake), every message from
another agent for that card must be HELD in its queue and delivered after the wake prompt, never typed in during
capture or clear. Today a say typed mid-capture can be lost, or land in the context that is about to be cleared.

- The cycle is `newContexts` in `internal/daemon/newcontext.go`: `begin` to `finish` / `fail` / `clear`. Add one
  predicate, e.g. `d.nc.holding(taskID)`, true from `begin` until the wake prompt has been typed.
- Every delivery path checks it and leaves the message queued: typing a say into the terminal (`handleMessage` in
  `messages.go`, the peer path in `peers.go`), `pendinginject.go` typing a queued say when the line clears, and the
  hook deliveries (`takeMessages` for the permission block in `onPermRequest` and for the Stop answer in
  `messages.go`). A hook delivery during the capture turn would put the message into the context about to be cleared,
  so it is held too. Do not reorder the permission chain: step 2 just finds nothing to take while holding.
- The new-context steps' own typing (`ncType`: the capture prompt, `/clear`, the wake prompt) is NOT held.
- When the wake prompt has been typed, release: the held messages are then delivered by the ordinary paths, after
  the wake prompt, never ahead of it. Kick `pendinginject` for the card. A cycle that fails or is cleared also
  releases, so nothing is stranded.
- The say's answer while holding is `queued`, never `terminal`, with a note that the card is starting a new context.
- Tests: a peer say during capture is not typed and not taken by a hook, and is delivered after the wake. The same for
  a say during the clear step. A failed cycle releases. The new-context steps' own typing is unaffected. The
  permission chain order is unchanged.
- Commit and report `progress` after it like the other stages. It lands on its own.

### Stage 2: item 91 comes before idle parking, and another worker builds it

Item 91 (a per-card HANDOFF file name, `docs/backlog-2.md` item 91) is being built by a SEPARATE worker named `91`.
Do not build it. Stages 3 and 4 below do not need it. Stage 5 does. When you reach stage 5, if item 91 is not on
`claude/main` yet, `atrium_report` status `question` and wait.

### Stage 3: the parking machinery r-007 needs (sections 4 and 5, and the part of section 1 it uses)

Build what idle parking stands on, and not the keep-alive rule changes (sections 2 and 3 stay unbuilt):

- The store: one migration at the END of `schema.go`'s slice, tolerant of already being there, adding `parked_at` and
  `human_at` / `human_via` to `task` (section 1's shape, section 4's `parked_at`). `Task` fields and scans.
- Section 1's `humanTouch` stamping, throttled, off the hot path, failures swallowed, from the sources in section 1's
  "What counts" table. Only the stamping: nothing in keep-alive reads it yet.
- Section 4's machinery WITHOUT the restart-time snapshot: `parkCard` (restore the status the card had, set
  `parked_at`, one `status-changed` event with `parked:true`, no new event kind), `unpark` (the launch lock, a shared
  `reopenRequest(t)`, resume by resume id, clear `parked_at`, `parked:false` event), the board mark and Resume menu
  entry if you can (the board is `internal/api/web`, keep it small), `sessionGone` false for a parked card, and the
  "first key resumes, attaching does not" behaviour.
- Section 5, the say: `sayGate` before `sessionGone` in both callers, `parked` answer with nothing queued, `wake=true`
  on the message body, `/tell`, `atrium_say` and `atrium tell --wake`, the operator's say resuming at once,
  `atrium_peers` marking parked cards. The woken card gets the text through the ordinary queued path, NEVER typed.
- A worker's `atrium_report` to its parked launcher resumes the launcher at once, as if `wake=true` (decided, question 8).
- Stage 1's rule now counts a parked worker as outstanding, and a parked card is never silently stopped.
- Tests as section 6 lists them for these parts ("Signal" for the stamping, "Parking" minus the restart snapshot
  ones, "Say").

### Stage 4: the orchestrator's own rule

clint: "parking makes sense ONLY when literally NOTHING else is running, since all things filter back through you".
The orchestrator card is `01a06dc7-0879-7068-b1eb-20f5c1979845`, alias `orchestrator`. It has no `origin:agent`, so
section 7's "clint's own cards are exempt" rule would exempt it. Build its rule as its OWN rule, not the
`atrium:park-idle` opt-in tag:

- The orchestrator card is marked by the tag `atrium:orchestrator` (add it to that card is clint's or the
  orchestrator's call, not yours: the rule does nothing until a card wears the tag).
- It parks only when every other part of section 7's rule holds AND no other card on the board has a live runner.
  Parked cards do not count as live. Fixtures and shells also do not count, since they are terminals and not work.
- Tests: parks with nothing else live, does not park while any other card has a live runner, parks once the last
  one parks or ends.

### Stage 5: idle parking itself (section 7), once item 91 is on claude/main

- The reaper tick rule in section 7, "When a card is parked", with `idle_park_after` (default 2 hours, floor 30
  minutes, `off`) as a daemon setting beside the keep-alive ones. Who is subject: `origin:agent` cards, plus clint's
  cards tagged `atrium:park-idle`, plus stage 4's orchestrator rule. Never fixtures, shells, throwaways or a card
  with a lent session in use.
- Idle-since is the latest of turn end, prompt and `human_at`. The handoff turn does not move it.
- The director's handoff at 50 minutes idle (or `idle_park_after` minus 70 minutes, whichever is later), through item
  66's capture step and item 91's per-card file name, then the park at `idle_park_after`. A capture that times out
  parks anyway and says so on the card. Decided: question 6 yes.
- Parking a card: snapshot status, `windDown` with exit keys, `parkCard`. Waking a director or the orchestrator
  queues item 66's wake prompt ("read your HANDOFF file") ahead of the waking message.
- Tests as section 7 lists them.

## Rules

- Do not reorder the permission chain in `onPermRequest`.
- Targeted tests while working (`go test ./internal/<pkg> -run '<yours>'`), with `ATRIUM_LOCATION` and
  `ATRIUM_DEBUG_INPUTLAG` cleared. Before each stage report, ONE run of `go test -p 4 ./internal/...` and say what
  failed, and whether it fails on `claude/main` too. If the board changed, `bash scripts/check-board.sh` with the
  headless sections you touched (`HEADLESS_ONLY=`, NODE_PATH `D:\worktrees\claude\atrium\merge\node_modules`).
- `docs/changes/r-007.md`: a `## Changelog` section (one entry starting `- **Title.**`) and a `## Test plan` section
  using `## @LETTER@. Title`, grown stage by stage. `pwsh scripts/fold-changes.ps1 -DryRun` must accept it.
- Mark in `docs/keepalive-policy-design.md` and r-007's status line what is built, stage by stage.
- Do NOT edit `CHANGELOG.md`, `docs/test-plan.md` or any CLAUDE.md.
- Commit on `claude/r-007` with one-line messages, under 30 words, meaningful, no "WIP", no trailer.
- No merge (except `claude/main` when stage 5 needs item 91, and only when @runtime says so), no deploy, no push,
  pull or fetch. Never touch the live room or the hub. No `restart_atrium`. Everything here is room-side, and clint
  picks when the room restarts.
- In Bash: no `;`, `&&`, `>` or `cd x && ...`, no `find`, no `git -C`. Do not `cd` out of your worktree.
- Mind your context. If you pass about 150k, commit, write `HANDOFF.md` in your worktree with the stage and what is
  left, and `atrium_report` status `progress` saying so.
- When all five stages are done, `atrium_report` status `done` with the head sha and the test results.
