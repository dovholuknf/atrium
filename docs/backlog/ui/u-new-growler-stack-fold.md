# u-new-growler-stack-fold: the growler's "+1 more" and the "fold" view confuse

Status: built 2026-10-04 on claude/u-new-growler-stack-fold (the orchestrator put it on tonight's list over the pause). Owned by @ui. Filed by the orchestrator 2026-10-02, from clint.

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

## Design note

- **"+1 more" is "next".** Of the two options (label the advance, or open that card directly) I kept the advance and
  labelled it: the row reads "next: <title>" and brings that growler up as the full one, going round the set. Opening
  the card is already one press away on the full growler ("open"), so a second way to do it on the strip would add a
  control and no ability. The growler in front is remembered by id, so a later event that replaces the set keeps it
  there while it lasts.
- **The stacked view stays, without "fold".** It is reached by a second row, "show all N: counts", shown only when more
  than one other growler waits (with one, "next" is all there is to show). It closes with "show less" in the place
  it opened from. The phone strip uses "show less" too. The reply box's own "fold the reply box" tooltip is a
  different control and is unchanged.
- **The count** is made by the Stop hook (`internal/cli/turnquestions.go`), not the board. It already read only the
  last Open Questions block, but it accepted up to three spaces of indent on every number, so the sub-items under a
  question (indented three spaces) counted as questions. A numbered line indented deeper than the first item is now
  part of the item above it.
