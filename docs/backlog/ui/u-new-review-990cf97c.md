# Review of 5786991e at 990cf97c (@ui u-m-older: /m loads older replies)

Reviewed by @review, 2026-10-01, from `git diff e94f997b 990cf97c -- internal/api/web` (only 5786991e is new; the
follow fix 07b91e0c was passed earlier). Read only. Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **The cursor is the room's.** `next_before` is passed back unchanged, only `more=false` ends paging, and an empty
  page with `more=true` keeps the row. That matches f640fafc.
- **One request per ask.** A click, or a reader's own scroll to within 40 px of the top held for 350 ms (the page's
  own scrolls are filtered out by the follow fix's `mine` check). Never while one is in flight, never an automatic
  retry after a failure, and the failure row retries on a tap.
- **The reader is not moved.** The prepend goes through `paintReplies`, which keeps the top bubble's y by `data-k`.
- **Nothing new reaches the page unescaped.** Older entries go through the same `replyHTML` and `promptHTML`. The
  row is static markup.

## Findings

### Medium

1. **Loaded older pages and a refreshed newest window leave a silent gap.** `olderOf` holds the older pages,
   fetched back from the cursor of the newest window as it was then. The newest window is re-read with `n=50` on
   every refresh, and `cache.set(id, got)` replaces it. Once a page has been loaded (`o.pages > 0`), each new reply
   pushes the oldest entry of the newest window out of it. That entry is not in the older pages either, because
   they stop at the old cursor. After k new replies, k entries between the two are missing, and nothing on screen
   says so.

   It is worse across a close. `olderOf` is never cleared (there is no reset in `finishClose` or anywhere else). A
   card opened an hour later shows its newest 50, then the old pages from before, with everything in between
   missing.

   Reproduction from reading: open a card with more than 50 replies, tap "load older", then let the card answer
   twice. The two replies that were the oldest of the first window are gone.

   **Fix.** While a card is open and has older pages, fold every refreshed newest window into the accumulated set
   before replacing it, for example `o.replies = o.replies.concat(prev.replies)` with the dedup `withOlder` already
   does, so nothing that was ever on screen leaves it. Drop the card's `olderOf` entry on close, so a later open
   starts from the newest window and its own cursor. An `mOlder` case that refreshes the newest window after a load,
   with new entries, would have caught this.

### Nit

2. Dedup by `at` and text collapses two distinct entries with the same time and text, for example a prompt sent
   twice in one second. That is rare, and the follow fix's anchor uses the same key.

Quality: after the Sonnet switch. The paging contract is followed exactly, including the empty-page rule I asked
@runtime to pass on, and the trigger discipline is careful. The miss is state consistency: two sources of entries
(the refreshed window and the accumulated pages) with nothing keeping the seam between them closed.

HOLD 5786991e

## Re-read of 22157cc3 (@ui, medium 1)

`git diff 990cf97c 22157cc3 -- internal/api/web`, read only.

- `loadReplies` folds the window it is about to replace into `o.replies` and `o.prompts`, deduped by time and text
  and sorted, once the card has older pages. Nothing that was on screen leaves the thread when a refresh moves the
  newest window on. Before the first older page, nothing is folded and the thread is the newest 50, as before.
- `finishClose` deletes the card's `olderOf` entry, so a reopen starts from its own first window and cursor. A
  `loadOlder` still in flight at the close returns on `id !== openId`, or writes only into the dropped object.
- Two `mOlder` cases cover both paths, and each fails without the fix.

Medium 1 is closed. Nit 2 stands.

Quality: after the Sonnet switch. The fix is exactly the seam named, with a test for each path. No drop seen.

HUB DEPLOY OK 5786991e~1..22157cc3

## Re-read of 818917aa (@ui: mOlder made machine independent, and a close race)

`git diff 22157cc3 818917aa -- internal/api/web`, read only.

**The race is real, and the token fixes it.** `transitionend` can finish a close (`openId` cleared) before the
400 ms timer fires. If the same card is reopened in that gap, `open` sets `openId` again before the two-frame `on`
class lands. The stale timer then saw `openId === id` and no `on`, and closed the new card. `open` now bumps
`closeTok`, and a close's `done` acts only on its own token. The same check also makes harmless a `transitionend`
listener left registered when the timer won, which would otherwise fire on the next open's slide.

The test change (ten tall replies, overflow asserted first, two heights, two pinch sizes, and a short-card case) is
@ui's own.

Quality: after the Sonnet switch. A product race found by making the test honest, and fixed with a token rather
than another timeout. No drop seen.

HUB DEPLOY OK 818917aa
