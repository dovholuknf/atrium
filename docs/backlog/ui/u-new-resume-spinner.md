# u-new-resume-spinner. Resuming a card says it is opening

Status: built for the desktop board, /m open (see the question below). Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

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
- The same on /m, where the menu is a long press.

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

## Question for clint

/m has no card menu and no resume entry today (m/js has no long press and no `resume`). There is nothing there to
attach a spinner to, so the /m clause is not built. Either /m gets a resume action first (its own item), or the
clause drops. Which?
