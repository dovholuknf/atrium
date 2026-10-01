# Review: u-no-toast ff5def61..f912ddd9 (m1mini, 2026-10-01): HOLD

claude/u-no-toast, 5 commits. Item: u-new-no-toast-on-focused-terminal.md, a pause exception approved by clint.
Commits are unsigned.

## What holds

- The rule sits in the right place. `growlOnIt` is `growlAsks && readySilenced(card)` (growl.js:49). That is the
  same test the ready alert follows: focused and visible (`focusIsHere`, notify.js:99), with the card attached on the
  terminals view or in solo (`termWatching`, notify.js:507), or another window's fresh focus claim naming the card.
  A raise under it is logged (growl.js:92), and it is kept out of `raised`, `growlDrawn` and `growlSay`.
- **Permission always shows: agreed.** The gate is answered on the board, not in the terminal, so hiding it hides
  the only way to unblock the card. Halts and deploy holds are not about one terminal. Keep both as they are.
- **The pop-out pairing at raise time is right.** The pop-out sends `win-focus` with `watch: watchedCard()`, the
  board's `readySilenced` matches it, and both windows hide the growler. The claim expires after `focusClaimFor`.
- **"Open" is right.** It awaits `landOnAlert`, then posts `dismiss` with the undo, the same as "dismiss this".
  `closeOpenDialogs` refusing still returns before anything is posted. On a permission, open now dismisses the
  growler. The request stays in the perms view, and the permission nag takes over from there, since `growlHas` goes
  false. That is fine.

## Medium (holds): a hidden question comes back only on a reminder, and after the 2h step never

`growlQuiet` loses an id in two places only: when the growler leaves the set (growl.js:102), and when a reminder
finds you elsewhere (growl.js:644). Nothing re-checks the rule when the conditions that made the growler quiet end:
window blur, the tab hidden, another card attached, the terminals view left, or the pop-out blurred or closed.
Reminders follow `growlBackoff` (internal/link/growl.go:84: 1, 2, 5, 10, 30, 60, 120 minutes from the raise) and stop
after the 2h step.

Scenario: clint is on the orchestrator terminal, a question is raised, he reads part of it and switches to another
card. Nothing is drawn until the next step, up to 30 or 60 minutes later. If he stayed on that terminal past the 2h
step (normal for the orchestrator), the growler never comes back. Only the card badge is left. Board and pop-out are
the same: a blurred or closed pop-out leaves the board's copy hidden.

The new case shows the gap. Its last step says "the next raise draws on the board again", but it only asserts that
the focus claim cleared. It never checks the hidden question.

Fix: re-check on the events that change the rule. That means `focus`, `blur` and `visibilitychange`, a terminal
attach or detach, a view switch, and the `win-focus` or `win-blur` message (plus the claim expiring). Drop every
quiet id where `!growlOnIt(g)` and call `growlDraw()`. Drawing without a ring is enough: you were on the card when it
was raised, and the next reminder rings as usual. Test: raise quiet, blur or attach another card, and expect it drawn
with no reminder. Do the same for the pop-out blur and close.

## Low

- The `growlOnIt` case's closing comment claims a redraw it does not test (see above). The fix's test covers it.

## Tests

Board checks are @ui's. I read the cases and did not run the suite. `growlOnIt` covers the raise rule, each
condition false in turn, a permission still showing, a reminder releasing the question, and the pop-out raise on
both windows. `growlActions` adds open, then one board `dismiss` post with the undo. They are meaningful and fail on
the old code, as the worker reports.

Quality: after the Sonnet switch, the work is clean and well reasoned. It reuses the existing silencing rule rather
than inventing one, and the design note answers the item's questions. The gap is state that leaves on the
"leaves the set" path only, never on "the reason went away", which is the usual miss for a hide-while rule.

Verdict: HOLD ff5def61..f912ddd9. A re-read starts at ff5def61, hub-ok and room-ok.
