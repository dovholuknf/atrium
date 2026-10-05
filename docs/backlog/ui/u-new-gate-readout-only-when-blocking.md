# u-new-gate-readout-only-when-blocking. The typing gate readout speaks when something is held

Status: done 2026-10-04, see Design note. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint's first use of it.

## What clint saw

"gate closed: 203 unsent character(s) on the line · line "so it sounds to me like ..." " under his terminal, a full
copy of the text he was typing, which is already on screen right above it.

clint: "it's not bad. we only care if it's BLOCKING anything right, so just 'has input - nchars' or not?"

Then, of the open state, "yeah, like this": `gate open: line empty and quiet · line "" · 0 chars · last key 7.8s
ago`. That line is the target shape. The closed state should read like it, with the count and no copy of the text.

## Wanted

- Nothing held: the readout is one short dim line, or nothing. Say which in the design, recommend a dim "line
  empty" or "203 chars on the line".
- A message held behind the line: it says that plainly, with the count and who it is from, for example "1 message
  from @runtime waits: 203 chars on your line". This is the case the readout exists for.
- Never repeat the line's text. If a held say is waiting on a line that LOOKS empty, which is the case the
  readout was built for, show what atrium thinks is there, quoted and cut short, because only then is it news.

## Design note

- Nothing held (or the gate open): a dim line, never hidden while the readout is on. "line empty" with nothing typed,
  "203 chars on the line" with some. No stripe, no copy of the text.
- A message held behind a closed gate: "1 message from @runtime waits: 203 chars on your line", with the amber stripe.
- Held behind a line that LOOKS empty (the line holds only blanks, so the count means nothing to the eye): the count
  plus the line quoted with blanks drawn as dots and newlines as marks, cut at 40 characters. Example:
  `1 message from @runtime waits: 3 chars that look empty, atrium thinks "··⏎·"`. Held with a count of 0, the
  gate's own reason is said.
- The text is built by `typingGateText(s, held)` in `js/typing.js`, which returns `{ text, blocking }`, so the
  details drawer can reuse it.
- "Looks empty" is judged from atrium's model of the line (blank only), not from the screen. Reading the real
  screen row would need the prompt's shape and is a separate question for clint if the model's guess is too narrow.
