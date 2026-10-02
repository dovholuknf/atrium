# Review: context bar land-the-plane badge f79f64fa

Range `df35bbbb..f79f64fa`, one commit on claude/u-ctx-badge. Board only, with no Go:

- `css/cards.css`, `index.html`, `js/board.js`, `js/notes-files.js` and `js/peek.js`;
- the headless units;
- `docs/backlog/ui/u-new-ctx-bar-findings.md`.

It merges onto landing `fa5bb21b` cleanly: `index.html` auto-merges and nothing else overlaps.

Verdict: **HOLD on M1.** M1 is one line, and it is the contrast question you asked me to check. The rest is OK.

## The three things asked

- **The pulse plays once and respects reduced motion. Yes.**
  - `animation: ctxline-pulse 1.2s ease-out 1` runs once.
  - The existing reduced-motion block (`cards.css`, the peek block) already names `.peek-bar.ctxline.over i`, and it
    covers the new keyframes because the selector is unchanged.
  - Both lists redraw through `morphChildren`, which keeps the node. So the poll's next paint does not restart the
    animation.
- **The red on the dark skin keeps contrast against the row text. The row text does, but the badge does not.**
  - The row flood is danger at .18 to .42 alpha under light text on `--card-0`. It reads fine in
    real-graphite-390.png.
  - The badge is the problem, in M1 below.
- **mNet and the pause rules are untouched. Yes.** The diff touches neither. The one "pause" in it is the findings
  doc's sentence about the store.

## Medium

### M1: the LAND badge is white on light red on every dark skin, about 2.8:1

`.chip.ctxland` is `color: #fff` on `var(--danger)`.

- On paper, `--danger-rgb` is `190,45,45`, and white on it is about 5.8:1, which is fine.
- On graphite it is `255,107,107`, and white on it is about 2.8:1. The other dark skins' danger values (`248,113,113`,
  `255,84,84`, `251,113,133` and so on) land between 2.6 and 3.1.
- The badge is small bold text, so it needs 4.5:1, and it is the one place the red has to be read.

The fix: dark text on the dark skins. `color: var(--bg-0)` on `#ff6b6b` is about 7:1. Or add an `--on-danger`
token, `#fff` on light skins and `--bg-0` on dark ones, so the next solid danger chip gets it right. Either way, add
one line to the screenshot check.

## Points that are OK

- **The pref.**
  - Every `localStorage` access is in try/catch.
  - A value that is not digits, or is outside 1 to 2000, reads as 200.
  - `landThePlaneK` clamps the line to at least `threshold_k`.
  - The `storage` listener reacts to the key, or to a clear.
  - The gear box refuses anything outside 10 to 2000 with a toast.
- **Escaping.** Both tooltips and the badge's number go through `esc()`, the meter's `data-tip` too. The width and
  left are numbers.
- **One drawing.** The popover and the row both call `ctxMeter` with the land line as the limit, and `METER_SPAN`
  1.1 puts the tick at the line. The popover's scale reads "warns at 150k, lands at 200k".
- **The old assert's reversal is intended.** The mark now carries text past land, and below land it is still the
  icon only.

## Lows

- **L1: the findings doc quotes a phrase.** "the 90% thin pink line" (line 16) reads as a verbatim quote from a
  person. Paraphrase it, for example "the thin red line at about 90%". The repo is public.
- **L2: the gear box's lower bound and the stored value's.** The box refuses anything under 10, but a stored value
  of 1 to 9 is accepted. The clamp to `threshold_k` makes this harmless, but use one bound.

I read the units and did not run them, per the standing note. The mutants you list match what the asserts check.

Atrium-Verdict: hold df35bbbb..f79f64fa
