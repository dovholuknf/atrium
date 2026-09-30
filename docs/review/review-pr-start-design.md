# A PR card that registers itself with @review

Status: proposed by @review, 2026-09-29. @rnd accepted it with changes the same day, applied here. Part of item 16
(`docs/review/review-memory-design.md`), which says how a review is run once @review has it. This says how it reaches
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
existing `pr-<n>` worktree launches plainly, and so does `gwt pr` on one of clint's own PRs unless `-Review` is passed:
gwt compares `gh pr view <n> --json author` with `gh api user --jq .login`. That keeps sessions like tlsuv `pr-369` as
they are.

| Field | Value | Why |
|---|---|---|
| title | `pr-<repo>-<n>: review <org>/<repo>#<n>`, the full repo name | clint's naming rule |
| alias | `pr-<repo>-<n>` when it is free, see below when it is not | rule 25 names a review card one way, whoever started it |
| tags | `dept:review`, `review`, `pr`, plus what gwt already sets | rule 30. NOT `atrium:subagent`: clint started it, so it is not one |
| lean | off | a review-manager spawns reviewers, and a lean launch has no Agent tool (until r-005) |
| model, theme | whatever clint's gwt default is | clint's card, clint's choice |
| cwd | the PR worktree, at the PR head | the reviewers read it there, read only |
| first prompt | below | |

An alias is unique on the board, so the one-card-per-PR rule below decides what happens when it is taken. When
`pr-<repo>-<n>` is free the gwt card holds it from the start and is adopted with no rename. When @review's own card for
that PR already holds it, the gwt launch still succeeds with no alias (the answer carries an `alias_note` and nothing
fails), the card says hello by its card id, and @review stands it down. A rename happens only if @review adopts a card
that started without an alias.

The first prompt, @dotfiles' revised draft with @review's and @rnd's edits. The text lives in gwt and @dotfiles owns
it, so this is the shape it must keep, not a copy to diff against:

```
This card was opened by gwt for <PR url> (<org>/<repo> PR #<n>), worktree <path>. Before anything else,
atrium_say review, with wake=true: "gwt opened PR <url> at <path>, as card <your card id>". Then wait. Do not
review, read the diff or start subagents until @review answers. @review either adopts this card as the review card
for this PR and sends you a brief, or tells you to stand down. If adopted, follow the brief, which names the run
folder, the panel and the rules. Everything for @review goes by atrium_say to review: atrium_report reaches only a
launcher, and this card has none. If told to stand down, stop. If @review has not answered in 10 minutes, write one
line on this card for clint, "waiting for @review, which has not answered since <time>", and end your turn.
```

The edits, each with its reason:

- **"Do not review ... until @review answers."** The #1441 card reviewed before its brief came, with a panel nobody
  picked and rules it did not have. The panel is @review's call (item 16), so the card waits.
- **`atrium_say`, not `atrium_report`.** `atrium_report` goes to the card's launcher. A card clint started from gwt
  has none, so a report lands on its own card and nobody reads it. The #1441 card found this out.
- **The brief names the run folder, and it is NOT `<worktree>/.review/`.** The run folder stays at
  `D:/worktrees/claude/reviews/<host>-<org>-<repo>/pr-<n>-<sha7>/`. The board's review tab reads runs only under its
  `reviews_root` setting (`docs/rnd/review-tab-design.md`), and its file endpoints answer 403 outside it, so a run folder
  in the worktree never reaches the tab. A worktree is also removed when its work is done, and the review goes with
  it: that is how the zrok #1277 run folder disappeared. And item 16 answer 9 fixed the report path already.
- **`review`, the alias, not a card id.** `atrium_say` takes an alias, and @review's card id changes whenever
  @review is relaunched. The alias does not.
- **`wake=true`, and a bound on the wait.** r-007 parks an idle director, and a peer's `atrium_say` to a parked card
  answers `parked` unless it asks to wake it (`docs/rnd/keepalive-policy-design.md` section 7). Without the flag the card
  says hello, gets `parked`, and waits forever. With the 10-minute bound, a card nobody answers says so on its own
  card and stops, not sitting silent.
- **One card per PR, and @review decides which.** When @review already has a `pr-<repo>-<n>` card for the same PR,
  the gwt card is told to stand down. Otherwise it is adopted.

## What @review does when the card says it has started

The same as a review @review starts itself (item 16, "How a review is started"), except that it launches no
review-manager. The card that said hello is the review-manager and then the walker.

0. Adopt it or stand it down (one card per PR). On adoption, set the tags on the board, since a card cannot set its
   own, and set the alias `pr-<repo>-<n>` only if the card started without one. A gwt card does not count against
   @review's limit of 2 workers at once, because clint started it and it cannot be queued the way a launch can. Past
   the limit @review still adopts it and tells it to wait: an idle card burns nothing, and r-007 parks it.
