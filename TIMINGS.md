# Paste-a-link speed: timings, before and after

No live hub board was available to this worker, so there are no keypress-to-session numbers. What follows is the code
path read end to end, the pieces I could run (real git against github.com/openziti/ziti-console, this machine), and the
one number clint measured (`POST /v1/recognise` = 0.92s, which is one `gh pr view` on the hub). Forge calls are counted,
not timed, and costed at that 0.92s.

## Measured pieces (git, real repo, PR 960, once warm)

| step | seconds |
|---|---|
| hub: init store + seed main (first time a repo is held) | 1.93 |
| hub: `FetchPR` of one PR head (fsck on) | 0.42 |
| room: **full clone** from the hub's store (first time on a room) | 0.49 (ziti-console is 21 MiB; a big repo is minutes) |
| room: fetch one PR head from the hub | 0.17 |
| room: `git worktree add` | 0.35 |
| `git ls-remote` (network floor) | 0.46 |

So git is about 1s when the room has the clone, and a clone away from that when it does not. The browser side is
already one `/v1/open` call (the f-pr-launch-fast work), so it adds a round trip, not seconds.

## Where the time was (code path of Enter on a PR link)

| step | before | after |
|---|---|---|
| browser recognise (paste box) | 1 forge read | unchanged, Enter reuses it |
| hub placement | least busy room, `/v1/tasks` of every room (<=1.5s), **ignores which room has the repo** | the same asks plus `GET /v1/scm/has`, beside them; a room holding the repo wins |
| room recognise inside `/v1/open` | 1 forge read | 1 forge read (the hub keeps it for the View below) |
| `prWorktree` View | 1 forge read | 0 (taken from the recognise's peek, 10s window, once) |
| `openRow` -> `postPR` | **recognised the link a third time**: hub rows call + 1 forge read | 0, takes what open recognised |
| clone on the placed room | full clone through the hub whenever the least busy room lacked the repo | only if no room holds it |
| fetch head, worktree add, launch | as measured above | unchanged |

Forge reads (each ~0.92s) per Enter on a PR, with the paste box's read counted: **before 4, after 2**. Estimated saving
about 1.8s on a repo the room already has. The big one is placement: a PR or a Zendesk/Discourse ticket (which opens a
worktree in the row's default repo) landing on a room that does not hold the repo paid a clone of it. That is the
likely "forever", and it is the step gwt skips by using the clone it has.

## Zendesk and Discourse

No forge read and no fetch (open runs none for a support link), so the card starts after: hub placement, room recognise
(hub rows call), worktree in the default repo (`worktree add`, ~0.35s) or a scratch folder, launch. The one slow thing
was the same one: placement could send it to a room with no clone of the default repo, which cloned it first. Placement
now prefers a room that has it. With `repo: none` there is no git step at all and nothing changed.

## What I could not measure

Keypress-to-session wall time, the `[atrium open] <url>: <step> took Ns` lines on a real room, and the cost of
`launch` itself (starting the runner). After deploying, paste a PR and read those log lines on the room: they time
recognise, read, checkout, fetch, worktree, review and card. Anything over a second there is the next suspect.
Not fixed here: the hub store's `main` is seeded once and never refreshed, so a PR head fetch into a long-held store
transfers everything main has gained since the seed.

