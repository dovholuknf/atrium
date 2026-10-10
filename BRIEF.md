# headless-sharded

Branch claude/headless-sharded in this worktree. First: `git fetch hub` then
`git rebase hub/claude/main`. Then also `git merge hub/claude/headless-green` (that branch fixes the suite itself and
lands tonight, your work builds on it). When done, push your branch: `git push hub claude/headless-sharded`.

## The job
`scripts/test-board-sharded.js` refuses to run. From docs/changes/headless-green.md (on the headless-green branch):
`mockVars()` lacks `cardColorsMock` and `blockerEsc`, and ten sections have no unit: termLagSection,
themePickerShotsSection, u002Section, mReloadSection, mReconnectSection, mActivityReadSection, mReadRetrySection,
switchBackCostSection, keepMemorySection, repliesShotsSection.

1. Make the sharded runner run: add the missing mock vars, give each listed section a unit (read how existing units
   are declared and copy that shape).
2. Add a check that fails the sharded runner at start when a section in test-board-headless.js has no unit, naming
   it, so this cannot rot silently again (if one exists and is what refused, make its message say what to do).
3. Run the sharded runner over the whole suite. Record wall time and pass/fail in
   docs/changes/headless-sharded.md, next to the serial time. Three units are known to flake in long runs (clearMark,
   mViewer, boardDocs): rerun a failing unit alone before calling it a failure, and list flakes.

Do not change board code under internal/api/web/. If a unit fails because the board is wrong, list it in the doc,
do not fix it.

## Notes
changelog/testing/2026-10-10-headless-sharded.md. No BOM, wrap at 120, no em-dashes, no semicolons in prose.

## Ending
Commit with a short one-line subject, no body, no trailer. Push the branch. Then atrium_done with the SHA, and exit.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.

`atrium_resources` lists the machines and environments you may use.
