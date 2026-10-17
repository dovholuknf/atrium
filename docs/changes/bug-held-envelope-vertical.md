## Test plan

## @LETTER@. Queued-message envelope is vertically centred

Pictures: `docs/changes/img/bug-held-envelope-vertical/` (`desktop-` and `phone-`, `single-` and `count-`, each `before` and
`after`, cropped to a Terminals row with two dots and a room pill). Headless: `HEADLESS_UNITS=heldEnvelope,bootClean node
scripts/test-board-headless.js`, with `HELD_ENV_SHOTS=<dir>` and `HELD_ENV_TAG=<name>` to write the pictures.

The cause: `termHeldChip` drew U+2709 as text, and its ink sits low in the line box. It now draws `MAIL_SVG` in a flex
chip (`.chip.queued` and `.chip.held` in `css/cards.css`), as the drafted-answers chip does. `.chip.queued` also
collided with the message list's `.queued` in `css/files.css` (a column with a top margin), which stacked the count
under the envelope, so the chip resets both. The phone bell's envelopes (`.hmsg`, `.pb-mail`) are the same svg.

### @LETTER@1. Envelope alignment

1. Queue a message for a session and leave it waiting on the turn. Look at its Terminals row with one message, then
   two, then on a phone.

**Expected:** the envelope, its count and the dots and room pill beside it share one centre line, within a pixel.
