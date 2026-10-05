# rnd-forge-doc

Branch `claude/rnd-forge-doc`, based on claude/main d35d9c1e. Docs only, no code.

## Job
`docs/rnd/scm-forge-design.md` has an "Answers (clint, 2026-10-04)" section at the end. Much of the body above
it says things those answers overrule (a review reviewer found sections 0, 1, 2, 3, 4, 5, 6, 9, 10 and the stages
affected). Examples of what the answers decided:
- The PR's host picks the CLI (gh for github.com, bb for bitbucket.org, other hosts need a provider row).
- Same-repo PR uses its real branch, fork PR uses `pr-<N>`.
- Least busy room: fewest running sessions, tie broken by idle CPU.
- A room without a checkout gets the code from the hub's clone. "Atrium does not clone" is overruled.
- A PR on an offline room is never re-placed automatically. The board shows a warning alert.
- No PR list, no source row, no polling, no webhooks. PRs reach the board when clint pastes them.
- All forges are on ask. Missing access raises an alert with a message and configuration.
- Review on arrival is dropped.

Rewrite the body so it states the design as decided, once, without "originally" or "was changed to" history.
Keep the Answers section as the record. Then add a short "Built" section listing what exists on claude/main now,
by reading the code: `internal/forge`, `internal/api/prworktree.go`, `internal/daemon/forgeaccess.go`,
`internal/requirements` forges section, the PR placement and fold (`internal/api/pulls.go`, `placePRRoom`), and
the recognisers in `scripts/recognisers/`. Mark what is still not built (Bitbucket forge, idle-CPU tie-break,
the paste-to-card glue are in progress by other workers).

Also check `docs/scm-design.md` and `docs/rnd/hub-forge-answers.md` for statements that contradict the answers
and fix them the same way.

## Rules
- Docs only. Wrap at 120. No em-dashes, no double-hyphen dashes, no semicolons in prose.
- Commits: one-line subject, no trailers.

## When done
Write REPORT.md in the worktree root (what you changed, anything you could not reconcile), commit it, then ONE
atrium_report with exactly `done <sha>, REPORT.md` (or `blocked: <one line>`), and stop. No atrium_say, no other
report, no start message.
