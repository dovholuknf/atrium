# u-walk-phone-shots

Branch `claude/u-walk-phone-shots`, worktree on m1mini, based on claude/main c9d65765. Launched by the @rnd director.

## Why
`u-pr-finding-states` (merged) gave the PR walk drawer the states accepted, posted, dismissed and deferred, and at
phone width every state is meant to be one tap: the drawer's action buttons got `min-height: 40px` in
`internal/api/web/css/phone.css`. Its screenshots in `docs/screens/u-pr-finding-states/` never show those buttons,
because the headless phone viewport cuts the drawer off below the rail. Design: `docs/rnd/pr-review-workflow.md`
section 4.

## Job
1. Drive the walk drawer at a phone viewport (390x844 and one narrower, 360x740) in the headless board
   (`scripts/test-board-headless.js`, its `walk` and `pullsDrawer` sections, `HEADLESS_ONLY=...`). Scroll the drawer or
   the finding pane so the action buttons are in view, and shoot them. If the drawer really cannot show its buttons
   on a phone without scrolling past the terminal, that is a layout bug: fix it in `phone.css` (and `walk.js` only if
   needed) so the finding and its buttons are reachable, and shoot before and after.
2. Tap each of accept, posted, dismiss, defer and undo at phone width in the headless run, and assert the state
   changes. Add that to the headless walk section.
3. PNGs under `docs/screens/u-walk-phone-shots/`: the buttons in view at both widths, and a finding after each state.
   Before shots only if you changed the layout.

## Rules
- Go and node are at `/opt/homebrew/bin` (add it to PATH). Playwright: install into a scratch dir and use NODE_PATH
  if the repo has none, never into the repo.
- Build `go build -o build.claude/ ./...` if Go changes. Run `scripts/check-skins.sh`. `scripts/check-board.sh` has a
  pre-existing failure on title attributes in `index.html`, `hubrepos.js` and `changereq.js`, not yours.
- Commits: one-line subject, no body, no trailers. Changelog `changelog/ui/2026-10-04-u-walk-phone-shots.md` only if
  the layout changed.
- No em-dashes, no semicolons in prose, wrap at 120. Do not edit CLAUDE.md. Never commit on claude/main or main. No
  deploy, no restart of atrium.

## When done
Write REPORT.md in the worktree root (what was done, what the shots show, not done), commit it, then ONE
atrium_report with exactly `done <sha>, REPORT.md` or `incomplete <sha>, state in REPORT.md`. Nothing else, no
atrium_say, no start or progress messages.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.
