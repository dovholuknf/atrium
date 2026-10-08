# f-pr-launch-fast

Pasting a PR link and pressing Enter was slow and showed a dialog first. This is the timeline and what changed.

## What was measured here

| Step | Seconds |
|---|---|
| POST /v1/recognise | 0.92 |
| `gh pr view` | 0.59 |
| `git worktree add` | 0.35 |
| 4 cheap git calls | 0.34 |
| full local clone of ziti-console | 12.7 |

I could NOT time the network fetch of `pull/960/head`. A hook here forbids any git fetch to a remote. The live logs
(`hub.err`, `room.err`) hold no open timings. The new `[atrium open] <url>: <step> took Ns` lines will give them on the
next live open.

## Where the time went, and what changed

| Step | Why it was slow | Before | After |
|---|---|---|---|
| Placement | the hub polled every room's /v1/tasks with a 10s timeout, so one slow room held the paste | up to 10s | bounded at 1.5s (`placeLoadWait`) |
| Resource measuring | the answer waited on measureResources | up to 5s | runs in the background after the answer |
| Finding the checkout | ran after the forge read | serial | parallel with the forge View |
| Recognise | the board recognised twice, in the box then in the dialog | 2 x 0.9s | once, the box's result is reused |
| Launch dialog | shown on Enter before the open | a dialog | never shown on Enter, a strip names the step |
| Fetch of the head | goes through the hub store | not measured | unchanged, now logged |
| Clone of a repo no room holds | full clone | 12s or more | unchanged |

## Not done

- Placement does not prefer a room that already holds the repo, so the least busy room may have to clone. Follow-up.
- No filtered or shallow clone. The hub store would need `uploadpack.allowFilter`, and a worktree review needs history.
- The room still never fetches from the forge itself, so the fetch stays through the hub store.

## Tests

- Go: `internal/api` scoped to PRWorktree, Open, Quick and Progress, 3 runs ok. `internal/daemon` ok. `internal/link`
  scoped to Placement, PRWorktree, OpenOn and Paste, 3 runs ok. New: `prworktree_reuse_test.go` and
  `TestPlacementDoesNotWaitForASlowRoom`.
- Board: `quickPasteSection` was extended (progress strip, failed open reopens the dialog, plain paste). Playwright is
  not installed on this machine, so it was only syntax-checked with `node --check`. It has NOT been run.
