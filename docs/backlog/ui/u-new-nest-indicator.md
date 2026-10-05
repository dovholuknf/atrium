# u-new-nest-indicator. A folded parent row says how many children are running

Status: filed by the orchestrator 2026-10-04, from clint. Owned by @ui. Not started.

## What is wrong

A row with folded child cards shows `▸ 3` in small grey text at its right edge. clint launched three workers, saw
"nothing running" on the terminals view, and only found them by clicking that marker: "was very subtle". The three
were running, with spinners, the whole time.

Screenshots: `.atrium/incoming/20261004-180432-pasted.png` (folded) and `20261004-180437-pasted.png` (unfolded).

## Done looks like

- A folded parent says what is under it at a glance: the count, and how many of those are running or need input,
  in the same visual weight as the row's other badges, not as grey text.
- A running child under a folded parent is visible from the parent row (a spinner or the running colour on the
  marker), so "nothing running" is never what the folded view says while something is.
- Before and after PNGs of real rows (rule: no visual change ships without them).
