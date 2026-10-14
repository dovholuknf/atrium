# One card everywhere: decisions (r-unified-card)

clint's answers, 2026-10-13, to the four "Open choices" at the end of `docs/rnd/unified-card.md`. The fixed rules
stand as written there: grey means not running and nothing else, one running mark everywhere, a running card waiting
for clint is orange whoever launched it, and `runnerGone`/`cardWaiting` (839b365c) is the baseline.

## 1. The board's columns

Chosen: **A, changed.** The columns are **needs you, working, done** (plus inbox, hidden when empty). There is no
stopped column. "Running" stays the count above the board.

clint: "A runner that is gone is done not ready. Also a paused runner is 'shelved'. I want to easily find cards with
no more agent running. I feel like stopped and done and shelved all can go under the done column."

- done holds every card with no agent running: finished, dead, runner gone, parked, shelved.
- A ready card whose runner is gone goes to done, never to needs you.
- The count line has no stopped figure: `N running: X need you, Y working` and `Z done` at the right.
- The fixed width in 3.3 applies to done only.

## 2. A pinned card once it is finished

Chosen: **A.** Pinned cards sort first in done, under the same cap, and the more row counts them ("17 of them
pinned"). Option B no longer applied once stopped was gone.

clint: "A but make sure these can be collapsed still too." Done (and the pinned cards at its top) must stay
collapsible.

clint also said "blocked might be a status". Not settled here. Where a blocked card would sit was asked and not
answered, so it is open for a later design.

## 3. Shelved

Chosen: **A.** Since clint put shelved in done (choice 1), the question became how shelved is told apart in done.
Answer: one list, each card carrying its state chip on line 1 (`shelved 1d`, `stopped 4h`, `done 2d`), all grey, most
recent first. No sub-headings inside done. The column chip that hides shelved cards stays.

## 4. Repo colour

Chosen: **neither A nor B. Keep group colours as they are today:** the hued band, the tint on cards inside a group, and
the recolor menu.

clint: "I don't remember complaining about group colors... That are useful." Then: "As they are."

So section 3.4 of the design is dropped: `groupHue`, `GROUP_HUES`, `pickGroupHue`, `setGroupHue`, the recolor menu and
the `hues` grouping pref all stay, and the **noBands** test (section 6, item 7) is dropped. The design's premise that
the bands were a complaint was wrong. The build still has to keep the waiting orange and the off grey readable on a
tinted card.

## Aside

clint, during the interview: "On Mobile there is a substantial delay now between thinking and my response showing up."
Not investigated here. Worth a backlog item.
