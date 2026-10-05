# r-pr-paste-review

## Built
1. **Paste starts the review.** `internal/api/web/js/fixtures.js`: new `startPastedReview`, called from `launchNow` after
   `makePastedWorktree` and before the card launch, with the room header the launch will use. Only when a PR worktree was
   made (so not for a typed directory, the throwaway tick, or a host with no provider). A `POST /v1/prs` error goes to
   `l-link-note` and the card still launches. The room header is now worked out once, before the launch.
2. **The card is the walker.** With a row, the card gets tags `pr` and `pr:<org>/<repo>#<n>`, and `setPastedWalker` posts
   `{"action":"set","task":<card id>}` to that room, then `pullsApplyRow` puts the answered row in `pulls.rows`.
   Checked: `walk.js` `walkPrOf(taskId)` is `pulls.rows.find(r => r.walker_task === taskId)`, so it finds the row from
   that moment, and the check script asserts it. A failed set is shown in the note and does not stop the launch.
3. **Second paste.** `created: false` and `GET /v1/tasks/<walker_task>` answering a card not `done` or `dead` closes the
   dialog, toasts "already under review, attached its card" and attaches it. A dead or missing walker launches a new card
   that becomes the walker.
4. **Resume.** `Store.RequeueInterruptedPRs` (`internal/store/prs.go`) moves `fetching` and `running` rows to `queued`
   (run_state and run_error cleared, folder, cost, started_at kept) and answers their ids. `Daemon.resumePRReviews`
   (`daemon.go`) calls it at the top of `Run` and hands each id to `prRunner.Start`.

## Does `begin` reuse the steps?
Yes. `begin` reads `review.json` in the row's run dir (steps, spent cost, budget flags) and carries the cost onto the
row. In `step`, a step recorded `Done` is called again as a reload: if its function finds its output files in the folder
and returns nil, nothing is run. `fetch` reloads when `bundle.md`, `pr.json` and a Done record exist, otherwise it
fetches again. Not reusable ones fall through and run for real.

**What a resumed run redoes:** the step that was in progress when the daemon died (its `Done` was never saved, so it
starts from its beginning, including every fork it had running, and what that step spent before dying is lost if it was
not yet written to `review.json`). The `write` step is always redone. A step whose outputs are missing or unreadable is
redone. Finished steps are not asked of the model or the forge again. One more caveat: a run cut inside `fetch` before
its folder was renamed to the head still has the `pending` folder, and the row keeps that run_dir, so it resumes there.
Rows are resumed with whatever forge the daemon has at that moment (the `Start` call happens at `Run`, after `New` wired
the forge callbacks).

## Tests
- `internal/store/prs_test.go` `TestRequeueInterruptedPRsMovesOnlyTheCutOnes`.
- `internal/daemon/prrunner_test.go` `TestPRRunnerResumesAReviewTheRestartCut`: full run, row cut back to running with
  the findings gone, requeued, started: ends ready, no more forks, merges or `gh pr view` calls, findings written again.
- `scripts/check-pr-paste.js` extended: the review request (body, room, before the card), the tags, the walker set,
  `walkPrOf`, a refused review not stopping the card, a second paste attaching with no second launch, a dead walker
  being replaced, a typed directory starting no review. It passes. Needs playwright (I installed it in a scratch dir and
  used NODE_PATH, the repo has none).
- `go test ./internal/store` and the `TestPRRunner*` daemon tests pass. `./internal/api` fails in `prworktree_test.go`
  on `Couldn't load public key ...id_ed25519_sign.pub` (git commit signing on this machine), nothing I touched.

## Screens
`docs/screens/r-pr-paste-review/`: `before-dialog-after-paste.png`, `after-dialog-after-paste.png` (the note after the
worktree is made, review started), `before-pulls-no-row.png` (old code: nothing in the pulls view after a launch) and
`after-card-with-walk.png` (the fixture row ready, with its `walk` button). The after shot's "no pulls here" line is the
mock's stale placeholder, not part of the change. Before shots came from the old `fixtures.js` run through the same script.

## Not done / notes
- No forbidden file was touched. The Go change is in `store/prs.go` and `daemon.go` only.
- The attach-on-second-paste path needs the walker's card to be known to the room asked. Through the hub the `GET
  /v1/tasks/<id>` goes with the placed room header, a card id from another room that the hub cannot find counts as not
  live and a new card is launched.
- `launchResolved.url` is the pasted url sent to `POST /v1/prs`.
