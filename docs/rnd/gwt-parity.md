# gwt parity: what gwt does, what atrium does instead, and what is left

gwt is clint's PowerShell worktree tool (dotfiles `powershell/onpath/git-worktree.ps1`, dispatch from line 1651, with
`claude-shell.ps1` for the launch side and `gwt-session-registry.ps1` for the session registry). It is the baseline:
anything gwt already decides is not reopened here. This page lists every gwt command, flag and behavior, the atrium
equivalent with a file and line read from the code, and a verdict. The plan at the end turns every gap into a
backlog item. Two decision clusters gwt does not settle went to interviews.

Verdicts:

- **have**: atrium does it, possibly a different way.
- **partial**: some of it, and the cell says what is missing.
- **missing**: nothing in atrium does it.
- **n/a**: a Windows Terminal or registry detail that atrium's own model replaces. Saying why is the whole entry.

gwt line numbers are in `git-worktree.ps1` unless another file is named. Atrium paths are relative to this repo.

## Creating work

| gwt | What it does | Atrium | Verdict |
| --- | --- | --- | --- |
| `gwt new <branch>` (1653) | From inside any checkout (`cd ziti-sdk-c`, `gwt new some.new.work`): resolve org and repo from origin, ensure the main clone is fetched and on a fast-forwarded main (Ensure-RepoClonedAndUpdated, 431), make `<WtRoot>/github/<org>/<repo>/<branch>` off `origin/main`, run the repo hook, launch claude in a tab. Reopens when the worktree exists. | `makeProviderWorktree` (internal/api/providerapi.go:312) makes the worktree at `worktreeDest` (:410) but does not fetch, branches off the checkout's HEAD (:386), and launches nothing. Board: providers.js:482 `makeWorktree`. No CLI verb. `atrium launch` (internal/cli/launch.go:45) needs a directory you already made. | partial: g-new-from-checkout, g-new-dialog |
| `-From <src>` | Fork from another branch instead of `origin/main`. | Not on either path. | missing: g-new-from-checkout |
| `-Prompt <str>` | Override the default first prompt. | `atrium launch --prompt` (internal/cli/launch.go). | have |
| `-y`, default yes for creators (1527) | Skip confirmations. | Atrium verbs do not prompt. | have |
| `--by-project`, window and theme per repo | Group the tab in a window named for the repo, themed from a per-repo map. | `atrium launch --window`, `--theme` (internal/cli/launch.go). Grouping is the board's `window`. The per-repo theme map is the caller's. | have |
| `-Room` | Run the new session on another machine. | `atrium open --room` (internal/cli/open.go:48). | have |
| `-Current`, `GWT_NEW_CURRENT` | Point the `current` junction at the new worktree. | Nothing. | missing: g-cd-current |
| `GWT_NEW_CD` | cd the shell into the new worktree. | Nothing (atrium owns the terminal, the human's shell is elsewhere). | missing: g-cd-current |
| `-NoAgentSetup` | Skip dotagents setup in the new worktree. | Atrium never runs it, so it is always skipped. | missing: g-worktree-setup-hook |
| repo hook (claude-shell.ps1:160) | Per-repo worktree hook after creation (symlinks, presets, CMake user presets). | Not run by any atrium worktree path. | missing: g-worktree-setup-hook |
| `gwt rehook` (6144) | Rerun the repo hook on every worktree of the repo. | Nothing. | missing: g-worktree-setup-hook |
| `gwt twig <branch>` (2616) | New worktree off the current worktree's HEAD, carrying uncommitted work. | Nothing. | missing: g-twig |
| `gwt tangent <name>` (4820) | Scratch folder under `<WtRoot>/tangents/<name>`, not a repo, session in the `tangent` window. | `scratchDir` (internal/api/openkinds.go:413) is a scratch folder for links that have no repo. No verb asks for one by name. | partial: g-tangent |
| `gwt clone <url>` (2243), bare repo URL | Clone if missing, fetch, open a session in main. | `SCMClone` (internal/api/prworktree.go:263) only on the PR path. `atrium open --start` refuses a missing directory (internal/cli/open.go:282). | partial: g-clone-open |
| `gwt claude [<branch>\|.]` (4846) | Open a session in an existing worktree, remembering window and prompt picks, `-Reselect`. | `atrium launch --cwd` with `--if-running skip\|adopt` (internal/cli/launch.go:98). Picks are not remembered. | have (picks are per-card state in atrium) |
| prompt presets (claude-shell.ps1:40) | critique, continue, explore, test offered at launch. | Named prompts (internal/store/actions.go) are said to a running card, not offered as the first prompt. | partial: g-prompt-presets |

## Links: PR, issue, advisory, support

| gwt | What it does | Atrium | Verdict |
| --- | --- | --- | --- |
| bare URL routing (1446-1520) | `pull`, `issues`, `security/advisories`, ghsa fork tree and PR, repo URL each go to their verb. | The open verb (internal/api/open.go:85) and the hub's recognisers. Probed with `atrium open --show`: GitHub PR, issue, advisory, advisory fork branch, advisory fork PR, branch, repo, Discourse, Zendesk, Bitbucket PR. An actions-run URL is unrecognised (gwt does not route it either). | have |
| `gwt pr <url\|n>` (2261) | Worktree on the PR head, review card unless it is your own PR, `-Review` forces one. | `prWorktree` (internal/api/prworktree.go:89), review row and walker (internal/api/open.go steps 4 and 6). Always a review, whoever wrote it. | partial: g-pr-own-plain |
| pr on an existing worktree | Prompt focus, open or cancel. Default focus. | A live card answers the open (internal/api/openkinds.go:104). A link held by another card is refused with "that link is held by X" (:117) and no way to get there. | partial: g-held-link-goto (references u-005) |
| pr on a clone that is missing | Clones first. | `repoCheckout` and `SCMClone` (internal/api/prworktree.go:251, :263). Adopting a clone in a bad state fails with `errPRCloneAdopt` (internal/daemon/gitclone.go:120). | have (the adopt failure is u-006) |
| Bitbucket PR | bbapi, or a picker over remote heads. | Bitbucket recogniser, branch filled by the forge (internal/forge/bitbucket.go:183). | have |
| `gwt issue <url>` (1976) | Worktree `issue-<n>` off main, gh title in the prompt. | `branchWorktree` (internal/api/openkinds.go:286) makes a fresh branch off `origin/HEAD` (:362) without fetching. The title reaches the card, not the prompt (hub rule, internal/store/recognisers.go:521). | have (no fetch is folded into g-new-from-checkout) |
| `gwt advisory <url>` (2020) | Worktree `advisory-<GHSA>` off main, advisory body to the agent. | Recognised, opens like an issue. The body is not given to the agent, by the hub rule. | partial: g-advisory-fork |
| `gwt ghsa <url>` (2123) | Advisory fork tree or PR: add remote `ghsa-<id>`, fetch the branch or `refs/pull/N/head`, worktree on it, drop the remote with the last advisory worktree. | Recognised (`github-advisory-fork-branch`, `github-advisory-fork-pr`). No fork-remote code in Go, so open fails. | missing: g-advisory-fork |
| `gwt backport <pr>` (2382) | Worktree off an LTS line (`-Lts active\|maint`, picker when omitted), cherry-pick the PR's commits with `-x`, branch `backport.<vX.Y.x>.<src>`, conflicts left in place. | Nothing. | missing: g-backport |
| `gwt discourse <url>` (2455) | Worktree `discourse-<id>`, repo picker (default openziti/ziti), bare id and slug-only link, topic written to `DISCOURSE-<id>.md`. | Full URL recognised, repo picker in the dialog (fixtures.js:1655, default plus recent plus none). No bare id, no slug-only link, nothing written to the worktree. | partial: g-support-bare-ids |
| `gwt zendesk <url\|id>` (2545) | Same shape, `zendesk-<id>`, bare id against a configured host. | Full URL recognised (default ziti-tunnel-sdk-c). No bare id, no host setting, nothing written. | partial: g-support-bare-ids |
| `gwt open` (1834) | Open a worktree with a chosen opener, atrium or wt. | `atrium open` (internal/cli/open.go:48), `--attach` (:99). | have |

## Sessions

gwt keeps a registry (`gwt-session-registry.ps1`) because Windows Terminal remembers nothing. Atrium's cards are that
registry, kept by the room in its store, so most of `gwt sessions` is answered by the card model rather than ported.

| gwt | What it does | Atrium | Verdict |
| --- | --- | --- | --- |
| `gwt ls` / `list` (5131), `-All` | Sessions for this repo, or all. | The board, filtered by repo. `atrium peers` from a session. | have |
| `gwt sessions list -Usage`, `usage -SortBy` | Token use per session. | `GET /v1/usage` and `/v1/tasks/{id}/usage` (internal/api/api.go:820, :812), `atrium usage`. | have |
| `gwt sessions audit` | Cross-check the registry against processes. | The reaper asks the OS (internal/daemon/reaper.go). | have |
| `gwt sessions restore` (`-ByTabs`, `-BySessions`, `-DryRun`, `-MaxAgeDays`, `-IncludeEnded`, `-ExcludeActive`) | Reopen tabs after a reboot. | Reopen after restart (internal/daemon/reopen.go:131), `POST /v1/tasks/{id}/resume` (internal/api/api.go:803), the sessions list per card (internal/api/sessions.go:147). | have |
| `gwt sessions tabs set/prune/test/status/clear` | Windows Terminal tab bookkeeping. | Atrium owns the terminal. | n/a |
| `gwt sessions move`, `gwt rehome` (5012) | Move a tab onto another worktree without dragging wt tabs. | A card's terminal is not a wt tab. Starting a card elsewhere is `atrium launch --onto`. | n/a |
| `gwt sessions save/unsave`, `gwt save/unsave` (2722) | Protect a session from clean. | Cull hold (internal/api/api.go:690) and pinning (:559). Whether "saved" also protects a worktree from prune is the prune interview. | have for cards |
| `gwt sessions snapshot` | Write the registry out. | The store is the snapshot. `atrium backups`. | have |
| `gwt sessions clean` (`-Aborted`, `-IncludeActive`, `-IncludeDuplicates`) | Drop dead entries in tiers. | Archive and delete timers (internal/daemon/sweep.go), merged cull (internal/cli/merged.go:27). | have |
| `gwt sessions close`, `gwt close` (1928) | Close this tab, mark the session ended. | `atrium close <card>` (internal/cli/close.go:37), `atrium finish`. | have |
| `gwt rename <match> <label>` (6164) | A display label without touching the branch. | The alias tool (internal/link/control_mcp.go:275) and the card title. | have |
| `gwt focus [<match>]` (5898) | Bring a session's window forward. | The board, `atrium open --attach`. | have |
| `gwt watch` (5114) | Tail the hook state log. | The board's event stream (`GET /v1/events`, internal/api/api.go:831). | have |
| worktree-history.jsonl | Everything ever opened. | `GET /v1/history` (internal/api/intake.go:122). | have |
| `gwt update-registry` (2699) | Fetch the registry script. | Nothing to fetch. | n/a |

## Worktrees and cleanup

| gwt | What it does | Atrium | Verdict |
| --- | --- | --- | --- |
| states (docs/gwt-states.md) | MAIN, ACTIVE, ACTIVE-REMOTE-GONE, PRUNE, DIRTY, ORPHAN and ORPHAN-DIRTY per worktree. | worktreegone (internal/daemon/worktreegone.go) knows a card's folder is gone. No other state. | partial: g-worktree-states |
| `gwt ls -Verbose` | `git status --short` inline for DIRTY rows. | A card's changes (`GET /v1/tasks/{id}/changes`, internal/api/api.go:818). Only for worktrees with a card. | partial: g-worktree-states |
| `gwt changes` / `status` (5361) | Every dirty worktree in the repo, orphans included. | Per card only. | partial: g-worktree-states |
| `gwt summary [-WithSize]` (6044) | Count worktrees per repo, optional byte totals. | Nothing. | missing: g-worktree-states |
| `gwt state` (6002) | Where gwt keeps state, with counts. | `atrium status`, `atrium store`. | have |
| `gwt update` (5322) | Fast-forward every clean worktree with a live upstream. | Nothing. | missing: g-update-worktrees |
| `gwt prune` (5439) `-Force -Fetch -NoFetch -Org -Repo -Recapped`, `<branch>` | Remove merged and clean worktrees, guard live and saved sessions, locksmith for locked files, drop ghsa remotes, repoint `current`. | Sweep cleans leftovers atrium made and does not walk the provider folder (internal/api/sweep.go:17). Merged cull closes cards, not worktrees. | missing: g-prune |
| `gwt rm <branch>` (5069), Remove-Worktree (828) | Remove one worktree. Closes its card first with `POST /v1/cards/archive {cwd, wait}` (claude-shell.ps1:500). | Close by card id only (internal/api/api.go:611, internal/api/close.go:162). gwt's call 404s and gwt deletes anyway. | missing: g-close-by-cwd |
| `gwt cd <branch>` (4946) | cd into a worktree. | Nothing. | missing: g-cd-current |
| `gwt current [.\|<branch>]` (1855), `gwt activate` (1905) | Manage the `current` junction an IDE pins. | Nothing. | missing: g-cd-current |
| `-V` global flag | Show runas chatter. | n/a | n/a |

## Plan

Every partial and missing row above is one backlog item on the hub. r-003 and the gwt part of u-005 fold into
g-new-from-checkout, g-new-dialog and g-held-link-goto, which reference them. Neither is deleted. u-006 stays its own
item.

| Item | Dept | Priority | What | Cost |
| --- | --- | --- | --- | --- |
| g-new-from-checkout | runtime | high | `atrium new <name>` from inside any checkout: fetch, fast-forward main, worktree off `origin/main` or `--from`, card launched. Folds r-003. | medium |
| g-new-dialog | ui | high | The launch dialog's repo plus name runs the same flow. Folds r-003's dialog and the gwt part of u-005. | small |
| g-close-by-cwd | runtime | high | `POST /v1/cards/archive {cwd, wait}` and `atrium close --cwd`, the call gwt already makes. | small |
| g-held-link-goto | ui | high | A held link answers with the holding card and a go-to, default focus as gwt does. References u-005. | small |
| g-worktree-setup-hook | runtime | medium | Run the repo hook and dotagents setup after every worktree atrium makes, plus a rehook. | small to medium |
| g-pr-own-plain | runtime | medium | Your own PR opens plainly, `--review` forces a review. | small |
| g-twig | runtime | medium | `atrium twig <name>`: branch off this worktree's HEAD, carry uncommitted work. | small to medium |
| g-backport | runtime | medium | `atrium backport <pr>` onto an LTS line with `-x` picks. | medium |
| g-advisory-fork | runtime | medium | Advisory fork remotes, the advisory body as a data file. | medium |
| g-clone-open | runtime | medium | A repo link clones when missing and opens in the main clone. | small to medium |
| g-worktree-states | runtime | medium | gwt's states per worktree, a badge, `atrium worktrees` with summary and size. | medium |
| g-prune | runtime | medium | `atrium prune` with gwt's rules and guards. Depends on g-worktree-states. | medium to large |
| g-tangent | runtime | low | `atrium tangent <name>`: a named scratch folder and a card. | small |
| g-support-bare-ids | runtime | low | Discourse and Zendesk bare ids and slug-only links, ticket text as a data file. | small |
| g-update-worktrees | runtime | low | `atrium update`: fast-forward clean worktrees with a live upstream. | small |
| g-cd-current | runtime | low | `atrium cd` prints a path, an optional `current` link. | small |
| g-prompt-presets | ui | low | Named prompts offered as the first prompt at launch. | small |

Suggested order: g-close-by-cwd first (gwt is calling it today), then g-new-from-checkout and g-new-dialog, then
g-held-link-goto and g-worktree-setup-hook. g-prune waits for the ownership interview.

## What gwt does not settle

Two clusters are real choices, each with its own interview card on a `claude/interview-gwt-<slug>` branch off this
one. Everything else above copies gwt's decision.

- **claude/interview-gwt-board-verbs**: twig and backport from the board. A card menu item or a dialog mode, twig from
  a live agent's worktree (carry its uncommitted work while it is still writing, or refuse), and how the LTS line is
  picked when there is no terminal to prompt in.
- **claude/interview-gwt-prune-ownership**: whether atrium lists and prunes worktrees it did not make. The provider
  folder is gwt's by atrium's own rule (internal/api/sweep.go:17), so prune either stays on worktrees with a card,
  takes the whole folder, or lets gwt keep it. Also what gwt's "saved" maps to on a card.
