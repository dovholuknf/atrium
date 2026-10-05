# u-new-resume-spinner. Resuming a card says it is opening

Status: built for the desktop board and for /m. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

## What clint asked

"When I right click a card and 'resume the conversation' I want some sort of spinner that says to the user: hold
on, we are opening it now."

Today the `resume` entry in the card menu (`internal/api/web/js/card-menu.js`, `resumeEntries`) starts the runner
and the board shows nothing until the terminal appears, which can be several seconds. It reads as a click that
did nothing, and the obvious reaction is a second click.

## Wanted

- From the click until the terminal has its first output, the card and the place it opens (terminals pane or its
  own window) show a spinner with the words "opening the conversation". Both entries under `resume`: the last
  conversation and `choose...`.
- A second resume on the same card while the first is opening does nothing more than point at the spinner.
- If the launch fails or no output arrives within a bound, the spinner turns into the reason, not a blank pane.
- The same on /m, where the menu is a long press. Decided: /m gets a minimal resume action (see the decision below).

## Open for the design

- The bound before "this is taking longer than it should". Start at 30 seconds, since a lean resume of a large
  conversation is the slow case.

## Design note

- A resume is `opening` from the click until the terminal's first output frame, not until `/v1/launch` answers. State is
  held per card in `resumeOpening` (js/card-menu.js), keyed by bare id.
- The card and row carry a chip "opening the conversation" with a spinner (in the card and stack templates, so a redraw
  keeps it). The terminals pane shows the same words in its wait banner, which `openTerm` and the socket open used to
  clear. Both entries under `resume` (`the last conversation`, `choose...`, and the lean `with my full setup` one) go
  through `resumeNow`, so all of them are covered.
- A second resume while `opening` launches nothing and pulses the chip, with a toast saying it is already opening.
- Bound is 30 seconds (`RESUME_BOUND_MS`). Past it, or on a failed launch, the chip and the pane become the reason
  (`no output after 30 seconds...` or `could not start it: <error>`), the pane's spinner is hidden, and a resume is
  allowed again, since the first one may be dead. A late output still clears it.
- Resume into its own window: the chip clears once the window is opened, because that window is a separate page and
  has its own wait banner. The opened window does not yet say "opening the conversation" itself.

## Decision (@ui director, 2026-10-04)

/m gets a minimal resume action rather than the clause dropping. A long press on a /m card opens a small sheet with
"resume the last conversation". It makes the same launch the desktop menu does and the row shows "opening the
conversation" until the card's first output, with the same 30 second bound and failure reason.

## /m design note

- The state, the wording, the bound (`RESUME_BOUND_MS`), the launch body and the failure text are `js/resume-opening.js`,
  loaded by both pages. It holds no DOM and announces a change as a `resume-opening` event, which the board paints on the
  card, the row and the pane and /m paints on the row. `cannotResumeCard` is the part of `cannotResume` that /m can answer
  from the card alone. The board adds the runner's resume arguments.
- The gesture and the sheet are `m/js/resume.js`. A press is 550ms. It is cancelled by movement past 10px, by a lift, by
  the system taking the touch and by any scroll, so it does not fight scroll. The row is `user-select: none` with the
  platform callout off, and `contextmenu` (Android's long press, a right click) opens the same sheet. The lift that ends
  a long press does not open the card.
- /m has no terminal, so first output is the card running (`supervised`) with its activity moved past what it was at the
  click. A card that disappears also ends it. The row shows the spinner or the reason in place of its usual line.
- Only "the last conversation" is offered on /m. There is no `choose...` and no lean variant there.
- The "already opening" toast on the board is kept and dismissed when the opening ends in a reason or in output.

## Tests

`HEADLESS_ONLY=resumeSpinner` (board, now also asserts the toast clears) and `HEADLESS_ONLY=mResume` (/m, at phone
width): short press and scroll do not open the sheet, a long press does and does not open the card, a running card is
refused with a reason, one launch with the card's resume id, a second resume launches nothing, first output, the bound
(400ms in the test) and a failed launch.

## Pictures, `docs/screens/u-new-resume-spinner/`

Board: `before-*` and `after-1` to `after-6`. Phone: `before-m1-card` (nothing to press), `after-m1-card`, `after-m2-menu`,
`after-m3-opening`, `after-m4-output`, `after-m5-slow`, `after-m6-failed`.

## Carried over from the first round

- Resume into its own window clears the chip once the window opens. That window does not yet say "opening" itself.
- `scripts/check-board.sh` shows two FAILs (`follow-scroll`, `onData`) and a title-allowlist list that are identical on
  claude/main, so not from this change.
