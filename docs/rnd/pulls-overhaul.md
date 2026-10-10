# The pulls tab overhaul

Status: design draft. Backlog item u-pulls-overhaul, HIGH. Nothing here is built.

clint on 2026-10-08, over a screenshot of the tab: it needs a "fucking sexy" overhaul.
This is the design that comes first.
The picture of today is `docs/changes/img/u-pulls-overhaul-before.png`, drawn by the pulls section of
`scripts/test-board-headless.js` with its mock rows.

Already shipped and not redone here: a failed panel is one stage with a muted `.pull-stage` line, not the PR's state.

## What is wrong today

- A row says nothing about what the reviewer found. There is a severity tally, but no verdict, no blocker count and no
  test result.
- There is no way to reach the review card or its terminal from a row until the single `walk` button, and that one only
  shows once the state is `ready`.
- Two full-width inputs (link, why) take the top of the tab for a rare action. The quick paste already does it.
- retry, log, move and close are equal-weight grey buttons. Nothing says which one to press.
- "leftovers" and "move" do not say what they do.
- A stage that failed is an error sentence in the state slot, not a place in a sequence.

## What the data has today, and what it lacks

The row is `store.PRReview` (`internal/store/prs.go`) wrapped by `prView` (`internal/api/prs.go`), drawn by
`pullRowHTML` in `internal/api/web/js/pulls.js`.

- repo#number, title, author, head7, why, cost, age, disk: yes. They are `org_repo`, `number`, `title`, `author`,
  `head7`, `why`, `cost_usd`, `created_at` with `ready_at` and `archived_at`, and `disk_bytes`.
- Findings by severity and leaks: yes, as `findings` {high, med, low, nit, leak}, filled only once the row is `ready`.
  Walk progress is `walk` {accepted, done, skipped, deferred, open}. The second opinion is `second`.
- Blocker count: yes, as a proxy. `findings.high` is it, since the renderer counts its `blocking` severity as `high`.
- Review card, working or waiting or done: partly. `walker_task` names the card. Its status is on the board's own task
  list, so the board joins the two. No API change.
- Verdict (approve, changes, blocked): no. Nothing writes one. Interim rule on the board: any `findings.high` is
  blocked, otherwise any finding is changes, otherwise approve. Real fix: the panel writes `verdict` into review.json
  and `viewPR` reads it.
- Tests run and result: no. Each finding has `proven` and `test_ask` (`internal/prreview/render/types.go`), but nothing
  totals them. Needs a `tests` object in review.json {ran, passed, failed, skipped} read by `viewPR`.
- Stages (worktree, deps, panel, review): partly. review.json has a `steps` map and `readPRStageNote` reads only the
  panel note. The API exposes no per-step status. Needs `stages` on `prView`, a list of {name, state} in run order.
- Owning room: partly. `claim` is on the row, and `/_hub/pr-claims` has the room name, used today only by move.
So three things need the daemon: a verdict, a tests total and per-stage status. Everything else is a board change.
The card's live state is a board-side join, so it needs no daemon work.

## The row

One row is two lines of text and a strip. Left to right on the top line: repo#number, title, author. Under it: the
reviewer's finding, then the stage strip. On the right: the card state, cost and age, ONE primary button and a
`...` menu.

Stage strip glyphs: `[x]` done, `[>]` running, `[ ]` not reached, `[!]` failed. Four stages in order:
worktree, deps, panel, review.

### State: working (card or run is going)

```
 openziti/zrok #11  Add reconnect backoff to the agent                          ekoby
 [x] worktree  [x] deps  [>] panel  [ ] review                  working   $0.71 . 12m
                                                              [ Open card ]  [...]
```

Primary: Open card. It attaches the walker's terminal. If no card exists yet the button reads Start review and it
starts one.

### State: waiting on you (card asked something)

```
 openziti/zrok #11  Add reconnect backoff to the agent                          ekoby
 [x] worktree  [x] deps  [x] panel  [>] review         waiting on you   $1.04 . 31m
 the card asked: keep the retry cap at 5?                     [ Answer ]  [...]
```

Primary: Answer. It opens the card with its question in view. The question text comes from the card's own pending
question. It is drawn as a muted line and never as the state word.

### State: done, approve

```
 openziti/zrok #11  Add reconnect backoff to the agent                          ekoby
 APPROVE   0 blockers . 3 low . 2 nit . tests 41 ran, 41 passed     done   $1.92 . 40m
 [x] worktree  [x] deps  [x] panel  [x] review              [ Post review ]  [...]
```

Primary: Post review. It walks the findings and posts them, which is today's `walk`. The verdict word is green.

### State: done, changes

```
 openziti/zrok #14  Rework the tls handshake                                    ekoby
 CHANGES   2 blockers . 6 med . 7 low . tests 38 ran, 2 failed          done   $2.40 . 1h
 [x] worktree  [x] deps  [x] panel  [x] review              [ Post review ]  [...]
```

