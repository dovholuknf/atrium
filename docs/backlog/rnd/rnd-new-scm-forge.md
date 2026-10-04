# rnd-new-scm-forge: a provider that knows its forge, so PRs come from providers

Filed by the orchestrator, 2026-10-01, from clint. HELD: clint said "not just yet. capture it for now." Do not
start until he says go.

clint: "i want to start advancing my 'pr process' but to do that i'll need scm providers"

## What exists

- Named git providers, built 2026-09-16 (`docs/providers-design.md`, `docs/backlog/backlog-2026-09-13-001.md`):
  root folder, worktree toggle and root, discovery of org/repo checkouts. Type is a field, only `git` exists.
  Still open from that item: provider export (question 5) and the provider label on a card (question 7).
- `docs/scm-design.md`: inbound URL recognisers (a PR or ticket URL fills a launch dialog) and outbound config as
  files. Nothing built.
- `rnd-new-pulls-view`: the pulls view design. P1 (PR review runner) landed b07cbfb2 and ff5def61. P3 (wire the
  view to real routes) held on sg3.

## What is missing

A provider knows a directory layout, never a forge. Nothing lists a repo's open PRs, the PRs that request clint's
review, or the PR behind a branch. Today PRs reach atrium only as gwt-opened cards.

## What the spike must answer

1. **Forge on a provider.** A `forge` field (github, bitbucket, gitlab, none) beside `type: git`, or a separate
   forge row a provider points at. Which, and why.
2. **The credential rule.** Atrium holds the NAME of a command (`gh`, `bb`), never a token. What each forge's
   command must answer, and what happens when it is missing or logged out.
3. **What the forge answers.** List open PRs for a repo, PRs requesting review from me, PR by URL, PR for a branch,
   the diff. Which of these the pulls view and the PR runner need first.
4. **Intake.** Reuse the `docs/scm-design.md` URL recognisers and `docs/intake-design.md` sources rather than a
   second intake. A source polling "review requested" per provider is the likely first use.
5. **Worktree for a PR.** The provider already computes `<worktree_root>/<org>/<repo>/<branch>`. A PR checkout
   uses that, so gwt is no longer the only way in.
6. **Hub vs room.** Providers are per room (paths are per machine). Where the forge call runs.
7. **Staging.** Stage 1 useful alone: GitHub via `gh`, PR list per provider in the pulls view.

## Depends on

- Providers (built). Pulls view P3 (held). Best after rnd-new-pulls-view, which it feeds.

## Output

A design doc on claude/rnd, options and one recommendation, staged, then @review. No code.
