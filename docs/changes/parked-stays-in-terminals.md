# Parked cards stay in Terminals

A parked card (idle-parked: no process, status `parked`, `parked_at` set) used to drop out of the Terminals list,
because `termRowKind` gave it no kind unless it was pinned and the list only holds live rows and the chip-gated ones.
It now has the kind `parked`, which is always listed. It sits in its tag group (an `atrium:interview` card in its
interview group, never under its launcher), drawn grey, with a `resume` button on the row. The button, and a click on
the row, call `resumeParked`, the same `POST /v1/tasks/<id>/resume` the card menu uses.

This also reverses the idle-park exemption for interviews from `interview-top-level`: parking an interview is fine, so
`idleParkSubject` is back to its earlier content and `TestInterviewIsNeverIdleParked` is gone. The board half of
`interview-top-level` (no nesting under the launcher, the launcher's chevron) stays. No Go code changed relative to
what was deployed before that change.

Code: `js/terminal-list.js` (`termRowKind`, `termListed`, `termRow`, `termRowChips`). Test: the `parkedStays`
headless unit. `term-nesting` is known failing and was not run.

## Test plan

## @LETTER@. Parked cards stay in Terminals

1. Let an agent card (and an interview card) sit idle until it parks.
2. Open the terminals tab.

**Expected:** the parked card is still listed, in its tag group, grey, with a `resume` button. An interview is a top
row of its group. One click on `resume` (or on the row) starts it again on the same conversation.
