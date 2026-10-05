# The PR review workflow: paste, review, walk every finding (rnd, 2026-10-04)

clint, 2026-10-04: "It would be nice if hub has some kind of sexy workflow for that. Like when I do a PR, first thing I
need is an 'atrium review', then capture durable items on that atrium object for human to review."

This design adds almost nothing new. The pieces exist and are not joined. It says how they join, what is missing,
and builds the missing parts.

Read for this design:
- `docs/rnd/scm-forge-design.md` (the forge, placement, the claim, and clint's answers of 2026-10-04)
- `docs/rnd/pulls-view-design.md` and `docs/rnd/pulls-api.md` (the PR row, the runner, the routes)
- `docs/rnd/review-tab-design.md` (the walk, the walk shape, `walk.txt`, posting option A)
- `docs/review/review-memory-design.md` (clint's standing rules for a walk)
- `docs/rnd/hub-forge-answers.md` (the hub is a router, not a store)
- the brief of `r-hub-forge` on m1mini (rooms never call a forge, the hub does)
- the code: `internal/daemon/prrunner.go`, `internal/api/prs.go`, `internal/api/prsdrawer.go`, `internal/link/prclaim.go`,
  `internal/link/prworktreeroute.go`, `internal/api/web/js/fixtures.js`, `walk.js`, `pulls.js`

## 0. The workflow in one paragraph

clint pastes a PR link on the board. The launch dialog recognises it, the hub places it on the least busy room, the
room makes the PR's worktree and starts a card there, **and the same press starts the review**: a `POST /v1/prs` on
that room, so a PR row exists and the PR runner runs the recipe's panel. The card is recorded as the row's walker, so
when the review is ready its findings show in the walk drawer **on that card**, and the PR's row in the pulls view
says how many are left. clint goes through every finding on the board or the phone and marks it `accepted`,
`dismissed` or `posted`. Atrium never posts. `posted` is clint saying they posted it by hand, with the comment's link
when they have one. The findings and their states are files in the run folder on the room that owns the PR. They survive
a room restart because they are files, a run cut by the restart resumes on start, and they travel with the PR when
clint moves it to another room.

## 1. What exists, and what was missing

| step | exists | missing, and built here |
| --- | --- | --- |
| paste a PR link | recogniser, launch dialog, `pr-worktree`, placement, claim (`u-pr-paste`) | nothing |
| start the review | `POST /v1/prs`, the PR runner, the recipe | **the paste never calls it.** Item `r-pr-paste-review` |
| tie the review to the card | `walker_task` on the row, the drawer reads `/v1/prs/{id}` for a walker | **the pasted card is not recorded as the walker.** Same item |
| a second paste of the same PR | the claim folds the worktree | **a second card starts.** Same item |
| durable findings | files under `<run>/findings/`, state in `walk.txt`, the drawer and the pulls view read them | **no `accepted` state.** Item `u-pr-finding-states` |
| the phone | the drawer has a phone layout (`phone.css`) | checked and fixed in `u-pr-finding-states` |
| a room restart | files survive | **a run cut by the restart stays `running` forever.** Item `r-pr-paste-review` |
| a room move | `POST /v1/pr-claims/move` moves the claim | **the run folder stays on the old room.** Item `f-pr-review-move` |

## 2. The review run

**Started by the paste.** In the launch dialog, when the matched recogniser is a pull request row and the worktree was
made, the launch also sends `POST /v1/prs {"url": <the pasted URL>}` to the placed room, before the card is
launched. The paste is the ask (scm-forge answer 5), so there is no review button to press. A paste that only wants a
card, with no review, is a typed directory or the throwaway tick, as today.

**One door.** `POST /v1/prs` stays the only way a row is made (pulls-api). It already dedupes: the same PR at the same
head answers the existing row with `created: false`, a failed or aborted row restarts.

**The model and the panel are the recipe's.** `RecipeFor(org/repo)` picks the recipe, `default` when no recipe
matches. The seeded default runs on the `claude` harness at the runner's default model, with the panel by file type
(`c-systems-reviewer` for C, `go-security-reviewer` for Go, `functional-tester` and `nonfunctional-tester` for all),
verify at `med`, no critics, a 3 USD budget and a 12 turn cap. Nothing in this design changes the recipe. clint
changes it in settings, per repo if clint wants.

**The second opinion is not run.** The runner's `second` and `settle` steps are not built (P3 of the pulls design),
and this design does not build them. See "Open for clint", 1.

**Where the PR comes from.** After `r-hub-forge` lands, the room reads the PR from the hub and fetches the head through
the `hub` remote. This design does not touch that path, and works the same before and after it.

## 3. A durable finding

**What it is.** One file in the run folder's `findings/`, `NN-<sev>-<file>-L<line>.txt`, in the walk shape: the PR URL,
the label line, the deep link, the comment bullets, then Evidence. Its key is `f-` plus its `Id:` line, or a hash of
its path and code (pulls-api, `GET /v1/prs/{id}/findings`). The runner writes it, the walker card and the board edit
it with a hash precondition. The store never holds a finding (review-tab 3.1).

**Which object it is on.** The **PR row** on the room that owns the PR, through the row's `run_dir`. The row's
`walker_task` names the card pasted for it, so the same findings are reached from the card (the walk drawer) and from
the pulls view (the row's `walk` button). The hub holds only the claim, never a copy.

**Its state.** One line per finding in `walk.txt`, written under a lock by `POST /v1/prs/{id}/findings/{key}/walk`.

| state | meaning | `walk.txt` word | key |
| --- | --- | --- | --- |
| open | not looked at yet | `open` | `u` sets it back |
| accepted | clint agrees it should be raised. Not yet posted | `accepted` | `y` |
| dismissed | clint does not want it raised | `skipped` | `s` |
| posted | clint posted it on the forge by hand, with the comment link when there is one | `done` | `d`, and `Enter` copies and opens |
| deferred | come back to it (kept from the walk, rule 9) | `deferred` | `f` |

`done` and `skipped` keep their words on the wire and in `walk.txt`, since pulls-api never renames a value and old
run folders already hold them. The board says posted and dismissed. `accepted` is the one new word. It counts in the
`walk` object of the row as a new key, `accepted`, added beside the others.

**Reviewed.** A PR is reviewed when every finding is posted or dismissed. Accepted and deferred are not yet reviewed,
since an accepted finding is one clint still has to post. The drawer's header and the pulls row say
`N of M: a accepted, p posted, d dismissed`, and the nav count keeps counting `ready` rows with any finding open,
accepted or deferred.

**Nothing is posted by atrium.** There is no route that writes to a forge, and the hub that talks to the forge has no
push or write rights (r-hub-forge). Posting is clint's action per finding: `Enter` copies the finding in the walk
shape and opens its line on the forge, clint pastes and submits, and `d` marks it posted.

## 4. Going through them, on the board and the phone

**The board.** The pasted card's terminal bar shows `walk` once its row is `ready`, because the drawer finds the row
whose `walker_task` is the card. The drawer is the rail, the finding and the card's own terminal side by side, so
asking the card about a finding is `a`, as the review tab designed. The card is in the PR's worktree, so it can read
the code at the head, which is what made the PR #369 walk useful. The pulls view lists the PR with its progress and a
`walk` button that attaches that card.

**The phone.** The same drawer in its phone layout: the rail is a strip above the finding and the terminal is below.
Accept, dismiss and posted are buttons as well as keys, since a phone has no `y`. The item checks that every state is
one tap on a phone width and that the pulls view's row is reachable from the phone's nav.

**The card's prompt.** The pasted card gets the recogniser's prompt as today. It is not told to review, since the
runner reviews. It is told nothing about walking until the review is ready, and clint drives the walk from the drawer.

## 5. What survives

| event | what happens |
| --- | --- |
| room restart, review ready | nothing changes. Files, `walk.txt` and the row are on disk |
| room restart, review running | on start, the room moves every `fetching` or `running` row back to `queued` and starts it. The runner's `begin` reuses the steps the earlier run left in `steps/`, so the run picks up where it stopped and the cost already spent is kept |
| hub restart | nothing. The hub holds the claim only, in its store |
| room offline | the PR waits for that room and the board warns after two minutes (scm-forge 6.6). The findings cannot be read until it returns, as clint decided for the hub (no copy, hub-forge Q2) |
| PR moved to another room | the hub copies the run folder from the old room to the new one before it moves the claim. The new room makes the row with the same head, state and walk, and the old room archives its row. The old room has to be online, and a move to an offline room or from one is refused with a sentence saying so. The walker card moves by the room handoff that asked for the move, and the new room records it |
| PR head moves | out of scope. The review is of the head it was run at, which the run folder's name says. A new paste at the new head is a new row |

## 6. Out of scope

- Posting to GitHub or Bitbucket in any form, a pending review included. That is its own design and needs the hub to
  get write rights, which clint has not given.
- Reading comments back from the forge to mark a finding posted (review-tab option B).
- The second opinion and the settle step.
- A PR list, a review on arrival, polling and webhooks (scm-forge answers 5 and 8).
- A copy of findings on the hub.
- Re-reviewing when the head moves.

## 7. The items

Each item is a worker, based on claude/main, or on `claude/r-hub-forge` once that branch has its REPORT.md. None
edits `prrunner.go`, `prworktree.go`, `forgeaccess.go` or `internal/forge`, which `r-hub-forge` owns tonight.

1. **`r-pr-paste-review`** (room sg3). The dialog sends `POST /v1/prs` on the placed room when it makes a PR worktree,
   tags the card `pr` and `pr:<org>/<repo>#<n>`, and records it with `POST /v1/prs/{id}/walker {"action":"set"}`. A
   paste of a PR whose row already names a live walker attaches that card and starts no second one. On daemon start,
   rows in `fetching` or `running` are re-queued and started. Tests, and before and after PNGs.
2. **`u-pr-finding-states`** (room sgg). `accepted` in `walk.txt`, in the walk route, in the row's `walk` counts and in
   the drawer, the `y` key and the buttons, the board labels posted and dismissed, the reviewed rule of section 3, and
   the phone check. Tests, and before and after PNGs.
3. **`f-pr-review-move`** (the first free room, after `r-hub-forge` lands since it touches `prclaim.go`). A room route
   that answers its run folder as a capped archive and one that makes a row from one, and the hub's move copying the
   folder before it moves the claim. Tests.

## Open for clint

1. **The second opinion.** The runner has no second opinion step yet. Default, built: none, the review is the recipe's
   Claude panel alone. The alternative is a Mercurius round over the finished findings, which the review tab decided
   as its default and which is a runner step to build next if you want it.
2. **A paste that does not review.** Default, built: every pasted PR is reviewed. To get only a card, type a
   directory or tick throwaway. The alternative is a `review` tick in the dialog, on by default.
3. **`accepted` holds the PR open.** Default, built: a PR is reviewed only when every finding is posted or dismissed.
   The alternative counts accepted as reviewed, for a reviewer who posts in one batch later.

## State

Design written 2026-10-04 (828a8cd0).
- Done: `r-pr-paste-review` claude/r-pr-paste-review@5a6935df (sg3).
- Done: `u-pr-finding-states` claude/u-pr-finding-states@ca90179b. Written on sgg (no Go or node there), moved to sg3
  by bundle, built, tested and shot there. The phone action buttons are not seen in a shot, as the headless phone
  viewport cuts the drawer below the rail.
- Waiting: `f-pr-review-move`, for `r-hub-forge` to land (it owns `prclaim.go`). sgg can only write code.

