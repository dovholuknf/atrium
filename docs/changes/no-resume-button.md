# Remove the resume button; a click on a dead card does not resume it

No card in any list carries a resume button now: the Terminals rows (`termresume`), the ended-group rows
(`endedresume`) and the shelved card on the board are gone. The phone never had a visible one (its long-press sheet
stays). A click on a dead card (parked, exited, pinned and cold) is the same click as on any card, `attachTask`: it
opens or selects it and never calls `/resume` or `/launch`, so the "resuming" toast cannot come from a click. Resume
stays in the right-click menu (`card-menu.js`, `terminal-list.js` menu), and the hover texts say so.

Before and after: `img/no-resume-ended-before.png`, `img/no-resume-ended-after.png`.

## Test plan

- Headless unit `noResumeButton`: no list row has a resume button or click, a cold row's click is `attachTask`, and
  the menu still offers resume. `parkedStays` and `termEnded` were changed to match.
- By hand: click a parked card, nothing is resumed. Right-click it, "resume" is there and works.
