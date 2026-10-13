# gwt board verbs: twig and backport from the board

Interview result for the gwt parity work (`docs/rnd/gwt-parity.md`, section "What gwt does not settle"). gwt runs
in a terminal and prompts. The board has no prompt, so these answers decide where gwt stops deciding. gwt source:
`powershell/onpath/git-worktree.ps1` in dotfiles, twig at 2616, backport at 2382.

All menu labels use three plain dots (`...`), never the ellipsis character.

Every worktree these verbs make runs the dotagents setup and the repo hook before its card launches, as gwt does
(`_InvokeGwtHook`). That is g-worktree-setup-hook, which g-twig, g-backport and g-new-dialog depend on.

## 1. Where the verbs live

**"+ new agent" becomes a menu.** The button in the top bar (`internal/api/web/index.html:213`, today a direct
`openLaunch()`) opens a menu, and each entry opens the launch dialog in that mode:

- **New agent...**: today's dialog, pick a folder and a runner.
- **New branch from a repo...**: pick a repo, name the branch, worktree off a freshly fetched `origin/main`, card
  launched. This is `gwt new` and is the g-new-dialog item.
- **Open a link...**: a PR, issue, advisory, Discourse or Zendesk link, routed by the existing open verb.

**Twig is a card menu item**, labelled **New worktree from this worktree...**, beside "new agent here"
(`internal/api/web/js/card-menu.js:789`). It needs a source worktree, and the card is that source, so it is not on
the top-bar menu.

**Backport is not on the board for now.** It started as a top-bar entry and a PR card menu item, and was pulled
because it is specific to repos with LTS release lines (openziti/ziti). See question 3.

## 2. Twig from a card whose agent is working

**Twig runs at once, always.** It takes the source worktree's HEAD and its uncommitted work as they are at that
moment, exactly as gwt does. It never waits for the turn to end and never refuses. Waiting was rejected because it
blocks the human on the agent.

The cost is accepted: a file the agent is mid-write may be snapshotted half done. Question 4 covers what happens
then.

## 3. Choosing the LTS line, and repo-specific verbs

**Settled: backport stays a CLI verb.** `atrium backport <pr> [--lts active|maint|release-vN.M.x]` keeps gwt's
rules (`_ResolveLtsBranch`, gwt :628): active is the highest `release-vN.M.x` on origin, maint the next one down,
and a terminal picker when the flag is omitted.

**Deferred: per-repo verbs.** A verb like backport only makes sense for some repos, so instead of a built-in menu
entry it becomes something a repo adds on. The shape clint sketched:

- The operator defines them in atrium settings, per repo (keyed by remote, for example `openziti/ziti`).
- They appear after a repo is picked in "New branch from a repo...", as extra options at that point, not on the
  top-level menu.

Nothing like this exists. Card actions (`internal/store/actions.go`) are the nearest precedent: operator-written,
scoped by tag or runner, but they only say text to a running card and cannot make a worktree. Inputs (a PR link, a
choice filled from `git branch -r`), the command it runs and the contract with atrium (the command prints the
worktree path, atrium launches a card there) are left for that item. Filed as g-repo-verbs.

## 4. When the git step fails

How each can fail:

- **Twig's patch not applying** is rare. The new worktree starts at the same HEAD the patch was taken against, so
  it fails only when a file changed during the snapshot (possible now that twig never waits) or a line-ending or
  LFS filter rewrites content.
- **A backport cherry-pick conflicting** is common, since a PR made on main often does not pick cleanly onto an
  older release line. Backport is CLI only for now, so the CLI prints the state and the recovery commands as gwt
  does.

**The card launches anyway, and the failure raises an error growler.** The growler names what failed (the patch
path left on disk, or the commit the pick stopped at and the conflicted files) and offers an optional prompt to
send to the card. Nothing is added to the card's first prompt. Low priority given how rarely twig fails.

## What this means for the items

- **g-twig**: the CLI as filed, plus the card menu item "New worktree from this worktree..." on the board, which
  runs immediately whatever the card is doing. A carried patch that does not apply raises an error growler with an
  optional prompt to send, and the card launches regardless.
- **g-backport**: CLI only, as filed, with gwt's `--lts` and picker. No board entry. The PR card menu item and the
  top-bar entry are dropped in favor of g-repo-verbs.
- **g-new-dialog**: owns turning "+ new agent" into the menu above (New agent..., New branch from a repo...,
  Open a link...).
- **g-repo-verbs** (new, low): operator-defined, per-repo extra options offered after a repo is picked in "New
  branch from a repo...". Backport for openziti/ziti is the first use. Needs its own design.
