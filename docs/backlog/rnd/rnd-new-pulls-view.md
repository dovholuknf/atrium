# rnd-new-pulls-view: pull request review as a first-class flow, with its own view

Filed by the orchestrator, 2026-10-01, from clint: "pull request review flow sucks at this time. this software
factory should prolly have a dedicated pulls type of view or scm or something and atrium should facilitate pr's.
right now it's janky and clunky and not working and not token efficient nor fast."

## What went wrong today (tlsuv PR 378), as evidence

- gwt opened card pr-tlsuv-378, which then had to announce itself to @review and wait for adoption. @review held it
  under a pause and "asked clint", and the question never reached him. Twenty minutes lost before any review.
- clint told the card to go. It ran review-panel with no brief: no run folder, no walk.txt, none of the walk rules
  (31-42). He got a flat summary, not the walk he uses.
- Three helper sessions shared one BRIEF.md in the worktree and overwrote it. The C systems review had to rerun.
- The helpers were full claude sessions. Each reported done and then stayed alive, burning nothing but showing as
  running, until the orchestrator exited them (r-new-exit-on-report now does this).
- The helpers carried the parent's title, sat beside it in the list, and the parent raised a "ready" alert while it
  waited on them (u-new-no-ready-while-children-run).
- Duplicated text in the final summary (the Fit block printed twice).
- Token cost: a director relaying, a card announcing, four full sessions each reading the whole diff, a summary pass.

## What clint asked for

- A dedicated place on the board for pull requests: a "pulls" (or "scm") view, not cards mixed into the agent list.
- Atrium facilitates PRs end to end, instead of a chain of agents passing messages.
- Fast and token-efficient.

## What the design must answer

1. **The view.** What one PR row shows: repo, number, title, author, state of our review (queued, reviewing,
   walking, done), finding counts by severity, link. What opening a row shows: the walk, item by item, with state
   from walk.txt, and the deep links.
2. **Intake.** A PR gets into atrium how: gwt, pasting a URL, a source polling review requests. Reuse
   `docs/scm-design.md` (URL recognisers) and `docs/intake-design.md` rather than invent a second intake.
3. **The pipeline without a director in the loop.** The brief, run folder and panel come from a stored recipe, not
   from @review typing them. Nothing waits on a human or director to "adopt". Pauses do not apply to clint's PRs.
4. **Token budget.** One diff fetch shared by every reviewer, not one per session. Which reviewers need a full
   session and which can be one-shot calls. Run folder per reviewer so nothing overwrites a shared file. A target
   cost per PR, measured on 378.
5. **The walk on the board.** Can the walk happen in the pulls view (answer per item: done, skip, defer, comment)
   instead of in a terminal, with walk.txt as the record. Posting comments to GitHub stays clint's action.
6. **What is built where.** Hub vs room, store tables, what the board needs, staged so stage 1 is useful alone.
7. **What is out.** Atrium holds no GitHub credential of its own (the overlays rule: it may hold the NAME of a
   command that has one, `gh`, never a token).

## Output

A design doc on claude/rnd with options and one recommendation, staged, with owners and acceptance per stage, then
@review. Measure against PR 378: how long and how many tokens it took today, and what the design would take.
