# u-findings-probe-404

Attaching a terminal probes `GET /v1/tasks/{id}/files/list?path=findings` ("does this card hold findings to walk",
`walkProbe` in `js/walk.js`). A card with no findings folder answers 404, which the browser logs.

## No card-side signal, so the miss is cached

Fix B (skip the probe for cards that cannot hold findings) needs a marker on the card record. There is none. A review
the pulls view knows is found through `walkPrOf` (a pulls row names the card as its walker), and that path already
makes no files request. A hand-started review card, with a bare `findings/` folder and no pulls row, is
indistinguishable from an ordinary card in the record the board holds: no tag, kind or flag says it. Skipping on any
guess would hide the walk button for those cards.

So the fallback applies: the probe is made once per card per session. `walkProbeFailed` caches the miss by card id, and
a failed probe is not repeated on reattach. A card that holds findings is probed as before. A path outside the card
still answers 403, untouched.

The first attach of each findings-less card per session still logs one 404. Removing that needs fix A, a Go follow-up:
`files/list` answering 200 with an empty list for a missing folder inside the card (and 403 outside it, unchanged).

## Test plan

1. Attach a card that has no findings folder, switch to another card and back, several times. The network log shows one
   `files/list?path=findings` for it, not one per attach.
2. Attach a card whose worktree has a `findings/` folder of finding files. The walk button appears and the drawer opens.
3. Headless unit `findingsProbe` (`scripts/board-tests/rooms.js`) asserts both against the mock daemon's request count.
