Build job: filter drawer restyle. Your worktree is /Users/claude/git/github/dovholuknf/atrium-worktrees/filter-drawer-strip on branch claude/filter-drawer-strip at 8a62bab0 (claude/main, the drawer just landed: internal/api/web, see filterdrawer.css, filter-drawer.js, docs/backlog/ui/u-011-interview.md on hub claude/interview-board-controls for the original spec).

What clint wants:
1. On the Stack and Board tabs the drawer is an ACCORDION: opening it pushes the content below it down; it no longer floats over the content.
2. It looks like part of the header: a thin full-width strip right under the tab row, SAME background as the header, the summary text (for example "running + ready 19 · name · by group") left-aligned in it, and a chevron AFTER the text showing open or closed. Target look: fetch with atrium_get the file drawer-target.png (put on the hub drop by minion).
3. Make that drawer chevron, and the subagent-rollup chevron on cards, BIGGER. The card one is too small today: atrium_get rollup-chevron-small.png. Pick a size that is clearly tappable but does not grow the card row; say the px you chose.
4. Terminals tab: leave it UNCHANGED (clint has not decided). Do not touch the terminals drawer look or behaviour.
Keep the existing behaviour: attached card never filtered, phone terminals line, swallow guards, keyboard close; the drawer check (check-board-drawer.js) must still pass, update it where the markup moves.

Before/after PNGs are REQUIRED: shoot Stack and Board closed and open, plus a card with a subagent rollup chevron, before (on 8a62bab0) and after, at desktop width and 390px phone. Commit them under docs/backlog/ui/img/filter-drawer-strip/ and reference them in a short ship note docs/backlog/ui/filter-drawer-strip-notes.md. Use the repo's existing shoot scripts/Playwright setup (look at how earlier drawer shots were taken; NODE_PATH to Playwright if needed). Build output to build.claude/.
Tests: run the drawer check and the headless sections the change touches, one foreground call, long timeout. Known failing, not yours: TestReposIsOpenLikeGrowlsAndExactlyShaped, TestRealSessionsKeepTheirText.
Finish line: committed (one-line messages, no trailer), `git push hub claude/filter-drawer-strip` (git remote hub is set; if fetch complains about a token use existing refs), PNGs and notes included, tests named with counts, then atrium_done with the tip sha.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.

`atrium_resources` lists the machines and environments you may use.
