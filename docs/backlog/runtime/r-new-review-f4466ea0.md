# r-new-review-f4466ea0. Fixes for the c184ae8c high, the b144c66a medium, and the growler lows

Status: open, two lows. Filed by @review 2026-09-30. Read-only review of f4466ea0 (merge of `claude/r-review-fixes`).
Owned by @runtime.

## Closed

- **c184ae8c high, the room halt.** `appendEventOn` refuses an empty card id before the insert, and turns a
  foreign key failure into `sql.ErrNoRows`, which `guard` returns without halting (`internal/store/tasks.go`).
  `eventnocard_test.go` pins both. The fixture's refused resume is now carried as `heldResume` and written on
  `task.ID` after `Launch` made the card, with `via` saying `fixture` or `reopen`. That closes low 2 of the same
  review as well.
- **b144c66a medium.** `windDown` sets `runner.leaving` before it types, and the hook's `/exit` rule skips a runner so
  marked. So the idle park, the worktree sweep and the stop half of a restart no longer count as asked. `StopRunner`
  still records its own ask explicitly, before it winds down.
- **b144c66a lows.** A join and the reaper's revival clear `exit_asked_at`. `Kill` marks only once the process is
  stopped or gone.
- **Growler lows (a23a9034, 06b876bb).** `publish` builds and compares under one lock. `openGrowls` waits up to a
  second on a 128-slot channel. A fill gives up after three misses per growler and forgets the count when the growler
  ends. `growl.since` stays in memory when it cannot be written. The notifier comment now says a first-turn report
  notifies. The `0005_growl` change is a comment only, with the dismissal-ends-resolved behaviour agreed with @ui, so
  no recorded migration was edited.

## 1. Low. The halt still treats every other "no such card" as broken storage

The fix is scoped to the event insert, which is where this finding was proved. `guard` still halts on any error
that is not busy or locked, so a foreign key failure on any other table (a message, a say, a permission, a fixture
row pointing at a card) halts the room in the same way. And the new branch recognises the failure by matching the
message text. Classify it once, in `guard`: an `SQLITE_CONSTRAINT` family code from `sqlite.Error.Code()` is the
caller's error, returned as such and never a halt. Storage failure stays what the halt is for.

## 2. Low. Low 3 of c184ae8c is still open

`bootResumes` is still filled from `t.ResumeID` (`internal/daemon/reopen.go`), while `reopenResume` resumes what
`resumeIDFor` answers. Where the two differ, the one-conversation-per-pass rule does not cover the card.

## Tests

`go test ./internal/store/` passes (78s). `./internal/link/ -run 'Growl|Notify'` passes. `./internal/daemon/ -run
'Reopen|Restart|Exit|Fixture|Resume|Ended|Asked|Held|Park|Prompt|Idle|Join|Reap|Kill' -timeout 20m` passes (57s),
including the fixture-first-start test that checks the store stays healthy.
