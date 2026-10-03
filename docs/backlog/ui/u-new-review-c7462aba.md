# Review: u-m-reload c7462aba

Range `110c224a..c7462aba`, 2 commits.

- `6f3912fb` adds `m/js/build.js`: the phone page reloads itself on a new build.
- `c7462aba` re-reads after a failed or missed read, and when a card's activity moves (`m/js/store.js`,
  `m/js/card.js`).

There are no Go changes. The rest is `m/index.html`, `m/m.css`, the item doc `docs/backlog/ui/u-new-m-build-reload.md`
and the headless sections.

Verdict: **hold** on M1. A reload can drop attached files and review-comment chips. The fix is one line. The rest is
sound, and the Lows can follow.

## How it was checked

- I read the diff in full, with `m/js/compose.js` (drafts, `busy`, attachments) and the store's resync and
  `visibilitychange` paths around it. Per the standing note, I read the board units and did not run them.
- `node --check` passes on build.js, card.js, store.js and test-board-headless.js at the tip.

## The reload

- **The signal.** It reads `build` from `/v1/health`, a hash of `web/`. It asks when the stream comes back live and
  when the page comes to the front. There is no clock of its own.
  - The id is only compared. It is never shown, so there is nothing to escape.
  - The cue's text is fixed.
- **No loop.** `go()` writes `{build, at}` to sessionStorage, and `guarded()` waits out 30 s after any reload.
  - Two deploys back to back give one reload, then a second one 30 s or more later.
  - A hub that answers with a new id every time costs one reload per 30 s at most. That is the worst case, and it
    needs the hub to be broken.
  - An id equal to the running one drops whatever reload was waiting.
- **The hub cannot be reached.** A failed fetch, a non-200 or bad JSON does nothing.
- **A rollback.** An older build counts as a different id, so the page reloads onto what the hub serves now. That is
  right.
- **The text draft is safe twice over.**
  - `busy()` defers while any textarea has text, while an input or textarea has focus, while text is selected, and
    while `mCompose.busy()` (a send or upload in flight).
  - Every card's draft text is also kept in localStorage and cleared only by an accepted send (compose.js:24). So even
    a forced reload through the "update ready" button keeps the typed text.
- **The file picker.** An open native picker leaves focus on the `type=file` input, which the input rule counts as
  busy. After a pick, the upload is busy until it finishes.

## The retries

- **The cards and permissions reads** are retried from 1 s, doubling, up to 15 s. A retry is only scheduled while the
  page is visible.
  - A hidden page schedules nothing. The store's `visibilitychange` handler reads both again when the page comes back.
  - `tasksBusy` and `permsBusy` keep a read from running twice at once.
  - The 60 s resync and an event both call `tasksSoon(0)`, which clears a backoff timer that is waiting. So nothing
    runs in parallel.
- **The thread is read again on reconnect.** It reads on the live edge only (`!wasLive && on`), only with a card open,
  and the replies GET is the only call.
- **The activity read.**
  - The card's activity key, `last_activity_at|prompted_at`, is compared on every `onCards`.
  - Only one timer is pending at a time, and it reads at `lastRead + 5 s`. So it reads at most once per 5 s, and the
    last change is always read.
  - The timer is cleared on open and on close, and checks `openId` before it reads. So there is no leak and no read
    for a card that has closed.
- **Nothing that changes state.** The only calls added are GETs: `/v1/health`, `/v1/tasks`, `/v1/permissions` and
  `/v1/tasks/{id}/replies`.

## The two surviving mutants

Both are acceptable, and the item doc names them:
- The permissions retry survives because the mock never fails that read. It is the same code shape as the cards
  retry, which is caught.
- "Newest id wins under a draft" survives because a reload count cannot see it. The code reads right: `target` is
  replaced, and `settle` reloads to whatever the hub serves.

## Mediums

### M1: a reload drops attached files and review-comment chips when the box has no text

`busy()` checks only these:
- a textarea with text;
- focus;
- a selection;
- `mCompose.busy()`, which is `sending > 0 || uploading > 0`.

It misses two things that compose state holds and does not save:
- `atts`, attached files that are already uploaded and shown as chips;
- `cmts`, review comments from the file viewer's tapped lines.

Neither is in localStorage. The draft only keeps text. Take a person who attaches a screenshot, or taps three lines
in the viewer with comments, and then pockets the phone before typing. The next deploy reloads the page when it
comes to the front, and the chips are gone with no word.

Fix:
- In compose.js, make `busy` (or a new `pending`) also true when `cur.atts.length || cur.cmts.length`.
- Add a line to `mReload` with a chip and an empty box: the page must not reload.

## Lows

- **L1: a turn change reads the thread twice.**
  - When a turn ends, `onCards` calls `loadReplies()` through `turnKey`, which sets `lastRead`.
  - The same `onCards` usually moves `last_activity_at` too. That starts the act timer at `lastRead + 5 s`, which
    reads the same thread again 5 s later.
  - Set `actKey` whenever the turn path has just read, or skip the timer when `Date.now() - lastRead < 50`.
- **L2: a working card is read every 5 s.** `last_activity_at` moves on most hooks. So a card that is open and busy
  re-reads `replies?n=REPLIES_N` every 5 s for the whole turn, on the phone's data, even when `output_at` is reported.
  Gate the act read on `output_at` being absent on that row (the case the comment names). Or keep 5 s only until
  `output_at` is first seen.
- **L3: the shared `readFails`.** Cards and permissions share one counter, and only a good cards read resets it. A
  permissions endpoint that keeps failing while cards succeed is reset back to a 1 s retry every time cards succeed,
  so it never backs off. Give each read its own counter.
- **L4: a retry on 401 or 403.** A page whose session has gone retries every 15 s forever while it is visible. That is
  harmless but wasted. Stop on an auth status: the stream's own handling already shows the sign-in state.

## Known gap (as stated)

A stream that dies silently is not noticed, because SSE comment pings never reach the page. The visibility and online
handlers cover the common phone case. A named `ping` event from the hub would close it later.

Atrium-Verdict: hold 110c224a..c7462aba
Quality: careful work. The reload never loops, never loses typed text, and backs off on a failure. One line keeps the
chips safe too.
