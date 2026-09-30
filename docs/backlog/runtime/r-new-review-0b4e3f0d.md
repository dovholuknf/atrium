# r-new-review-0b4e3f0d. Fixture skips done, resume refuses a held conversation: review

Status: open. Filed by @review 2026-09-30. Read-only review of 0b4e3f0d (merge of `claude/r-reopen-exited`). Owned
by @runtime.

The boot rule in this merge (`done` means ended) is what broke the 12:58 restart, and ff747683 replaced it with
`ExitAsked`. That part is reviewed in `r-new-review-ff747683.md`. What is still live from this merge is
`resumeHeld` and `Store.ResumeHolder`.

## 1. Medium. Only the newest holder of a conversation is checked

`ResumeHolder` (`internal/store/tasks.go:868`) returns one card, the newest by `created_at`, and `resumeHeld`
(`internal/daemon/fixtures.go:291`) asks only whether THAT card is live. When two other cards carry the id, which is
the r-021 part 2 shape and this incident's, a newer dead one hides an older live one. The conversation is then
resumed while a live card has it open, which is the thing the function exists to refuse.

Fix: select every holder other than `except` and refuse when any is live.

## 2. Low. At boot, a holder that has not been reopened yet reads as not live

`reopenResume` calls `resumeHeld` card by card (`internal/daemon/reopen.go:250`). A holder later in the same
reopen pass has no supervisor entry yet, and its recorded pid is from before the restart, so `runnerIsLive` says no.
Two cards on the reopen list with one resume id both resume it. `ClaimResumeID` should stop the pair existing, so this
is a backstop gap rather than a path seen live. Resolve the conflict over the whole list before launching anything.

## 3. Low. The refusal is a log line only

The comment on `resumeHeld` says the refusal is "said out loud, because the runner would start fresh without a word".
It is said in the room log and nowhere a person looks. The card starts a fresh conversation, and the fixture row and
the card timeline say nothing. Put it on the fixture row with `NoteFixtureRun`, as `fixtureEnded` does, and as an
event on the card.

## 4. Not covered, restated

A live holder on another room on the same machine is invisible here, as the backlog item says. After ff747683 the
boot rule that closed this incident's path covers only exits that went through `StopRunner`. See finding 2 of
`r-new-review-ff747683.md`, which reopens the path for a terminated fixture.

## Tests

Same run as `r-new-review-ff747683.md`.
