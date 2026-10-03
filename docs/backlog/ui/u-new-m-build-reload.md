# u-new-m-build-reload: the phone page picks up a new build

**Status: done.**

## Cause

After a hub deploy the phone page (`/m/`) kept the build it had loaded until it was reloaded by hand. The desktop board
reloads itself when `/v1/health`'s `build` changes (`checkBuild` in `js/terminal-links.js`, `docs/runtime/reload-design.md`).
Nothing under `m/` read `build`, or `/v1/health` at all, so the page had no way to learn that the hub was new.

Ruled out, with the evidence:

- **HTTP cache.** `internal/webasset` serves everything except `vendor/` as `Cache-Control: no-cache` with an ETag, so a
  load revalidates. The stale page was never re-fetched because nothing navigated it.
- **Service worker.** `sw.js` is registered by the board (`js/notify.js`), not by `/m/`, and its fetch handler only
  touches failed navigations (the down page). It never serves a script.
- **Manifest.** `manifest.webmanifest` only sets `scope` and `start_url`. An installed app or a background tab is a
  long-lived page, and it is never navigated, which is the same reason a popped-out terminal needs the board's check.

No Go change is needed.

## What changed

`m/js/build.js` asks `/v1/health` when the event stream comes back (`live`, which is what a hub restart looks like) and
when the page returns to the front, and remembers the first `build` it saw. A different one reloads the page, with these
rules:

- It waits while any box holds a draft, a box has focus, text is selected, or a send or upload is in flight
  (`mCompose.busy()`). A quiet "update ready" button in the header shows while it waits; pressing it reloads at once, and
  the reload comes by itself when the page is clear. The phone page has no voice input to wait on.
- A tab reloads at most once per 30 seconds (`sessionStorage`), so a hub that keeps changing its id cannot loop the page.
  Seeing the id it already runs drops whatever it was waiting for.
- The offline page is untouched: `sw.js` and `DOWN_CACHE` are not edited.

## Done

- `HEADLESS_ONLY=mReload`: the same id does not reload; a new id reloads once; a draft holds it with the cue up and it
  reloads when the draft clears; a flapping id does not reload twice; the cue goes when the id returns; a selection holds it and releases it.
- Mutants (no busy check, no 30s guard, no selection check, no "same id clears the wait") each fail the section; the
  last is caught by an assert that the cue goes away when the id returns to the one the page runs.
- `mServe`, `mHome`, `phoneHeader`, `bootClean` pass alone.

## Follow-up: refresh after a hub restart

Checked on `/m` after back-to-back hub restarts:

1. **Stream reconnect: works.** `store.js` closes a failed `EventSource` and reopens it on a 1s to 30s backoff, so a hub
   that is down for a few seconds leaves no stuck state. A stream that dies without an error is not detected (the hub's
   25s pings are comments the browser does not surface); a page coming to the front reconnects only if it has no stream.
2. **Re-fetch on reconnect: two gaps, fixed.** The stream opening read the cards and permissions once, and a failed read
   (the room is not up yet right after a restart) was only tried again by the 60s resync. Failed reads now retry on a
   1s to 15s backoff while the page is in front. The open card's thread was read only on open or a turn change, so a reply
   that landed while the stream was down stayed missing; `card.js` now reads it when the stream comes back.
3. **Build check with two restarts close together: no bad interaction.** The 30s guard holds a second reload until it
   expires, the wait follows the newest id, and a draft holds both. A reload that starts just as a second restart takes
   the hub down would land on the browser's error page; the page cannot prevent that, since it only reloads after the hub
   answered.

The open card also reads its thread when the row's `last_activity_at` or `prompted_at` moves, for rooms whose rows carry
no `output_at`. At most one read per 5s per card with the last change always read, and nothing when the row did not change.

Tests: `mReconnect` (refused connects with backoff, a failed first read, state missed while down, the open card's thread)
and `mActivityRead` (unchanged row reads nothing, a moved activity reads once, ten changes make one more, `prompted_at`
alone reads); `mReload` also covers two builds arriving under a draft. Mutants that fail: no read retry, no card re-read
on reconnect, no throttle, no change gate, no `prompted_at`, no trailing read. Two survive: the permissions retry (the mock
never fails that read) and "the newest id wins under a draft" (not observable from a reload count).

## Review follow-up

- **Attached files and review-comment chips** are held in memory only, so a reload loses them even with an empty box.
  `mCompose.holding()` (an attachment or a comment chip on the mounted box) now counts as busy in `build.js`. `mReload`
  adds a chip with an empty, unfocused box: no reload, the cue shows, and it reloads once the chip is removed. The
  attachment half of `holding()` has no headless line (the mock has no upload route); the chip half is mutant-checked.
- **A turn end read the thread twice**, the second 5s later: the activity that came with it was still pending. The
  immediate read now takes the activity key with it and cancels the pending one.
- **The activity read is only for rows with no `output_at`.** Where a room reports it, its move is the signal.
- **Retry backoff is per read** (cards and permissions each count their own failures), and a 401 or 403 is not retried.
  `mReadRetry` covers a 503 backing off on permissions and a 401 and a 403 not retried. The cards read's own backoff is
  not mutant-checked (a failing cards read needs a page that loaded once and then failed).