1. Read the PR (`gh pr view`, the diff's size and files) and size the panel. Check the per-repo record of dangerous
   changes.
2. Make the run folder, `D:/worktrees/claude/reviews/<host>-<org>-<repo>/pr-<n>-<sha7>/`, and write `BRIEF.md` into
   it: the target, the panel with its sizing line, the reviewer files at a named dotagents commit, the known
   consumers, the second-opinion setting, the report path, and the rules for writing and for walking. The #1441
   brief is the first one written this way.
3. `atrium_say` the brief's path to the card. The brief names the head as the run folder's sha7. If the PR head moved
   between the hello and the brief, the card checks out the sha7 the brief names, or @review names the new head and
   its run folder (rule 17). The card runs the review and reports with `atrium_say` to @review, and then waits.
   @review applies the repo notes, tells clint the review is ready, and the walk happens on the card.
4. When clint says the walk is done, @review asks the card to leave with `atrium_exit`. The card never culls itself.

## Writing the run folder from a card whose cwd is the worktree

The findings, the report and `walk.txt` all go under `D:/worktrees/claude/reviews/`, outside the card's working
directory. No edit there may wait on a permission prompt, since nobody watches a review card's terminal while its
panel runs.

- **The card is gated through atrium.** `atrium launch` supplies `ATRIUM_PERM_GATE=on` as a default
  (`permGateDefault` in `internal/daemon/launch.go`), so every tool call goes through the permission chain although
  the card never ran `atrium join`. The #1441 card shows it: its `Write` calls into the run folder are in the
  permission history, answered by board-wide auto mode.
- **Two standing folder rules, in place before auto mode is ever off during a review.** Kind `path`, prefix
  `D:/worktrees/claude/reviews/`, decision approve, one for `Edit` and one for `Write`, since a rule names one tool.
  Board-wide auto mode covers the #1441 card today, and a review should not depend on it. The rules are a standing
  approval on clint's machine, so they are added only when clint says yes.
- **An allow from atrium's hook clears Claude Code's own prompt for a write outside the working directory.** Checked
  on Claude Code 2.1.284, 2026-09-29: `claude -p` in manual permission mode, prompts answered by nobody, asked to
  `Write` a file in a sibling directory. With no hook the write was denied and no file was created. With a PreToolUse
  hook answering `permissionDecision: "allow"` the write went through and the file exists. So the brief does not have
  to arrive before the card starts, as an extra directory on the launch, which gwt could not know yet.

## Rejected

- **A SessionStart hook that recognises a `pr-*` directory.** It is magic, and it would be wrong: clint also works
  on his own PRs in `pr-*` worktrees (tlsuv `pr-369` is one), and those are not reviews. gwt knows it is starting a
  review, and a hook can only guess.
- **Putting the rules in the prompt, or a path to them.** The prompt would drift the day a rule changes. The rules
  live in dotagents `general.md` and reach the card in the brief, at the commit @review names.
- **Waiting for the board's recogniser (`docs/runtime/scm-design.md`).** It is not built. When it is, its GitHub PR row should
  launch with this same table and this same prompt, and gwt and the board would then be two callers of one launch.

## Settled with @rnd and @dotfiles

1. Only `gwt pr` and a PR URL pass `-Review`. Reopening a `pr-<n>` worktree does not, and neither does `gwt pr` on
   one of clint's own PRs unless `-Review` is passed.
2. A gwt card does not count against @review's worker limit. Past it, @review adopts the card and tells it to wait.
3. The prompt text lives in gwt. If the board's recogniser is built, the two copies become one file that both read.
4. No `gwt-` prefix. The alias is `pr-<repo>-<n>` from the start, and one card per PR makes the prefix unneeded.

## Open

1. For clint: the two folder rules for `D:/worktrees/claude/reviews/`, `Edit` and `Write`. A yes adds them.
2. Later, and more general than `--report-to`: a `report_to` field on `/v1/launch` (and `atrium launch
   --report-to`) that sets the card's launcher in the ledger. Then `atrium_report`, the silent-stop notice and
   owed-report tracking (r-001's area, item 41) all reach @review. It is an @runtime item. Until it exists, the
   prompt's `atrium_say` line stays.
3. `wake=true` on `atrium_say` arrives with r-007. Today the tool refuses the call (`unexpected additional properties
   ["wake"]`) and the hello is lost, so gwt ships the prompt without `, with wake=true` and keeps the 10-minute line.
   When r-007 ships `wake`, @review tells @dotfiles and the words go back in.
