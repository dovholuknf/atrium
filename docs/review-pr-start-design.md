# A PR card that registers itself with @review

Status: proposed by @review, 2026-09-29, for @rnd before anything is approved or built. Part of item 16
(`docs/review-memory-design.md`), which says how a review is run once @review has it. This says how it reaches
@review when clint starts the card himself.

## What happened, and what clint asked for

clint started a review card by hand for openziti/ziti-tunnel-sdk-c #1441, from a gwt PR worktree. It was titled
after the branch (`normalize.tunnler_id`) and carried the tags `active,gwt,ziti-tunnel-sdk-c`. @orchestrator had to
tell it to alias itself and to ask @review for a brief. Before that brief arrived it had already reviewed the PR on
its own. When it reported, `atrium_report` went to its own card, because a card clint started has no launcher. It
also could not set its own tags. clint wants PR starts to work this way from now on: he starts the card, and it
finds @review on its own.

## Proposal: gwt names the card and gives it a first prompt that points at @review

@dotfiles is changing gwt so that a PR worktree launches its card with a name and a first prompt. @rnd has already
reviewed @dotfiles' side, and this section is that revision with @review's edits. The prompt stays SHORT, because
everything about how a review is run lives with @review and changes weekly (rules 1 to 42 in two days). A prompt
that repeats the rules drifts. A prompt that points at @review cannot.

What gwt sets at launch, through `atrium launch`, and only from `gwt pr` and a PR URL (`-Review`). Reopening an
existing `pr-<n>` worktree launches plainly.

| Field | Value | Why |
|---|---|---|
| title | `gwt-pr-<repo>-<n>: review <org>/<repo>#<n>`, the full repo name | the alias cannot collide with a `pr-<repo>-<n>` card @review launched |
| alias | `gwt-pr-<repo>-<n>`. @review renames an adopted card to `pr-<repo>-<n>` | rule 25 names a review card one way, whoever started it |
| tags | `dept:review`, `review`, `pr`, plus what gwt already sets | rule 30. NOT `atrium:subagent`: clint started it, so it is not one |
| lean | off | a review-manager spawns reviewers, and a lean launch has no Agent tool (until r-005) |
| model, theme | whatever clint's gwt default is | clint's card, clint's choice |
| cwd | the PR worktree, at the PR head | the reviewers read it there, read only |
| first prompt | below | |

The first prompt, @dotfiles' revised draft with @review's edits:

```
This card was opened by gwt for <PR url> (<org>/<repo> PR #<n>), worktree <path>. Before anything else,
atrium_say review: "gwt opened PR <url> at <path>, as card <your card id>". Then wait. Do not review, read the diff
or start subagents until @review answers. @review either adopts this card as the review card for this PR and sends
you a brief, or tells you to stand down. If adopted, follow the brief, which names the run folder, the panel and the
rules. Everything for @review goes by atrium_say to review: atrium_report reaches only a launcher, and this card has
none. If told to stand down, stop.
```

The edits, each with its reason:

- **"Do not review ... until @review answers."** The #1441 card reviewed before its brief came, with a panel nobody
  picked and rules it did not have. The panel is @review's call (item 16), so the card waits.
- **`atrium_say`, not `atrium_report`.** `atrium_report` goes to the card's launcher. A card clint started from gwt
  has none, so a report lands on its own card and nobody reads it. The #1441 card found this out.
- **The brief names the run folder, and it is NOT `<worktree>/.review/`.** The run folder stays at
  `D:/worktrees/claude/reviews/<host>-<org>-<repo>/pr-<n>-<sha7>/`. The board's review tab reads runs only under its
  `reviews_root` setting (`docs/review-tab-design.md`), and its file endpoints answer 403 outside it, so a run folder
  in the worktree never reaches the tab. A worktree is also removed when its work is done, and the review goes with
  it: that is how the zrok #1277 run folder disappeared. And item 16 answer 9 fixed the report path already.
- **`review`, the alias, not a card id.** `atrium_say` takes an alias, and @review's card id changes whenever
  @review is relaunched. The alias does not.
- **One card per PR, and @review decides which.** When @review already has a `pr-<repo>-<n>` card for the same PR,
  the gwt card is told to stand down. Otherwise it is adopted.

## What @review does when the card says it has started

The same as a review @review starts itself (item 16, "How a review is started"), except that it launches no
review-manager. The card that said hello is the review-manager and then the walker.

0. Adopt it or stand it down (one card per PR). On adoption, rename its alias to `pr-<repo>-<n>` and set the tags on
   the board, since a card cannot set its own.
1. Read the PR (`gh pr view`, the diff's size and files) and size the panel. Check the per-repo record of dangerous
   changes.
2. Make the run folder, `D:/worktrees/claude/reviews/<host>-<org>-<repo>/pr-<n>-<sha7>/`, and write `BRIEF.md` into
   it: the target, the panel with its sizing line, the reviewer files at a named dotagents commit, the known
   consumers, the second-opinion setting, the report path, and the rules for writing and for walking. The #1441
   brief is the first one written this way.
3. `atrium_say` the brief's path to the card. The card runs the review and reports with `atrium_say` to @review, and
   then waits. @review applies the repo notes, tells clint the review is ready, and the walk happens on the card.
4. When clint says the walk is done, @review asks the card to leave with `atrium_exit`. The card never culls itself.

## Rejected

- **A SessionStart hook that recognises a `pr-*` directory.** It is magic, and it would be wrong: clint also works
  on his own PRs in `pr-*` worktrees (tlsuv `pr-369` is one), and those are not reviews. gwt knows it is starting a
  review, and a hook can only guess.
- **Putting the rules in the prompt, or a path to them.** The prompt would drift the day a rule changes. The rules
  live in dotagents `general.md` and reach the card in the brief, at the commit @review names.
- **Waiting for the board's recogniser (`docs/scm-design.md`).** It is not built. When it is, its GitHub PR row should
  launch with this same table and this same prompt, and gwt and the board would then be two callers of one launch.

## Open questions

1. Settled by @dotfiles and @rnd: only `gwt pr` and a PR URL pass `-Review`. Reopening a `pr-<n>` worktree does
   not. Still open: does `gwt pr` on one of clint's OWN PRs skip `-Review`?
2. Does a card clint starts count against @review's limit of 2 workers at once? The proposal says no: clint started
   it, and it cannot be queued the way a launch can.
3. Later, and as an atrium change: an `atrium launch --report-to <handle>` that makes `atrium_report` reach @review
   from a card nobody launched. With it, the prompt's last paragraph goes away. Without it, `atrium_say` works now.
4. Where the prompt text lives. The proposal keeps it in gwt, since it is short and says only "ask @review". If the
   board's recogniser is built, the two copies should become one file that both read.
