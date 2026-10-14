# The agent attribution gate

Status: built. The walls live in dovholuknf/dotfiles (live today) and in `internal/guard` (the atrium port). The ACL
hardening that would replace the drive-root guard is not part of this page: it is backlog rnd-002. A guard against
`git reset` on `claude/main` is rnd-003.

This is the record of what keeps an agent with a shell from committing under its own name or reaching a git remote,
what each wall is, how it can be got around, and what went wrong once. The product vulnerabilities are a different
subject: see `SECURITY-AUDIT-2026-10-01.md` beside this page. Paths into dotfiles are as relayed by the session that
owns them (dotfiles c4f97c8), not re-read for this page.

## The walls

| Wall | Where | What it does | How it is got around, and what backs it |
| --- | --- | --- | --- |
| Trailer wall | dotfiles `githooks/lib/attribution.sh`, `githooks/agent/commit-msg` | refuses a commit whose author, committer or trailer names claude or anthropic | `--no-verify` skips it, so `githooks/agent/pre-push` re-scans the pushed commits |
| Push wall | dotfiles `claude/hooks/pre-tool-use-hook.ps1` | blocks every git push, pull, fetch and git lfs remote op, except `git push [-u] hub\|atrium-hub <branch>` and `git fetch hub\|atrium-hub`, and checks the remote URL against atrium's `daemon.json` | the hub's pre-receive is the wall behind it |
| Drive-root guard | same ps1; reason in dotfiles `claude/hooks/docs/gate-decisions.md` ("2026-10-10: drive-root guard kept") | refuses creating a folder at a drive root and points at `D:\tmp` | it is a path rule, not a permission: icacls gives Authenticated Users Modify on `D:\` and AD on `C:\`, so it is the only enforcement |
| Atrium port | `internal/guard/rules.json`, wired by `internal/cli/hook_guard.go` (`atrium hook`) | the same three rules (trailer, `git-remote`, `drive-root`) | see `docs/rnd/gate-exe-vs-atrium-hook.md`; a hook that cannot start is non-blocking, so it fails open |

Which layer is live on a machine, and that the two do not drift, is the thing to check when a rule changes in one.

## The hooksPath split

The agent and the operator use different global `core.hooksPath`:

- claude: `githooks/agent`. `commit-msg` and `pre-push` as above. The other hook names are `_chain` copies that run the
  repo's own hooks, so a repo's hooks still work.
- clint: `githooks/`. `pre-push` runs the attribution scan again before anything reaches GitHub, and asks for
  confirmation on a push to main or master.

So the agent is held to attribution and no-remote, and the operator gets a last scan and a guard on the main branches.
Tests isolate themselves from the operator's git with `GIT_CONFIG_GLOBAL` (`docs/changes/tests-ignore-operator-git.md`).

## Incident: a misdirected `git reset --soft HEAD~1`

A `git reset --soft HEAD~1` was run by mistake in the atrium main checkout, `D:/git/github/dovholuknf/atrium`.
It moved `claude/main` back to 7857da92. It was then reset to 8836cd0d, the commit it had been on. A soft reset
keeps the index and the tree, and the target commit was known, so the branch was put back. The reflog was not
re-read for this page.

What it shows: the main checkout and a worktree look the same from a shell, and nothing in the walls above looks at
which branch a history-rewriting command lands on. The walls are about attribution and remotes, not about local
history. Whether a rule is worth having is rnd-003.

## Learnings

- A wall that a flag can skip needs a second one behind it (`--no-verify` is backed by pre-push, the push wall by the hub's pre-receive): rule, already built.
- Two copies of a rule in two repos drift; say which is live per machine: nothing new, this page is the record.
- A path rule stands in for a permission it cannot be: replace it with ACLs, then drop the rule: rnd-002.
- A hook that cannot start fails open, so a wall must not be the only one: nothing, a known limit of the hook model.
- The agent and the operator need different git hooks, and the repo's own hooks must still run (`_chain`): rule, already built.
- A history-rewriting command in the main checkout can move a shared branch unseen: guard, rnd-003.
- Tests must not read the operator's git config: rule, already built (`GIT_CONFIG_GLOBAL`).
