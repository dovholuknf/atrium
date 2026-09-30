# r-hold-notices. Launcher notices must not be typed into the orchestrator's terminal

Status: built on claude/r-hold-notices. Owned by @runtime. Asked for by clint 2026-09-30 (BRIEF item 3a).

The orchestrator's terminal is where clint reads, and every automatic notice about a worker (`notifyLauncher` in
`internal/daemon/a2a.go`, and the ledger's `ended` notice in `internal/store/ledger.go`) was typed there. A launcher
tagged `atrium:orchestrator` or `atrium:hold-notices` now has them held: an event on its card, a line on the worker's
work item, and a `notice` event for the board's bell. `atrium_task` with `notices: true` reads them.

Not held: a worker's report (`atrium_report`, `atrium finish`). It is the worker speaking, not atrium.

Not covered: a launcher on another room. The notice is relayed through the hub, and this room cannot see that card's
tags.

## The second silent stop 15 seconds after the first

`review-director-of-software-review-on-cl-2` got silent-stop notices at 09:54:30 and 09:54:45. That was two prompts,
not a dedupe gap: its turn ended at 13:54:30Z, `atrium-87300` prompted it at 13:54:40Z, and that four-second turn
ended at 13:54:45Z without a report. The key is the prompt that opened the turn (`PromptKey`), so each got one
notice, as designed. The noise is that the directors report to `notes\director-reports.md` and never through atrium,
so every turn a director ends after a prompt from the orchestrator reads as a silent stop. Holding the orchestrator's
notices stops them reaching the terminal.
