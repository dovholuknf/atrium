# u-new-resume-spinner. Resuming a card says it is opening

Status: not started. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

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
