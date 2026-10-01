`node scripts/test-board-sharded.js` runs on sg3 by default now, not on the machine it is typed on. It snapshots the working
tree (committed or not) to a detached worktree in sg3's clone, runs the sharded suite there and streams the report and
the exit code back. sg4 lagged under the suite (50ms timer drift p99 104ms, max 317ms) where sg3 (26ms) and m1mini (5ms)
did not, at about the same wall time. `--local` or `ATRIUM_SUITE_LOCAL=1` runs it here, and so does a repository with no
git remote named sg3. `scripts/board-suite-remote.ps1` and `scripts/board-suite-run.ps1` do the work.

A snapshot step that fails (an unreadable untracked file such as one named NUL) stops the run with exit 3 instead of
testing HEAD without the changes. The suite arguments reach sg3 as an array, never as PowerShell, and each run there
sweeps worktrees and refs older than a day that an interrupted run left.
