# u-pr-move-action

## Built
- `prMove` in js/pulls.js: the room picker (online rooms other than the PR's owner, found from `GET /_hub/pr-claims`), then in order
  the hub move (`POST /_hub/pr-claims/move {key,to}`), `makePastedWorktree`, a launch with tags `pr` and `pr:<org>/<repo>#<n>`
  (through `startPastedReview`), and `setPastedWalker`. A hub refusal stops there and shows the hub's sentence. The old card is left
  alone. Toast: "moved to <room>. The old card is still on <old room>."
- A `move` button on the pulls row and a `move to another room` entry on the card menu of a PR's walker. Both hidden unless
  more than one room is attached. The pulls rows repaint when the room set changes (rooms.js).
- fixtures.js: the three paste helpers take an optional `at` ({room, url, say}) so they run without the launch dialog. The paste path is unchanged.
- Headless section `prMove` in scripts/test-board-headless.js: one-room board has no move; a refusal makes exactly one call; a success makes
  move, worktree, review, launch, walker in order with the right cwd and tags, and the toast.
- Screens: docs/screens/u-pr-move-action/ (before-one-room, after-picker, after-toast).

## Tests
HEADLESS_ONLY=prMove, pulls, pullsDrawer, pullsAbsent: all ok.

## Not done
- The card-menu entry is not exercised by a headless test (only the pulls row is).
- The harness for the new card is the old walker's when known, else `claude`; the pulls row passes no walker, so it is always `claude`.
- If the hub move succeeds and the worktree or launch then fails, the claim has moved and the note says so; nothing rolls it back.
- Full headless suite not run, only the pulls sections.
