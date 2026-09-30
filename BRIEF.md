# u-020: the phone terminal header takes a quarter of the screen

Launched by @ui (the board's director) for clint, via the orchestrator. URGENT: it is in the second phone hub deploy.
Report to @ui with atrium_report when done or blocked. Read CLAUDE.md, internal/api/CLAUDE.md and
internal/api/web/CLAUDE.md first.

## The bug

clint's phone screenshot (Brave on Android, 1080x2340, over a zrok share, live build). In full screen the BOARD
header is hidden already. What is left is the terminal pane's own header, `.term-bar` in index.html (around line 348:
#t-title, the alias chip, the room chip, pid, the path with its copy button, then #t-kind agent/shell, ctrl-c,
#t-attach paperclip, #t-full, #t-files folder, exit, and the gear). On a phone it wraps to 4 rows, about 25% of the
screen, and there is no collapse control anywhere. Earlier work (u-016) added a hide for the BOARD header
(#hdr-hide, phoneHeaderSet, js/termfull.js). That was not the bar he meant, which is why "the title bar still
cannot be minimized" on the live hub.

## The fix, on a phone only (body.term-phone, which termPhone() in terminal-links.js sets). Desktop is unchanged

1. Collapsed BY DEFAULT to one slim row: the alias (or the title when there is no alias) and a chevron button. A
   tap on the chevron (or the slim row) expands it and a tap collapses it again. Remember the choice per device in
   localStorage (e.g. `atrium.phone.termBarOpen`, the same style as `atrium.phone.headerHidden` in termfull.js).
   The chevron is a real button with an aria-label and aria-expanded, and it is at least 44px of touch target.
   Keep it clear of the notch (env(safe-area-inset-*)) and away from the key bar.
2. The pid, path and room chips show only when expanded.
3. Remove the duplicate paperclip from the terminal bar on a phone (#t-attach), since the key bar has
   #t-keys-attach. Keep #t-attach on desktop.
4. Drop the "fit this screen resizes the terminal for every window watching it" help text on a phone (.term-view,
   #t-view, around line 578). It costs two lines. Keep the button (#t-view-btn) and put the sentence in its
   data-tip, or move the whole row into the expanded header. Say which.
5. Decide where the buttons live when collapsed. Recommended: collapsed shows alias + chevron + full screen (#t-full,
   the way out of full screen must always be one tap) and nothing else. Expanded shows everything. Say what you
   chose.
- Collapsing or expanding changes the pane height, so xterm refits (ResizeObserver). That's fine, but it must not
  steal focus or pop the keyboard.
- The board is event driven: no polling and no timer loops.

## Do not touch

- u-019 is working IN PARALLEL on the phone terminal's HEIGHT (visual viewport and the keyboard) and the key bar's
  position, in css/terminal.css, css/phone.css and termfull.js sizing. Stay out of the height and key bar rules. You
  own .term-bar and .term-view on a phone. If you both need termfull.js, keep your change to the header state
  functions.
- u-017 owns the pan, focus and tap code in terminal-links.js. Do not change those functions.

## Tests

A new headless section `phoneTermBar` in scripts/test-board-headless.js (register it in `only` and in the full run,
and copy the preamble style of phoneView). On a 412x915 touch viewport, with a card attached, in both the normal phone
view and body.term-full:
1. Collapsed by default: the .term-bar is one row (height at most about 48px), the pid, path and room chips are not
   visible, and the chevron's box is at least 44px.
2. A tap on the chevron expands it (the chips are visible), a tap collapses it, and the choice survives a reload.
3. #t-attach is not visible on a phone and is visible on desktop, and #t-keys-attach is still there on a phone.
4. The "fit this screen" sentence is not visible as a line on a phone.
5. A desktop viewport is unchanged: the bar is fully shown and no chevron.
Run `HEADLESS_ONLY=phoneTermBar,phoneView,u016,phoneFocus` with NODE_PATH at a Playwright install (`npm i
playwright` in a scratch dir and `npx playwright install chromium` if sg3 has none) and `SKIP_HEADLESS=1 bash
scripts/check-board.sh`. Targeted tests only, never the full suite. Take a 412x915 screenshot, collapsed and
expanded, into your worktree's untracked scratch and say its path in the report.

## Rules

- Write docs/changes/u-020.md with a "Changelog" section and a "Test plan" section. Do not edit CHANGELOG.md,
  docs/test-plan.md or any CLAUDE.md.
- Commit on claude/u-020 only, one-line messages under 30 words, no trailers. No merge, no push, no deploy, and
  never touch the live room or hub.
- Report done with atrium_report to @ui with the sha.
