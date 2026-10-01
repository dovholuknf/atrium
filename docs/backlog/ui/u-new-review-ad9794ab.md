# Review of ad9794ab (@ui u-m-home) and 869cfa7f (@ui u-m-card on top of 183d043c)

Reviewed by @review, 2026-09-30, from `git diff 7a37dcc2...ad9794ab` and `git diff 183d043c 869cfa7f`, scripts only,
read only. Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **cardrules.js** has no DOM work and no load-time call beyond declarations. None of its 11 top-level names is
  declared a second time anywhere in `internal/api/web` (grepped), so loading it first cannot raise "already
  declared". Only `index.html` and `m/index.html` load it. `new Function` compiles only the two constant defaults,
  as `board.js` already did.
- **Escaping.** The /m card picker escapes the id and the name, and the way out of a card's window is built with
  `textContent`. Group headings and the stored view options are escaped, or limited to their known values on load.
- **compose.js.** A send clears the box at once and puts the text back on failure, ahead of anything typed since.
  Attachment paths are already in the text, so clearing the chips loses nothing. Enter sends only with a fine
  pointer, and not while composing or on keyCode 229. In-flight rows are in memory only.
- **toasts.js** guards `termOnly` and `isViewing` before calling them.

## Findings

### Low

1. **"Needs you" is no longer oldest wait first.** `needsList` still sorts its rows "OLDEST WAIT FIRST" by
   `since`, but `render` now passes every list through `sortRows`, which re-sorts by last activity, newest first
   by default. So a question asked two hours ago, on a card idle since, sinks below one asked a minute ago. That is
   the ordering CLAUDE.md's "a column is a bucket of human attention" rule was written against. Was newest-first
   meant for the "all" list only? If so, skip `sortRows` for the needs list, or make the needs list's default
   order "oldest wait".

### Nit

1. A failed send puts the text back, but the attachment chips do not come back. The paths are still in the text.

HUB DEPLOY OK ad9794ab 869cfa7f
