# Review of 23fa4602 (u-ready-spam, a focused window silences a card's ready alert in every window)

Reviewed by @review, 2026-09-30, from reading `git diff 23fa4602^1 23fa4602`. Board files only, a hub-only deploy.
The headless tests are @ui's and were not run here. This is the fix for the medium in `u-new-review-5e68b83f.md`.

## What holds

- **The medium is closed.** A focused window now says which card it reads in its `win-focus` beat (`watch`,
  `internal/api/web/js/notify.js:124-132`), and `readySilenced` (`notify.js:115-120`) silences that card's ready
  alert in every other window as well as its own. The board's ready path (`notify.js:1065`) and a pop-out's
  (`solo.js:765`) both ask it.
- **A forwarded toast is caught at the receiver too.** `win-toast` now carries `ready`, and the focused window drops
  a ready toast for the card it is reading and logs it (`notify.js:198-204`). So a sender with a stale view of who is
  focused is still quiet.
- **Ids compare bare on both sides** (`bareId` on the watch and on the alert), so `room~id` on the merged view and a
  bare id in a pop-out match.
- **The announce follows the card.** `openTerm`, `clearTermPane` and `switchView` each re-announce, and the 4 s beat
  (`focusBeat`, `notify.js:91`) covers a window that misses one. `termWatching` and `watchedCard` are wrapped
  against the `termTask` `let` not existing yet at load.
- **Still logged.** Every silenced alert writes its toast-log line.

## Findings

### Info

1. **The first announce from `openTerm` can name the wrong card for up to one beat.** It runs before `term` is made
   (`terminal.js:564` against `terminal.js:650`), so on a first open `watchedCard` is empty, and on a switch it names
   the new card while the old terminal is still up. The next 4 s beat corrects it. A ready alert needs 5 s of quiet
   before it rings, so no alert falls in that gap. No change.

## Verdict

**HUB DEPLOY OK 23fa4602.** Closes the 5e68b83f medium. No room half.
