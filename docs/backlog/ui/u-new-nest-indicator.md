# u-new-nest-indicator. A folded parent row says how many children are running

Status: filed by the orchestrator 2026-10-04, from clint. Owned by @ui. Built on claude/u-new-nest-indicator, not merged.

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

## Design note

Folded, the button on the parent row is `▸`, then three chips: the number of cards directly under it, the number of
cards at any depth that are running (the teal live colour, with the runner mark's spinner), and the number that need
you (the warn chip, `1 !`). The running and waiting chips only draw when non-zero, and the button wears a `running`
class. Running is `workingNow`, waiting is `isWaiting`, the same predicates the rows use. The count used to be the
hidden cards only and is now the total, since a waiting child stays drawn under the row. The hover says it in words.
Cost: on a narrow strip the parent's name truncates earlier when all three chips show. Unfolded rows are unchanged.

Before and after: `docs/screens/u-new-nest-indicator/`. The after shot has two children running and one waiting.
Under the skin in the shots the teal is the monochrome live colour, so the running chip is told apart by its spinner.
