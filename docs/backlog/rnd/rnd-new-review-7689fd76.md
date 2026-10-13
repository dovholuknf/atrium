# rnd-new-review-7689fd76: @rnd design review of docs/rnd/pr-review-story.md

Status: open. @rnd, 2026-10-01. Review of `claude/pr-review-story` 7689fd76 off claude/main 610819c1, rebuilt on m1mini
from the patch (same blob 35827b60, same commit id). It was checked against `docs/rnd/pulls-view-design.md`,
`docs/rnd/review-tab-design.md`, `docs/review/review-pr-start-design.md`, `docs/review/review-memory-design.md`, and
the built runner, renderer, store, API and web (r-pr-run at a2a1909d on claude/main). clint's six open questions are
his, so this does not answer them. Two findings add a risk next to questions 3 and 5 so that his answer is informed.

**Verdict: OK for clint's read. Fold the four MEDs before any stage is sent to an owner.** Nothing in the story is
wrong about what is built, but its stages and ids run beside pulls-view section 9 instead of on it, and two of its
defaults touch the hole @review just closed in r-pr-run.

## MED 1. The stages and ids duplicate pulls-view section 9, and the ids cannot be checked

- Story stage 1's acceptance (walk 378 in the new order and say whether item 1 is right) is pulls-view P2's
  acceptance, word for word. Story stage 4 (steps 6 and 7, the review-requested source, the recipe editor, the head
  check) is pulls-view P3. The gwt door (A6) is also P3. pulls-view's E2E, the test @rnd owns that closes the design,
  is not mentioned at all.
- The A1 to A18 and G0 to G9 ids come from `D:/tmp/backlog-deps/BACKLOG-DEPS.md`, an untracked file in sg4's tmp. A
  director on m1mini or sg3 cannot read it, and it is not in the repo, so @rnd could not check a single A or G id.
  "item 16" is a third scheme (review-memory). The names that are in the repo are right: r-pr-render, r-pr-store,
  r-pr-run, f-pulls-hub, pulls-p3 and u-005 all match their changelogs and backlog files.
- **Fix:** say per story stage which pulls-view stage it is, extends or replaces (P2 acceptance, P3, E2E). Replace
  each A or G id with the repo name of the item (or commit BACKLOG-DEPS.md under `docs/backlog/`). Say how the E2E
  changes if clint takes the defaults.

## MED 2. "The walker as a fork of the prime" is a launch change, and must keep r-pr-run's no-settings rule

- Built: `launchWalker` (`internal/api/prsdrawer.go:522`) launches a fresh card with cwd set to the run folder, the
  recipe's `walker_brief` as its prompt, and no resume. The prime runs in `<run>/work` with `--setting-sources ""`,
  `--tools Read,Grep,Glob` and `src/` through `--add-dir` (`internal/daemon/prrunner.go:770-790`, @review's PR-hooks
  HIGH, fixed at 8c9b17d7 and a2a1909d).
- A forked walker needs a resume and fork-session on launch, with the prime's cwd (claude finds a session by its
  project folder). It also needs the same empty setting sources, because a card launched the normal way gets
  leanArgs' `project,local`. If the walker's cwd ever becomes `src/`, or picks up project settings, the PR-hooks hole
  reopens on the one session that has full tools.
- **Fix:** the story says (2.3 and stage 1) that this is a launch change owned by @runtime, and states the invariant:
  the walker never treats `src/` as a claude project. Next to question 5, name this as part of its cost, beside the
  $0.17 re-read.

## MED 3. `prove` runs the PR's code on the review room

`go test`, `make` or a CMake build run the PR's own code and build scripts as the daemon's user, with whatever
credentials it can reach (gh's token, the ssh agent, the room's env). That is the same class of risk as the PR-hooks
HIGH that held r-pr-run. Question 3 asks only about scope (only on `prove`, never on its own, never pushing). The
story should state the risk next to question 3, and stage 2 should name the containment `prove` runs under before it
is built: a separate user or container, no credentials in its env, no network beyond module fetch.

## MED 4. Question 1's default changes the E2E, and the walk button is already built

- pulls-view's E2E closes on a PR "walked in the pulls view on the live board". pulls-view 7 keeps the drawer as a
  walk without a walker. Story stage 1's default (question 1) is the walker's terminal, with the drawer only in stage
  3, behind A3, which is on HOLD c0bccc01. If clint takes the default, the E2E wording has to change with it. The
  story should say so.
- 4.1 and stage 1 say "the button is part of A3". It is not. P2 built it: `internal/api/web/js/pulls.js:194`
  ("launch walker", `POST /v1/prs/{id}/walker`). So stage 1 needs nothing from A3, which makes it more useful alone
  than the story claims.

## LOW 5. The gwt door supersedes review-pr-start-design, and should say so

Section 1 says `gwt pr 4397` "the way he does today" ends in `POST /v1/prs`. Today gwt launches a card that finds
@review (`docs/review/review-pr-start-design.md`). pulls-view 6 names that design as the one to change. The story
should too, and keep its exceptions: clint's own PRs and a reopened `pr-<n>` worktree launch plainly unless
`-Review` is passed.

## LOW 6. A group that spans bands has no rule

3.1 says severity decides between bands, then a group walks "where its highest-ranked member would be". If a group
holds a MED and a LOW on the same function, does the LOW come up into the MED band ahead of other MEDs? Say which
rule wins. `group` and `take_or_leave` are new over pulls-view 5.6. The existing `cause` field is
introduced or pre-existing, not a group, so nothing is duplicated.

## LOW 7. Enter = copy and open is review-tab 2.4's

Stage 3 offers `Enter` (copy the comment and open its line) as new. review-tab 2.4 already designs it as the drawer's
primary action. Cite it rather than restate it.

## Checked and right

- Rule numbers and wording, 5, 9, 14, 16, 23, 24, 27, 31 to 44, against `review-memory-design.md`.
- The `walk.txt` line format (`render.WalkLine`) and the finding file names (`render.go:82`). The states open, done,
  skipped and deferred (`prsdrawer.go:389`).
- The renderer's order (disputes, band, rank, diff order, `render.go:117`) and `Suggested fix:` only for
  `proven: code` (`check.go`). `proven: test` is stage 2, as pulls-view 5.6 left it.
- The seeded panel (`DefaultPRPanel`) and the five-sentence `DefaultWalkerBrief` (`internal/store/prs.go`).
- `lessons.json` is in the `repo_notes` add shape, with @review as the only writer of reviewer files.
- A PR clint asked for is never held by a pause, as in pulls-view 6.

## Are the stages useful alone

Stage 1 is, more so than it says (MED 4): the walker brief rewrite is a recipe row edit, and the button exists. Its
acceptance is the same 378 replay @runtime owes for P1, held by the pause, so one run serves both. Stage 2's `prove`
is useful alone once MED 3 is answered. Its memory half waits on review-memory item 16's three questions for clint.
Stage 3 waits on A3 (HOLD c0bccc01). Stage 4 is P3.
