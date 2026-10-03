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
