# Review: one-tooltip 19fc5e58 (@ui)

Range b6edcfd6..19fc5e58, one commit on sg3/claude/one-tooltip. clint's ask: one tooltip per terminal-list row, its
children carry none of the row's text, and the history recap chip no longer repeats the text the row grows to show.
Unsigned (sg3 has no key), noted as asked.

Board checks are @ui's. I read the `oneTooltip` section and did not run it. @ui reports it fails 9 ways on the old
code and passes now, and that bootClean, check-board and check-skins are green.

## What holds

- The row's `data-tip` is the hover text (name, whole address, suffix), plus the exited or joined sentence. It is now
  escaped, where the two fixed strings it replaced were not. The name and path spans and the joined chip lose their
  copies. `termRunnerMark` strips the mark's tip on this list only. `runnerMark` stays as it is for its other
  callers.
- The recap chip in a history row loses its `data-tip`, so the recap is said once, where the row grows.
- The pin star and the other chips keep tips of their own, which are not the row's text.

## Findings

### NITS

- The runner's name was only on the mark's tip (`runnerMark` writes `data-tip="<runner>"`), and an unknown harness
  draws only its first letter. On this list it is now said nowhere. Append it to the row's tip, for example
  `name · address · claude`.
- The cold and joined sentences are joined with `"\n"`. I found no `white-space: pre-line` on the tooltip, so the
  break shows as a space and the sentence runs on from the address. Every other composed tip joins with `" · "`
  (alias.js).

## Verdict

OK hub and room, b6edcfd6..19fc5e58, 2 nits.

Quality: after the Sonnet switch, no drop seen. The test was written to fail on the old code first, and the shared
`runnerMark` was left alone for its other callers.
