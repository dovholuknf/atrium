# Interviews stay top level and are never idle parked

A card tagged `atrium:interview` is a top row in its tag group on every list. It never nests under the card that
launched it: `nestsUnderLauncher` in `js/cardrules.js` is the one rule, and the terminals list (`termNest`) and the
stack and board (`subagentRollup`) both ask it. A launcher whose children are filtered out or not drawn still shows
its expand chevron, since `termKidTotal` counts the children in the whole pool and not only the drawn ones.

Interviews were parked with no process while waiting on the human. The cause was not the lean tag: `idleParkSubject`
(`internal/daemon/idlepark.go`) parks every `origin:agent` card, and an interview waiting on a plain-text question
is not held by the asked/answered check. An interview is now never subject to the idle clock. Lean is unchanged and
nothing strips it from existing cards. Go code changed.

Code: `js/cardrules.js`, `js/terminal-list.js`, `js/filter-drawer.js`, `internal/daemon/idlepark.go`. Tests: the
`interviewTop` headless unit and `TestInterviewIsNeverIdleParked`. The `term-nesting` unit is known failing and was
not run.

## Test plan

## @LETTER@. Interviews stay top level

### @LETTER@1. Nesting

1. Have an orchestrator launch a worker and an interview (`kind: interview`).
2. Open the terminals tab, the stack and the board.

**Expected:** the interview is its own row under its tag group. The worker still folds under the launcher. A
launcher whose children are all hidden by a chip or the search still shows its chevron.

### @LETTER@2. No parking

1. Leave an interview waiting on a question for longer than the idle park time.

**Expected:** its process is still there.
