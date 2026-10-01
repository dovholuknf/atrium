# Review of r-deploy-ready-bound 1b263964 (@runtime, the deploy-ready hang on the live hub)

Range `e7bc8868..1b263964`, one commit on claude/r-deploy-ready-bound. Hub side: `internal/deployready/` and
`internal/link/deployready.go`. Asked: can a cached "carries nothing" still be wrong, and can a stale answer arm a
click.

## Asked: can a stale answer arm a click

No. Every path was checked:

- The click calls `readyReport(ctx, true)`. `fresh` skips the 10 second reuse, so the click never reads a cached
  answer.
- If a pass is in flight, the click gets the last answer with `Stale` true and `Ready` forced false, and `startDeploy`
  refuses `Stale` with 409 before the `!Ready` check. With no last answer it gets `readyUnknown`, which is not ready.
- A pass that hits the 5 second bound answers `readyUnknown`, and its `lastAt` is zeroed so the next ask reads again.
- The tip comparison still runs against the click's own fresh pass.

## Asked: can a cached "carries nothing" still be wrong

**Yes, proven.** See finding 1.

## Also checked

- `rev-list --first-parent --parents` prints every parent of a merge (checked on this repo at e7bc8868), so
  `coverRead` finds the merges of a first-parent range.
- `--max-count` counts commits after `--merges` filters them, so the merge read in `coverRead` covers the same
  commits as the rev-list beside it.
- A cut-off read is not cached: `fillMerges` checks `ctx.Err()`, `cover` caches only with no error and a live
  context, `verdicts` returns the context error, and `Check` fails on `ctx.Err()` after `candidates`.
- The gitsync runner's own bound is 10 minutes, so it cannot fire inside the 5 second pass and look like a non-context
  error. Its stop is shutdown, and the cache dies with the process.
- `pinnedSpec` caches only specs made of commit names. A branch or tag in a spec is read again every time.
- The `deployReadyState` lock is no longer held across git. `checkerFor` takes it briefly, and the `Checker` has its
  own lock.
- `go vet` and `go test -count=1` of `./internal/deployready/` (60s) and `./internal/link/` (160s) pass at 1b263964.
  I did not run it against the live hub.

## Findings

1. **MEDIUM, proven, older than this branch and made wider by it.** A git error on the merge read that is not the
   pass's own context marks every unread merge in the range as "carries nothing", and that stays cached for the
   hub's life. A merge with a conflict resolution then needs no verdict, so the pass answers **ready** and arms the
   click. `fillMerges`: `if err != nil { if ctx.Err() == nil { for _, sha := range todo { c.put(sha,
   commitInfo{}) } } }`. The proof is
   `D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-1b263964/zz_scratch_review_test.go`. It builds the
   conflict merge from `TestMergeNeedsAVerdictOnlyForItsResolution` and fails the first `--remerge-diff` call once. Both
   passes answer `ready`, the second with git working again. It fails the same way against claude/main's checker,
   where `fillMerge` put `{}` on any `show` error. The batch makes one error cover every merge in the range rather
   than one. Fix: on any error that is not the context, cache nothing and return the error so the pass answers
   unknown. If a git too old for `--remerge-diff` must keep working, recognise that one error by its message and
   say so in a note, rather than treat every failure as "too old". Also cache only the merges that `parseNamed`
   actually returned, not all of `todo`. The same holds for the `-p` call. A non-context failure there caches paths
   with no patch, which blocks that merge until the hub restarts. That fails closed, but it never clears.
2. **Low.** A pass started by a GET that changes the answer fires no `deploy-ready` event. Only the minute tick
   compares signatures. On a cold hub the first board's GET hits the 5 second bound and shows unknown, other boards
   get `still reading git` or stale. The pass that finishes later tells nobody, so pills show unknown for up to a
   minute. Move the signature compare and `deployReadyChanged` into `readyReport`, after `st.last` is set.
3. **Low.** `coverRead` caps a verdict range at `--max-count=2000` without saying so. A longer range is read in part
   and cached that way. It fails closed, since the rest stays uncovered, but the blockers carry no note naming why.
   `candidates` refuses a range over the cap. Do the same here, or add a note.

Quality: after the Sonnet switch, the pattern seen before holds: correct and well measured on the path the rule
names, the context, and missing the other branch of the same `if`. The question it asked me was the right one.

**HUB DEPLOY OK e7bc8868..1b263964.** The hang is live and worse than finding 1, which needs a real git failure and
was already on claude/main. Finding 1 should be the next fix to deploy-ready.
