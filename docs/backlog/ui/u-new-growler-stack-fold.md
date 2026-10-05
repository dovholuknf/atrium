# u-new-growler-stack-fold: the growler's "+1 more" and the "fold" view confuse

Status: HELD (pause). Owned by @ui. Filed by the orchestrator 2026-10-02, from clint.

Screenshots on sg4: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261002-085639-pasted.png` and
`20261002-085656-pasted.png`.

## What happened

1. A growler "orchestrator asked 10 questions" had a dashed "+1 more: 1 question" row under it. clint expected that row
   to open the other card. It advanced the growler to the next one instead. clint: "i expected the +1 to open another
   card not advance me -- but that was ok."
2. He then saw a stacked list (one line per growler with "open" and "x") with a dashed "fold" row at the bottom.
   clint: "'fold' is -- stupid/strange..."

Also: "asked 10 questions" counted every numbered line of a reply that had 6 questions plus 4 numbered sub-items. The
count is wrong.

## Wanted

- "+1 more" says what it does: "next: <card> asked a question" or open that card directly. Pick one and label it.
- No "fold". Collapse the stack with the same control that opened it, in words a person uses ("show less", or a
  chevron), or drop the stacked view.
- Count questions from the Open Questions block only, top-level numbers only.
