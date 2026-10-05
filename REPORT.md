# u-new-tray-header-overlap

## What changed
- `internal/api/web/css/notify.css`: the tray head wraps (`flex-wrap: wrap`), the title is `min-width: max-content` with
  no ellipsis, and the gap is `--sp-2` by `--sp-4`. The title block also gets `display: block`, because phone.css hides
  every bare `.grow` at 900px and under, which removed the tray title on any window that narrow.
- `scripts/test-board-headless.js`: section `trayHead` at 320, 360, 901 and 1400px. It checks the title is shown and
  whole, nothing in the head overlaps, no button spills, and a button on the title's line is 8px or more from it.
  It failed on the old CSS (6.6px gap at 1400) and passes now.
- `scripts/shoot-dialogs.js`: `SHOOT_W` and `SHOOT_H` set the viewport.
- Changelog `changelog/ui/2026-10-04-u-new-tray-header-overlap.md`, design note in the item file.

## Design notes
- Wrap, not squeeze, as the item asks. The old comment said a wrapped button reads as broken, the item overrules it.
- Deploy pill cut: not intended, already handled. Desktop shows the short form and the tooltip has the full line.
  No change.

## Tests
- `HEADLESS_ONLY=trayHead node scripts/test-board-headless.js`: `trayHead ok`.
- Same command on the old notify.css: FAIL, first button 6.59px from the title at 1400.
- The full headless suite was not run.

## PNGs (real tray, mocked daemon, from shoot-dialogs)
`docs/screens/u-new-tray-header-overlap/` before-320, after-320, before-901, after-901. The before shots have no title
at 320 because of the hidden `.grow`.
