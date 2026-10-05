# u-new-gate-readout-only-when-blocking. The typing gate readout speaks when something is held

Status: not started. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint's first use of it.

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
