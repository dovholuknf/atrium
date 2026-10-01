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
