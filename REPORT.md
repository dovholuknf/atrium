# u-walk-phone-shots

## Done
- Headless `pullsDrawer` section now drives the walk drawer at 390x844 and 360x740. It opens the drawer with the walk
  button, scrolls the finding pane to the buttons, and asserts they sit inside the drawer and the viewport and are at
  least 40px tall. It then taps accept, posted (and the "mark posted" confirm), dismiss, defer and undo, and asserts the
  rail row, the finding's state label and the pulls row counts change each time. Passes.
- `WALK_SHOTS=dir HEADLESS_ONLY=pullsDrawer node scripts/test-board-headless.js` writes the PNGs.
- The layout was a real problem, so `phone.css` changed: the drawer max-height 62% -> 80% (finding pane about 130px ->
  about 190px at 390 wide), and the walk head wraps so walk done and the close button no longer fall off the right edge
  at 360 once a finding is marked. `walk.js` untouched. Changelog added.

## Shots, `docs/screens/u-walk-phone-shots/`
- `before-{390,360}-finding.png`, `before-{390,360}-buttons.png`: the 62% drawer. The finding is a sliver and the
  buttons are only reachable by scrolling it.
- `after-{390,360}-finding.png`: top of the finding. `after-*-buttons.png`: the buttons in view.
- `after-*-state-{accepted,posted,dismissed,deferred,undo}.png`: the finding after each tap, with its state label and
  the new count in the head.

## Not done
- The finding and the full button block still do not fit on screen together at either width: the page chrome (tabs,
  terminal picker, terminal bar, drawer head) uses about 340px at 390 and 440px at 360 before the drawer starts. Buttons
  are a scroll of the finding pane away. I tried sticky buttons and dropped them, they left about 50px for the finding.
- Taps use `click` in a resized page, not touch emulation (the shared context has no `hasTouch`).
- `scripts/check-board.sh` not run (pre-existing failure per the brief). `check-skins.sh` passes.
- Playwright was installed in a scratch dir, nothing in the repo.