Primary: Post review. The verdict word is amber when there are no blockers and red when there are. A row with blockers
also shows the blocker count in the red word, so "BLOCKED 2 blockers" is the same row with the word changed. Tests that
failed draw their count in red.

### State: failed stage

```
 openziti/zrok #13  Bump deps                                                   ekoby
 [x] worktree  [!] deps  [ ] panel  [ ] review                      failed   $0.00 . 9m
 deps: npm install exited 1                                      [ Retry ]  [...]
```

Primary: Retry. The muted line is the stage note and nothing more. The row state word stays "failed" and the stage that
broke is the one marked `[!]`. A failed panel keeps the shipped behaviour: it shows as one stage with its note, and the
review carries on.

### Other states

Queued shows the strip with nothing reached and the primary Review. Aborted shows the strip where it stopped and the
primary Retry. A closed row (archived) has no primary and no menu, only "closed 3d ago".

## Primary action per state

| State | Primary | Calls |
|---|---|---|
| queued | Review | `POST /v1/prs/{id}/start` |
| fetching, running, card working | Open card | attach `walker_task`, or `POST /v1/prs/{id}/walker` when none |
| card waiting on you | Answer | attach `walker_task` with the question in view |
| ready, nothing posted | Post review | today's `pullsWalk` |
| ready, partly posted | Continue review | same call, drawer opens where it stopped |
| ready, all posted or dismissed | Open card | attach only |
| failed, aborted | Retry | `POST /v1/prs/{id}/retry` |
| closed | none | |

## What goes into the `...` menu

Menu items carry their meaning in the label, since "move" and "leftovers" did not.

- Abort the run (while queued, fetching or running)
- Show the run log (failed rows)
- Move to another room (only when more than one room is attached). Label: "Move review and card to another room"
- Close card and free its worktree, branch and review (when a card exists). Keeps the existing confirm dialog
- Open on GitHub
- Copy PR link

The header button "leftovers" becomes "Find unclaimed worktrees" in a header menu, with its result unchanged.

## What is cut

- The link input and the why input at the top of the tab. The quick paste (the board's one-call open verb, the same
  `/v1/open` the paste button uses) does the same thing. The `+ paste a PR` button stays as a single control that opens
  a small popover with one link field and an optional why field, so the rare case is still reachable and takes no room.
- The `head7` chip on the top line. It moves into a hover on the repo#number.
- The "ready to walk" and "N of M: ..." sentences in the state slot. Progress is the stage strip and the verdict line
  carries the finding counts.

## Phases

Each phase is one worker, one branch and one merge.

1. **Data: verdict, tests, stages (daemon).** Add `verdict`, `tests` and `stages` to `prView`, read from review.json.
   Done when: the API doc `docs/review/pulls-api.md` lists the three fields, a Go test over a fixture review.json
   asserts each, and a row with no review.json still answers with them empty.
2. **Row layout (board).** New `pullRowHTML` with the two-line layout, the stage strip, the verdict line and the
   `...` menu, still using the shipped buttons' calls. The interim verdict rule runs when the daemon sends none.
   Done when: the headless pulls section passes, check-pulls-closed.js passes, and an after picture is committed at
   `docs/changes/img/u-pulls-overhaul-after.png`.
3. **Card state and primary action (board).** Join `walker_task` to the task list for working, waiting and done.
   Wire the primary button per the table above, including Answer. Done when: a headless test per row in the table clicks the
   primary and asserts the call made or the card attached.
4. **Cut the inputs and rename (board).** Remove the two inputs, move paste into a popover, rename leftovers and move.
   Done when: no `#pulls-url` or `#pulls-why` is on the tab, the paste popover still opens a card (headless), and the
   menu labels are the sentences above.
5. **Polish.** Colour of the verdict word, hover details, narrow-width layout that stacks the right column under the
   strip. Done when: shots at 1280 and 390 wide are committed and read well to clint.

Phases 2 and 3 can start once phase 1's field names are fixed, since the board draws the interim rule meanwhile.

## Open questions for clint

1. Who decides the verdict? If the panel writes it into review.json, then the row shows exactly what the reviewer said,
   and a PR with no verdict yet shows nothing. Recommended: the panel writes it, and until then the board uses the
   blocker rule above, drawn in a dimmer style so it is not mistaken for the reviewer's word.
2. Is a finding of severity `high` a blocker? If yes, then the blocker count is `findings.high` and needs no new data.
   Recommended: yes, since the renderer already folds `blocking` into `high`.
3. Where do the test totals come from? If the reviewer's `proven` lines are totalled by the panel into review.json, then
   the row can say "41 ran, 2 failed". Recommended: the panel writes a `tests` object, and a row with none shows
   "tests not run" and not a zero.
4. Does Answer open the card or answer inline? If it opens the card with its question in view, then no new input
   widget is needed on the row. Recommended: open the card.
5. Does the paste popover replace the two inputs, or do you want them gone entirely? If it replaces them, then the rare
   why text is still reachable. Recommended: the popover.
6. Should a closed row show by default? If the default filter stays "open", then closed reviews leave the tab at once
   and "all" brings them back. Recommended: keep it as it is.
