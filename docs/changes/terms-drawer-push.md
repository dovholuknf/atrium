# Terminals drawer pushes the terminal

`997c6f0d` made the Terminals filter drawer an overlay so the terminal kept its row count (no fit, no pty resize). This
reverses it for a trial: the drawer is an accordion, the terminal pane shrinks while it is open, and `fdRefitTerms`
calls `onTermResize` on open and close and once more when the slide ends, so the pty always gets the final size. The
screen's ResizeObserver also fits frame by frame during the slide, and the settled resize is debounced.

Going back is one line: `var FD_TERMS_PUSH = true;` to `false` in `js/filter-drawer.js`. The overlay CSS (`.foverlay`)
and the fixed-row behaviour are untouched. See `docs/backlog/ui/filter-drawer-strip-notes.md`.

Images: `img/terms-drawer-push/{before,after}-{closed,open}-{1280,390}.png`, shot by
`scripts/shoot-terms-drawer-push.js` (`before` is the overlay path).

## Test plan

- Headless `termsDrawerPush`: opening shrinks `#term-layout` and calls the resize, closing restores it, and
  `FD_TERMS_PUSH = false` brings the overlay back with the pane unchanged.
- By hand: attach a long scrollback, open and close the drawer a few times, look for duplicated lines.
